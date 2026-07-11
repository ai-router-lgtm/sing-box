package clashapi

import (
	"crypto/sha256"
	"encoding/hex"
	stdjson "encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/experimental/clashapi/trafficontrol"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/gofrs/uuid/v5"
)

const maxRuntimeDisconnectSelectors = 1000

var runtimeCapabilities = []string{
	"runtime_disconnect_batch",
	"runtime_policy_snapshot",
	"runtime_state_digest",
	"runtime_users_batch_delete",
	"runtime_users_batch_upsert",
	"runtime_users_replace_managed",
}

type applyPolicyRequest struct {
	Revision  int64                           `json:"revision"`
	RequestID string                          `json:"request_id"`
	Replace   *bool                           `json:"replace"`
	Policies  []trafficontrol.PrincipalPolicy `json:"policies"`
}

type disconnectPrincipalRequest struct {
	Principal  string   `json:"principal"`
	Principals []string `json:"principals"`
	UserID     string   `json:"user_id"`
	UserIDs    []string `json:"user_ids"`
	DeviceID   string   `json:"device_id"`
	User       string   `json:"user"`
	RequestID  string   `json:"request_id"`
}

type disconnectPrincipalResponse struct {
	Principal     string `json:"principal,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	SelectorCount int    `json:"selector_count"`
	Disconnected  int    `json:"disconnected"`
	Idempotent    bool   `json:"idempotent,omitempty"`
}

type applyUsersRequest struct {
	Revision       int64                 `json:"revision"`
	RequestID      string                `json:"request_id"`
	ReplaceManaged bool                  `json:"replace_managed"`
	Inbound        string                `json:"inbound"`
	Operations     []usersOperation      `json:"operations"`
	Upsert         []adapter.RuntimeUser `json:"upsert"`
	Delete         []string              `json:"delete"`
}

type usersOperation struct {
	Inbound string                `json:"inbound"`
	Upsert  []adapter.RuntimeUser `json:"upsert"`
	Delete  []string              `json:"delete"`
}

type runtimeState struct {
	traffic    *trafficontrol.Manager
	inbound    adapter.InboundManager
	instanceID string
	startedAt  time.Time

	userAccess       sync.Mutex
	userRevision     int64
	requests         map[string]int64
	managedUsers     map[string]map[string]adapter.RuntimeUser
	baselineUsers    map[string]map[string]adapter.RuntimeUser
	baselineCaptured map[string]bool

	disconnectAccess   sync.Mutex
	disconnectRequests map[string]disconnectPrincipalResponse
}

func runtimeRouter(trafficManager *trafficontrol.Manager, inboundManager adapter.InboundManager) http.Handler {
	state := newRuntimeState(trafficManager, inboundManager)
	r := chi.NewRouter()
	r.Put("/policy", applyPolicy(state.traffic))
	r.Get("/policy/snapshot", getPolicySnapshot(state.traffic))
	r.Post("/disconnect", disconnectPrincipal(state))
	r.Get("/status", getRuntimeStatus(state))
	r.Get("/stats/snapshot", getPrincipalSnapshot(state.traffic))
	r.Put("/users", applyUsers(state))
	r.Delete("/users/{principal}", deleteRuntimeUser(state))
	return r
}

func newRuntimeState(trafficManager *trafficontrol.Manager, inboundManager adapter.InboundManager) *runtimeState {
	instanceID, err := uuid.NewV4()
	instanceIDString := instanceID.String()
	if err != nil {
		instanceIDString = fmt.Sprintf("runtime-%d", time.Now().UnixNano())
	}
	state := &runtimeState{
		traffic:            trafficManager,
		inbound:            inboundManager,
		instanceID:         instanceIDString,
		startedAt:          time.Now().UTC(),
		requests:           make(map[string]int64),
		managedUsers:       make(map[string]map[string]adapter.RuntimeUser),
		baselineUsers:      make(map[string]map[string]adapter.RuntimeUser),
		baselineCaptured:   make(map[string]bool),
		disconnectRequests: make(map[string]disconnectPrincipalResponse),
	}
	return state
}

func (s *runtimeState) ensureBaselineUsersLocked(inboundTag string, userInbound adapter.RuntimeUserInbound) {
	if s.baselineCaptured[inboundTag] {
		return
	}
	users := make(map[string]adapter.RuntimeUser)
	for _, runtimeUser := range userInbound.SnapshotRuntimeUsers() {
		runtimeUser = normalizeRuntimeUser(runtimeUser)
		if runtimeUser.Principal != "" {
			users[runtimeUser.Principal] = runtimeUser
		}
	}
	s.baselineUsers[inboundTag] = users
	s.baselineCaptured[inboundTag] = true
}

func applyPolicy(trafficManager *trafficontrol.Manager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request applyPolicyRequest
		if err := render.DecodeJSON(r.Body, &request); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}
		replace := true
		if request.Replace != nil {
			replace = *request.Replace
		}
		requestID := strings.TrimSpace(request.RequestID)
		current := trafficManager.CurrentPolicyRevision()
		if request.Revision > 0 && request.Revision < current {
			render.JSON(w, r, map[string]any{
				"applied":   false,
				"revision":  current,
				"rejected":  "stale_revision",
				"requestId": requestID,
			})
			return
		}
		applied := trafficManager.ApplyPolicyRevision(request.Revision, replace, request.Policies)
		render.JSON(w, r, map[string]any{
			"applied":   applied,
			"revision":  trafficManager.CurrentPolicyRevision(),
			"count":     len(request.Policies),
			"requestId": requestID,
		})
	}
}

func disconnectPrincipal(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if state.traffic == nil {
			render.Status(r, http.StatusNotImplemented)
			render.JSON(w, r, map[string]any{"code": "runtime_batch_unsupported", "operation": "disconnect"})
			return
		}
		var request disconnectPrincipalRequest
		if err := render.DecodeJSON(r.Body, &request); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}
		if len(request.Principals)+len(request.UserIDs) > maxRuntimeDisconnectSelectors {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]any{"error": "too many selectors", "max_selectors": maxRuntimeDisconnectSelectors})
			return
		}
		principals, userIDs, legacyPrincipal := normalizeDisconnectSelectors(request)
		selectorCount := len(principals) + len(userIDs)
		if selectorCount == 0 {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}
		if selectorCount > maxRuntimeDisconnectSelectors {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]any{"error": "too many selectors", "max_selectors": maxRuntimeDisconnectSelectors})
			return
		}

		requestID := strings.TrimSpace(request.RequestID)
		if requestID != "" {
			state.disconnectAccess.Lock()
			cached, loaded := state.disconnectRequests[requestID]
			if loaded {
				state.disconnectAccess.Unlock()
				cached.Idempotent = true
				render.JSON(w, r, cached)
				return
			}
		}

		response := disconnectPrincipalResponse{
			Principal:     legacyPrincipal,
			RequestID:     requestID,
			SelectorCount: selectorCount,
			Disconnected:  state.traffic.DisconnectSelectors(principals, userIDs),
		}
		if requestID != "" {
			if len(state.disconnectRequests) >= 2048 {
				state.disconnectRequests = make(map[string]disconnectPrincipalResponse, 1024)
			}
			state.disconnectRequests[requestID] = response
			state.disconnectAccess.Unlock()
		}
		render.JSON(w, r, response)
	}
}

func getPolicySnapshot(trafficManager *trafficontrol.Manager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if trafficManager == nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, map[string]any{"error": "traffic manager unavailable"})
			return
		}
		policies := trafficManager.PolicySnapshot()
		render.JSON(w, r, map[string]any{
			"revision": trafficManager.CurrentPolicyRevision(),
			"count":    len(policies),
			"digest":   digestPolicies(policies),
			"policies": policies,
		})
	}
}

func getRuntimeStatus(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		state.userAccess.Lock()
		userRevision := state.userRevision
		userState, userCount, userDigest := state.managedUserStatusLocked()
		state.userAccess.Unlock()

		policies := []trafficontrol.PrincipalPolicy{}
		policyRevision := int64(0)
		if state.traffic != nil {
			policies = state.traffic.PolicySnapshot()
			policyRevision = state.traffic.CurrentPolicyRevision()
		}
		render.JSON(w, r, map[string]any{
			"ready":               state.inbound != nil && state.traffic != nil,
			"runtime_instance_id": state.instanceID,
			"version":             C.Version,
			"started_at":          state.startedAt,
			"capabilities":        runtimeCapabilities,
			"user_revision":       userRevision,
			"user_count":          userCount,
			"user_digest":         userDigest,
			"user_state":          userState,
			"policy_revision":     policyRevision,
			"policy_count":        len(policies),
			"policy_digest":       digestPolicies(policies),
		})
	}
}

func getPrincipalSnapshot(trafficManager *trafficontrol.Manager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		upload, download := trafficManager.Total()
		render.JSON(w, r, map[string]any{
			"revision":        trafficManager.CurrentPolicyRevision(),
			"upload_total":    upload,
			"download_total":  download,
			"principal_stats": trafficManager.SnapshotByPrincipal(),
		})
	}
}

type runtimeUserOperationTarget struct {
	tag       string
	inbound   adapter.RuntimeUserInbound
	operation usersOperation
}

func applyUsers(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if state.inbound == nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, map[string]any{"error": "inbound manager unavailable"})
			return
		}
		var request applyUsersRequest
		if err := render.DecodeJSON(r.Body, &request); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}
		if len(request.Operations) == 0 && request.Inbound != "" {
			request.Operations = append(request.Operations, usersOperation{
				Inbound: request.Inbound,
				Upsert:  request.Upsert,
				Delete:  request.Delete,
			})
		}
		if len(request.Operations) == 0 {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}

		targets := make([]runtimeUserOperationTarget, 0, len(request.Operations))
		seenInbounds := make(map[string]struct{}, len(request.Operations))
		for _, operation := range request.Operations {
			inboundTag := strings.TrimSpace(operation.Inbound)
			if inboundTag == "" {
				render.Status(r, http.StatusBadRequest)
				render.JSON(w, r, map[string]any{"error": "missing inbound tag"})
				return
			}
			if request.ReplaceManaged {
				if len(operation.Delete) > 0 {
					render.Status(r, http.StatusBadRequest)
					render.JSON(w, r, map[string]any{"error": "replace_managed cannot be combined with delete"})
					return
				}
				if _, exists := seenInbounds[inboundTag]; exists {
					render.Status(r, http.StatusBadRequest)
					render.JSON(w, r, map[string]any{"error": fmt.Sprintf("duplicate inbound in replace_managed request: %s", inboundTag)})
					return
				}
				seenInbounds[inboundTag] = struct{}{}
			}
			inbound, loaded := state.inbound.Get(inboundTag)
			if !loaded {
				render.Status(r, http.StatusNotFound)
				render.JSON(w, r, map[string]any{"error": fmt.Sprintf("inbound not found: %s", inboundTag)})
				return
			}
			userInbound, ok := inbound.(adapter.RuntimeUserInbound)
			if !ok {
				render.Status(r, http.StatusBadRequest)
				render.JSON(w, r, map[string]any{"error": fmt.Sprintf("inbound %s does not support runtime users", inboundTag)})
				return
			}
			targets = append(targets, runtimeUserOperationTarget{tag: inboundTag, inbound: userInbound, operation: operation})
		}

		state.userAccess.Lock()
		defer state.userAccess.Unlock()
		requestID := strings.TrimSpace(request.RequestID)
		if request.Revision > 0 && request.Revision < state.userRevision {
			render.JSON(w, r, map[string]any{
				"applied":   false,
				"revision":  state.userRevision,
				"rejected":  "stale_revision",
				"requestId": requestID,
			})
			return
		}
		if requestID != "" {
			if _, loaded := state.requests[requestID]; loaded {
				render.JSON(w, r, map[string]any{
					"applied":    false,
					"revision":   state.userRevision,
					"idempotent": true,
					"requestId":  requestID,
				})
				return
			}
		}

		totalUpserted := 0
		totalDeleted := 0
		for _, target := range targets {
			state.ensureBaselineUsersLocked(target.tag, target.inbound)
			var upserted, deleted int
			var err error
			if request.ReplaceManaged {
				upserted, deleted, err = state.replaceManagedUsersLocked(target)
			} else {
				upserted, deleted, err = state.applyUserDeltaLocked(target)
			}
			if err != nil {
				render.Status(r, http.StatusBadRequest)
				render.JSON(w, r, map[string]any{"error": err.Error(), "inbound": target.tag})
				return
			}
			totalUpserted += upserted
			totalDeleted += deleted
		}

		if request.Revision > 0 {
			state.userRevision = request.Revision
		}
		if requestID != "" {
			if len(state.requests) >= 2048 {
				state.requests = make(map[string]int64, 1024)
			}
			state.requests[requestID] = request.Revision
		}
		render.JSON(w, r, map[string]any{
			"applied":        true,
			"revision":       state.userRevision,
			"replaceManaged": request.ReplaceManaged,
			"upserted":       totalUpserted,
			"deleted":        totalDeleted,
		})
	}
}

func (s *runtimeState) replaceManagedUsersLocked(target runtimeUserOperationTarget) (int, int, error) {
	desired := runtimeUserMap(target.operation.Upsert)
	current := s.managedUsers[target.tag]
	if current == nil {
		current = make(map[string]adapter.RuntimeUser)
	}
	baseline := s.baselineUsers[target.tag]
	deletePrincipals := make([]string, 0)
	restoreUsers := make([]adapter.RuntimeUser, 0)
	for principal := range current {
		if _, keep := desired[principal]; keep {
			continue
		}
		if baselineUser, restore := baseline[principal]; restore {
			restoreUsers = append(restoreUsers, baselineUser)
		} else {
			deletePrincipals = append(deletePrincipals, principal)
		}
	}
	sort.Strings(deletePrincipals)
	deleted := 0
	if len(deletePrincipals) > 0 {
		var err error
		deleted, err = target.inbound.DeleteRuntimeUsers(deletePrincipals)
		if err != nil {
			return 0, 0, err
		}
	}
	upsertUsers := runtimeUserMapValues(desired)
	upsertUsers = append(upsertUsers, restoreUsers...)
	sortRuntimeUsers(upsertUsers)
	upserted := 0
	if len(upsertUsers) > 0 {
		var err error
		upserted, err = target.inbound.UpsertRuntimeUsers(upsertUsers)
		if err != nil {
			return 0, deleted, err
		}
	}
	s.managedUsers[target.tag] = desired
	return upserted, deleted, nil
}

func (s *runtimeState) applyUserDeltaLocked(target runtimeUserOperationTarget) (int, int, error) {
	managed := s.managedUsers[target.tag]
	if managed == nil {
		managed = make(map[string]adapter.RuntimeUser)
		s.managedUsers[target.tag] = managed
	}
	upsertUsers := runtimeUserMapValues(runtimeUserMap(target.operation.Upsert))
	upserted := 0
	if len(upsertUsers) > 0 {
		var err error
		upserted, err = target.inbound.UpsertRuntimeUsers(upsertUsers)
		if err != nil {
			return 0, 0, err
		}
		for _, runtimeUser := range upsertUsers {
			managed[runtimeUser.Principal] = runtimeUser
		}
	}
	deletePrincipals := normalizeStrings(target.operation.Delete)
	deleted := 0
	if len(deletePrincipals) > 0 {
		var err error
		deleted, err = target.inbound.DeleteRuntimeUsers(deletePrincipals)
		if err != nil {
			return upserted, 0, err
		}
		for _, principal := range deletePrincipals {
			delete(managed, principal)
		}
	}
	return upserted, deleted, nil
}

func deleteRuntimeUser(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if state.inbound == nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, map[string]any{"error": "inbound manager unavailable"})
			return
		}
		principal := strings.TrimSpace(chi.URLParam(r, "principal"))
		inboundTag := strings.TrimSpace(r.URL.Query().Get("inbound"))
		if principal == "" || inboundTag == "" {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}
		inbound, loaded := state.inbound.Get(inboundTag)
		if !loaded {
			render.Status(r, http.StatusNotFound)
			render.JSON(w, r, map[string]any{"error": fmt.Sprintf("inbound not found: %s", inboundTag)})
			return
		}
		userInbound, ok := inbound.(adapter.RuntimeUserInbound)
		if !ok {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]any{"error": fmt.Sprintf("inbound %s does not support runtime users", inboundTag)})
			return
		}
		state.userAccess.Lock()
		deleted, err := userInbound.DeleteRuntimeUsers([]string{principal})
		if err != nil {
			state.userAccess.Unlock()
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]any{"error": err.Error(), "inbound": inboundTag})
			return
		}
		if managed := state.managedUsers[inboundTag]; managed != nil {
			delete(managed, principal)
		}
		state.userAccess.Unlock()
		render.JSON(w, r, map[string]any{
			"inbound":   inboundTag,
			"principal": principal,
			"deleted":   deleted,
		})
	}
}

func normalizeDisconnectSelectors(request disconnectPrincipalRequest) (principals, userIDs []string, legacyPrincipal string) {
	principals = append(principals, request.Principals...)
	userIDs = append(userIDs, request.UserIDs...)
	if principal := strings.TrimSpace(request.Principal); principal != "" {
		principals = append(principals, principal)
		legacyPrincipal = principal
	} else if user := strings.TrimSpace(request.User); user != "" {
		principals = append(principals, user)
		legacyPrincipal = user
	}
	if userID := strings.TrimSpace(request.UserID); userID != "" {
		if deviceID := strings.TrimSpace(request.DeviceID); deviceID != "" {
			principal := userID + ":" + deviceID
			principals = append(principals, principal)
			if legacyPrincipal == "" {
				legacyPrincipal = principal
			}
		} else {
			userIDs = append(userIDs, userID)
			if legacyPrincipal == "" {
				legacyPrincipal = userID
			}
		}
	}
	return normalizeStrings(principals), normalizeStrings(userIDs), legacyPrincipal
}

func normalizeStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeRuntimeUser(user adapter.RuntimeUser) adapter.RuntimeUser {
	user.Principal = strings.TrimSpace(user.Principal)
	user.Name = strings.TrimSpace(user.Name)
	if user.Principal == "" {
		user.Principal = user.Name
	}
	return user
}

func runtimeUserMap(users []adapter.RuntimeUser) map[string]adapter.RuntimeUser {
	result := make(map[string]adapter.RuntimeUser, len(users))
	for _, runtimeUser := range users {
		runtimeUser = normalizeRuntimeUser(runtimeUser)
		if runtimeUser.Principal == "" || (runtimeUser.Enabled != nil && !*runtimeUser.Enabled) {
			continue
		}
		result[runtimeUser.Principal] = runtimeUser
	}
	return result
}

func runtimeUserMapValues(users map[string]adapter.RuntimeUser) []adapter.RuntimeUser {
	result := make([]adapter.RuntimeUser, 0, len(users))
	for _, runtimeUser := range users {
		result = append(result, runtimeUser)
	}
	sortRuntimeUsers(result)
	return result
}

func sortRuntimeUsers(users []adapter.RuntimeUser) {
	sort.Slice(users, func(i, j int) bool {
		return users[i].Principal < users[j].Principal
	})
}

type runtimeInboundUserStatus struct {
	Inbound string `json:"inbound"`
	Count   int    `json:"count"`
	Digest  string `json:"digest"`
}

type canonicalRuntimeUser struct {
	Inbound   string `json:"inbound"`
	Principal string `json:"principal"`
	UUID      string `json:"uuid,omitempty"`
	Password  string `json:"password,omitempty"`
	Flow      string `json:"flow,omitempty"`
	AlterID   int    `json:"alter_id,omitempty"`
	Enabled   bool   `json:"enabled"`
}

func (s *runtimeState) managedUserStatusLocked() ([]runtimeInboundUserStatus, int, string) {
	inboundTags := make([]string, 0, len(s.managedUsers))
	for inboundTag := range s.managedUsers {
		inboundTags = append(inboundTags, inboundTag)
	}
	sort.Strings(inboundTags)
	status := make([]runtimeInboundUserStatus, 0, len(inboundTags))
	allUsers := make([]canonicalRuntimeUser, 0)
	for _, inboundTag := range inboundTags {
		users := canonicalRuntimeUsers(inboundTag, s.managedUsers[inboundTag])
		status = append(status, runtimeInboundUserStatus{
			Inbound: inboundTag,
			Count:   len(users),
			Digest:  digestJSON(users),
		})
		allUsers = append(allUsers, users...)
	}
	return status, len(allUsers), digestJSON(allUsers)
}

func canonicalRuntimeUsers(inboundTag string, users map[string]adapter.RuntimeUser) []canonicalRuntimeUser {
	principals := make([]string, 0, len(users))
	for principal := range users {
		principals = append(principals, principal)
	}
	sort.Strings(principals)
	result := make([]canonicalRuntimeUser, 0, len(principals))
	for _, principal := range principals {
		runtimeUser := users[principal]
		enabled := runtimeUser.Enabled == nil || *runtimeUser.Enabled
		result = append(result, canonicalRuntimeUser{
			Inbound:   inboundTag,
			Principal: principal,
			UUID:      strings.TrimSpace(runtimeUser.UUID),
			Password:  runtimeUser.Password,
			Flow:      strings.TrimSpace(runtimeUser.Flow),
			AlterID:   runtimeUser.AlterID,
			Enabled:   enabled,
		})
	}
	return result
}

func digestPolicies(policies []trafficontrol.PrincipalPolicy) string {
	copyPolicies := append([]trafficontrol.PrincipalPolicy{}, policies...)
	for index := range copyPolicies {
		copyPolicies[index].Principal = strings.TrimSpace(copyPolicies[index].Principal)
	}
	sort.Slice(copyPolicies, func(i, j int) bool {
		return copyPolicies[i].Principal < copyPolicies[j].Principal
	})
	return digestJSON(copyPolicies)
}

func digestJSON(value any) string {
	payload, err := stdjson.Marshal(value)
	if err != nil {
		payload = []byte("null")
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
