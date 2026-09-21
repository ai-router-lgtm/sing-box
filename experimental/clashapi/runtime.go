package clashapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	stdjson "encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/experimental/clashapi/trafficontrol"

	"github.com/go-chi/chi/v5"
	"github.com/gofrs/uuid/v5"
)

const (
	runtimeSchemaVersion                = "runtime-control/v3"
	runtimeDigestVersion                = "canonical-json-sha256/v1"
	maxRuntimeDisconnectSelectors       = 1000
	maxRuntimeMutationBodyBytes   int64 = 16 << 20

	runtimeErrorInstanceMismatch   = "runtime_instance_mismatch"
	runtimeErrorStaleRevision      = "stale_revision"
	runtimeErrorRevisionConflict   = "revision_payload_conflict"
	runtimeErrorRequestConflict    = "request_id_payload_conflict"
	runtimeErrorDigestMismatch     = "payload_digest_mismatch"
	runtimeErrorUnsupportedInbound = "unsupported_inbound"
	runtimeErrorNotReady           = "runtime_not_ready"
)

var runtimeCapabilities = []string{
	"runtime_disconnect_all",
	"runtime_disconnect_fence",
	"runtime_disconnect_selectors",
	"runtime_policy_authoritative_replace",
	"runtime_policy_zero_delete",
	"runtime_request_payload_digest",
	"runtime_state_digest",
	"runtime_users_authoritative_replace",
	"runtime_users_delta",
}

type runtimeEnvelope struct {
	SchemaVersion             string `json:"schema_version"`
	ExpectedRuntimeInstanceID string `json:"expected_runtime_instance_id"`
	Revision                  int64  `json:"revision"`
	RequestID                 string `json:"request_id"`
	PayloadDigest             string `json:"payload_digest"`
}

type applyPolicyRequest struct {
	runtimeEnvelope
	Mode     string                          `json:"mode"`
	Replace  stdjson.RawMessage              `json:"replace,omitempty"`
	Policies []trafficontrol.PrincipalPolicy `json:"policies"`
}

type runtimeFence struct {
	UsersRevision  int64 `json:"users_revision"`
	PolicyRevision int64 `json:"policy_revision"`
}

type runtimeSelectors struct {
	Principals []string `json:"principals"`
	UserIDs    []string `json:"user_ids"`
}

type disconnectPrincipalRequest struct {
	runtimeEnvelope
	Fence     runtimeFence      `json:"fence"`
	Selectors *runtimeSelectors `json:"selectors,omitempty"`
	All       bool              `json:"all,omitempty"`
}

type applyUsersRequest struct {
	runtimeEnvelope
	Mode           string             `json:"mode"`
	Scope          string             `json:"scope,omitempty"`
	ReplaceManaged stdjson.RawMessage `json:"replace_managed,omitempty"`
	Operations     []usersOperation   `json:"operations"`
}

type usersOperation struct {
	Inbound string                `json:"inbound"`
	Upsert  []adapter.RuntimeUser `json:"upsert"`
	Delete  []string              `json:"delete"`
}

type canonicalUsersAction struct {
	SchemaVersion             string           `json:"schema_version"`
	ExpectedRuntimeInstanceID string           `json:"expected_runtime_instance_id"`
	Revision                  int64            `json:"revision"`
	Mode                      string           `json:"mode"`
	Scope                     string           `json:"scope,omitempty"`
	Operations                []usersOperation `json:"operations"`
}

type canonicalPolicyAction struct {
	SchemaVersion             string                          `json:"schema_version"`
	ExpectedRuntimeInstanceID string                          `json:"expected_runtime_instance_id"`
	Revision                  int64                           `json:"revision"`
	Mode                      string                          `json:"mode"`
	Policies                  []trafficontrol.PrincipalPolicy `json:"policies"`
}

type canonicalDisconnectAction struct {
	SchemaVersion             string            `json:"schema_version"`
	ExpectedRuntimeInstanceID string            `json:"expected_runtime_instance_id"`
	Revision                  int64             `json:"revision"`
	Fence                     runtimeFence      `json:"fence"`
	Selectors                 *runtimeSelectors `json:"selectors,omitempty"`
	All                       bool              `json:"all,omitempty"`
}

type runtimeDomainIdentity struct {
	Revision      int64  `json:"revision"`
	RequestID     string `json:"request_id"`
	PayloadDigest string `json:"payload_digest"`
}

type runtimeCachedResult struct {
	PayloadDigest string
	Status        int
	Body          []byte
}

type runtimeDomainState struct {
	runtimeDomainIdentity
	requests   map[string]runtimeCachedResult
	lastResult runtimeCachedResult
}

func newRuntimeDomainState() runtimeDomainState {
	return runtimeDomainState{requests: make(map[string]runtimeCachedResult)}
}

type runtimeState struct {
	traffic    *trafficontrol.Manager
	inbound    adapter.InboundManager
	instanceID string
	startedAt  time.Time

	// mutationAccess is the Runtime instance's users/policy/disconnect critical
	// section. It keeps domain fences and watermarks consistent with the actual
	// in-memory mutation they describe.
	mutationAccess sync.Mutex
	users          runtimeDomainState
	policy         runtimeDomainState
	disconnect     runtimeDomainState

	managedUsers     map[string]map[string]adapter.RuntimeUser
	baselineUsers    map[string]map[string]adapter.RuntimeUser
	baselineCaptured map[string]bool
}

func runtimeRouter(trafficManager *trafficontrol.Manager, inboundManager adapter.InboundManager) http.Handler {
	state := newRuntimeState(trafficManager, inboundManager)
	router := chi.NewRouter()
	router.Put("/policy", applyPolicy(state))
	router.Get("/policy/snapshot", getPolicySnapshot(state))
	router.Post("/disconnect", disconnectPrincipal(state))
	router.Get("/status", getRuntimeStatus(state))
	router.Get("/stats/snapshot", getPrincipalSnapshot(state.traffic))
	router.Put("/users", applyUsers(state))
	return router
}

func newRuntimeState(trafficManager *trafficontrol.Manager, inboundManager adapter.InboundManager) *runtimeState {
	instanceID, err := uuid.NewV4()
	instanceIDString := instanceID.String()
	if err != nil {
		instanceIDString = uuid.NewV5(uuid.Nil, fmt.Sprintf("runtime-%d", time.Now().UnixNano())).String()
	}
	state := &runtimeState{
		traffic:          trafficManager,
		inbound:          inboundManager,
		instanceID:       instanceIDString,
		startedAt:        time.Now().UTC(),
		users:            newRuntimeDomainState(),
		policy:           newRuntimeDomainState(),
		disconnect:       newRuntimeDomainState(),
		managedUsers:     make(map[string]map[string]adapter.RuntimeUser),
		baselineUsers:    make(map[string]map[string]adapter.RuntimeUser),
		baselineCaptured: make(map[string]bool),
	}
	if trafficManager != nil {
		state.policy.Revision = trafficManager.CurrentPolicyRevision()
	}
	return state
}

func (s *runtimeState) ready() bool {
	if s.inbound == nil || s.traffic == nil {
		return false
	}
	runtimeInboundCount := 0
	for _, inboundValue := range s.inbound.Inbounds() {
		if _, runtimeUserInbound := inboundValue.(adapter.RuntimeUserInbound); !runtimeUserInbound {
			continue
		}
		runtimeInboundCount++
		if _, atomicRuntimeUserInbound := inboundValue.(adapter.AtomicRuntimeUserInbound); !atomicRuntimeUserInbound {
			return false
		}
	}
	return runtimeInboundCount > 0
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

func applyPolicy(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request applyPolicyRequest
		if !decodeRuntimeJSON(w, r, &request) {
			return
		}
		request.runtimeEnvelope = normalizeRuntimeEnvelope(request.runtimeEnvelope)
		canonical, err := normalizePolicyAction(request)
		if err != nil {
			writeRuntimeError(w, state, http.StatusBadRequest, "policy", "invalid_request", err.Error(), nil)
			return
		}
		computedDigest := digestJSON(canonical)
		if !validateRuntimeEnvelope(w, state, "policy", request.runtimeEnvelope, computedDigest, state.ready()) {
			return
		}

		state.mutationAccess.Lock()
		defer state.mutationAccess.Unlock()
		if _, handled := handleDomainReplayOrConflict(w, state, "policy", &state.policy, request.runtimeEnvelope); handled {
			return
		}

		replace := canonical.Mode == "authoritative_replace"
		if !state.traffic.ApplyPolicyRevision(canonical.Revision, replace, canonical.Policies) {
			writeRuntimeError(w, state, http.StatusConflict, "policy", runtimeErrorStaleRevision, "policy revision is stale", &state.policy)
			return
		}
		state.policy.runtimeDomainIdentity = runtimeDomainIdentity{
			Revision:      canonical.Revision,
			RequestID:     request.RequestID,
			PayloadDigest: computedDigest,
		}
		_, policies := state.traffic.PolicyStateSnapshot()
		body := map[string]any{
			"schema_version":      runtimeSchemaVersion,
			"runtime_instance_id": state.instanceID,
			"domain":              "policy",
			"applied":             true,
			"idempotent":          false,
			"revision":            canonical.Revision,
			"request_id":          request.RequestID,
			"payload_digest":      computedDigest,
			"mode":                canonical.Mode,
			"count":               len(policies),
			"digest":              digestPolicies(policies),
		}
		cacheAndWriteDomainResult(w, &state.policy, request.RequestID, computedDigest, http.StatusOK, body)
	}
}

func disconnectPrincipal(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request disconnectPrincipalRequest
		if !decodeRuntimeJSON(w, r, &request) {
			return
		}
		request.runtimeEnvelope = normalizeRuntimeEnvelope(request.runtimeEnvelope)
		canonical, err := normalizeDisconnectAction(request)
		if err != nil {
			writeRuntimeError(w, state, http.StatusBadRequest, "disconnect", "invalid_request", err.Error(), nil)
			return
		}
		computedDigest := digestJSON(canonical)
		if !validateRuntimeEnvelope(w, state, "disconnect", request.runtimeEnvelope, computedDigest, state.ready()) {
			return
		}

		state.mutationAccess.Lock()
		defer state.mutationAccess.Unlock()
		if _, handled := handleDomainReplayOrConflict(w, state, "disconnect", &state.disconnect, request.runtimeEnvelope); handled {
			return
		}
		if state.users.Revision < canonical.Fence.UsersRevision || state.policy.Revision < canonical.Fence.PolicyRevision {
			message := fmt.Sprintf(
				"disconnect fence not reached: users=%d/%d policy=%d/%d",
				state.users.Revision,
				canonical.Fence.UsersRevision,
				state.policy.Revision,
				canonical.Fence.PolicyRevision,
			)
			writeRuntimeError(w, state, http.StatusServiceUnavailable, "disconnect", runtimeErrorNotReady, message, &state.disconnect)
			return
		}

		selectorCount := 0
		disconnected := 0
		if canonical.All {
			disconnected = state.traffic.DisconnectAll()
		} else {
			selectorCount = len(canonical.Selectors.Principals) + len(canonical.Selectors.UserIDs)
			disconnected = state.traffic.DisconnectSelectors(canonical.Selectors.Principals, canonical.Selectors.UserIDs)
		}
		state.disconnect.runtimeDomainIdentity = runtimeDomainIdentity{
			Revision:      canonical.Revision,
			RequestID:     request.RequestID,
			PayloadDigest: computedDigest,
		}
		body := map[string]any{
			"schema_version":      runtimeSchemaVersion,
			"runtime_instance_id": state.instanceID,
			"domain":              "disconnect",
			"applied":             true,
			"idempotent":          false,
			"revision":            canonical.Revision,
			"request_id":          request.RequestID,
			"payload_digest":      computedDigest,
			"fence":               canonical.Fence,
			"all":                 canonical.All,
			"selector_count":      selectorCount,
			"disconnected":        disconnected,
		}
		cacheAndWriteDomainResult(w, &state.disconnect, request.RequestID, computedDigest, http.StatusOK, body)
	}
}

func getPolicySnapshot(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if state.traffic == nil {
			writeRuntimeError(w, state, http.StatusServiceUnavailable, "policy", runtimeErrorNotReady, "traffic manager unavailable", nil)
			return
		}
		state.mutationAccess.Lock()
		defer state.mutationAccess.Unlock()
		policyRevision, policies := state.traffic.PolicyStateSnapshot()
		writeRuntimeJSON(w, http.StatusOK, map[string]any{
			"schema_version":      runtimeSchemaVersion,
			"runtime_instance_id": state.instanceID,
			"revision":            policyRevision,
			"request_id":          state.policy.RequestID,
			"payload_digest":      state.policy.PayloadDigest,
			"count":               len(policies),
			"digest":              digestPolicies(policies),
			"policies":            policies,
		})
	}
}

func getRuntimeStatus(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		state.mutationAccess.Lock()
		defer state.mutationAccess.Unlock()

		userState, userCount, userDigest := state.managedUserStatusLocked()
		policies := []trafficontrol.PrincipalPolicy{}
		if state.traffic != nil {
			_, policies = state.traffic.PolicyStateSnapshot()
		}
		writeRuntimeJSON(w, http.StatusOK, map[string]any{
			"protocol_version":    runtimeSchemaVersion,
			"digest_version":      runtimeDigestVersion,
			"ready":               state.ready(),
			"runtime_instance_id": state.instanceID,
			"version":             C.Version,
			"started_at":          state.startedAt,
			"capabilities":        runtimeCapabilities,
			"limits": map[string]any{
				"max_mutation_body_bytes":  maxRuntimeMutationBodyBytes,
				"max_disconnect_selectors": maxRuntimeDisconnectSelectors,
			},
			"users": map[string]any{
				"revision":       state.users.Revision,
				"request_id":     state.users.RequestID,
				"payload_digest": state.users.PayloadDigest,
				"count":          userCount,
				"digest":         userDigest,
				"inbounds":       userState,
			},
			"policy": map[string]any{
				"revision":       state.policy.Revision,
				"request_id":     state.policy.RequestID,
				"payload_digest": state.policy.PayloadDigest,
				"count":          len(policies),
				"digest":         digestPolicies(policies),
			},
			"disconnect": state.disconnect.runtimeDomainIdentity,
		})
	}
}

func getPrincipalSnapshot(trafficManager *trafficontrol.Manager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if trafficManager == nil {
			writeRuntimeJSON(w, http.StatusServiceUnavailable, map[string]any{"code": runtimeErrorNotReady})
			return
		}
		upload, download := trafficManager.Total()
		writeRuntimeJSON(w, http.StatusOK, map[string]any{
			"revision":        trafficManager.CurrentPolicyRevision(),
			"upload_total":    upload,
			"download_total":  download,
			"principal_stats": trafficManager.SnapshotByPrincipal(),
		})
	}
}

type runtimeUserOperationTarget struct {
	tag     string
	inbound adapter.RuntimeUserInbound
	atomic  adapter.AtomicRuntimeUserInbound
	desired map[string]adapter.RuntimeUser
}

func applyUsers(state *runtimeState) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request applyUsersRequest
		if !decodeRuntimeJSON(w, r, &request) {
			return
		}
		request.runtimeEnvelope = normalizeRuntimeEnvelope(request.runtimeEnvelope)
		canonical, err := normalizeUsersAction(request)
		if err != nil {
			writeRuntimeError(w, state, http.StatusBadRequest, "users", "invalid_request", err.Error(), nil)
			return
		}
		computedDigest := digestJSON(canonical)
		if !validateRuntimeEnvelope(w, state, "users", request.runtimeEnvelope, computedDigest, state.ready()) {
			return
		}

		state.mutationAccess.Lock()
		defer state.mutationAccess.Unlock()
		if _, handled := handleDomainReplayOrConflict(w, state, "users", &state.users, request.runtimeEnvelope); handled {
			return
		}
		targets, err := state.resolveUserTargetsLocked(canonical)
		if err != nil {
			writeRuntimeError(w, state, http.StatusUnprocessableEntity, "users", runtimeErrorUnsupportedInbound, err.Error(), &state.users)
			return
		}

		transactions := make([]adapter.RuntimeUserReplaceTransaction, 0, len(targets))
		abortTransactions := func() {
			for index := len(transactions) - 1; index >= 0; index-- {
				transactions[index].Abort()
			}
		}
		for _, target := range targets {
			state.ensureBaselineUsersLocked(target.tag, target.inbound)
			actualUsers := mergeRuntimeUsers(state.baselineUsers[target.tag], target.desired)
			transaction, prepareErr := target.atomic.PrepareRuntimeUserReplace(runtimeUserMapValues(actualUsers))
			if prepareErr != nil {
				abortTransactions()
				writeRuntimeError(w, state, http.StatusUnprocessableEntity, "users", "invalid_request", fmt.Sprintf("inbound %s rejected snapshot: %v", target.tag, prepareErr), &state.users)
				return
			}
			transactions = append(transactions, transaction)
		}

		totalUpserted := 0
		totalDeleted := 0
		for index, transaction := range transactions {
			result := transaction.Apply()
			totalUpserted += result.Upserted
			totalDeleted += result.Deleted
			state.managedUsers[targets[index].tag] = targets[index].desired
		}
		for _, transaction := range transactions {
			transaction.Release()
		}
		state.users.runtimeDomainIdentity = runtimeDomainIdentity{
			Revision:      canonical.Revision,
			RequestID:     request.RequestID,
			PayloadDigest: computedDigest,
		}
		_, userCount, userDigest := state.managedUserStatusLocked()
		body := map[string]any{
			"schema_version":      runtimeSchemaVersion,
			"runtime_instance_id": state.instanceID,
			"domain":              "users",
			"applied":             true,
			"idempotent":          false,
			"revision":            canonical.Revision,
			"request_id":          request.RequestID,
			"payload_digest":      computedDigest,
			"mode":                canonical.Mode,
			"scope":               canonical.Scope,
			"count":               userCount,
			"digest":              userDigest,
			"upserted":            totalUpserted,
			"deleted":             totalDeleted,
		}
		cacheAndWriteDomainResult(w, &state.users, request.RequestID, computedDigest, http.StatusOK, body)
	}
}

func (s *runtimeState) resolveUserTargetsLocked(action canonicalUsersAction) ([]runtimeUserOperationTarget, error) {
	targets := make([]runtimeUserOperationTarget, 0, len(action.Operations))
	provided := make(map[string]struct{}, len(action.Operations))
	for _, operation := range action.Operations {
		inboundValue, loaded := s.inbound.Get(operation.Inbound)
		if !loaded {
			return nil, fmt.Errorf("inbound not found: %s", operation.Inbound)
		}
		userInbound, ok := inboundValue.(adapter.RuntimeUserInbound)
		if !ok {
			return nil, fmt.Errorf("inbound %s does not support runtime users", operation.Inbound)
		}
		atomicInbound, ok := inboundValue.(adapter.AtomicRuntimeUserInbound)
		if !ok {
			return nil, fmt.Errorf("inbound %s does not support atomic runtime users", operation.Inbound)
		}
		desired := copyRuntimeUserMap(s.managedUsers[operation.Inbound])
		if action.Mode == "authoritative_replace" {
			desired = runtimeUserMap(operation.Upsert)
		} else {
			for principal, runtimeUser := range runtimeUserMap(operation.Upsert) {
				desired[principal] = runtimeUser
			}
			for _, principal := range operation.Delete {
				delete(desired, principal)
			}
		}
		targets = append(targets, runtimeUserOperationTarget{
			tag: operation.Inbound, inbound: userInbound, atomic: atomicInbound, desired: desired,
		})
		provided[operation.Inbound] = struct{}{}
	}
	if action.Mode != "authoritative_replace" {
		return targets, nil
	}
	controllable := 0
	for _, inboundValue := range s.inbound.Inbounds() {
		if _, ok := inboundValue.(adapter.RuntimeUserInbound); !ok {
			continue
		}
		controllable++
		if _, ok := inboundValue.(adapter.AtomicRuntimeUserInbound); !ok {
			return nil, fmt.Errorf("inbound %s does not support atomic runtime users", inboundValue.Tag())
		}
		if _, loaded := provided[inboundValue.Tag()]; !loaded {
			return nil, fmt.Errorf("authoritative snapshot omitted runtime inbound: %s", inboundValue.Tag())
		}
	}
	if len(provided) != controllable {
		return nil, fmt.Errorf("authoritative snapshot contains an unsupported inbound")
	}
	return targets, nil
}

func normalizeUsersAction(request applyUsersRequest) (canonicalUsersAction, error) {
	if len(request.ReplaceManaged) != 0 {
		return canonicalUsersAction{}, fmt.Errorf("replace_managed is not supported by %s", runtimeSchemaVersion)
	}
	mode := strings.TrimSpace(request.Mode)
	if mode != "delta" && mode != "authoritative_replace" {
		return canonicalUsersAction{}, fmt.Errorf("mode must be delta or authoritative_replace")
	}
	scope := strings.TrimSpace(request.Scope)
	if mode == "authoritative_replace" {
		if scope != "all_runtime_inbounds" {
			return canonicalUsersAction{}, fmt.Errorf("authoritative_replace requires scope=all_runtime_inbounds")
		}
	} else if scope != "" {
		return canonicalUsersAction{}, fmt.Errorf("delta does not accept scope")
	}
	if len(request.Operations) == 0 {
		return canonicalUsersAction{}, fmt.Errorf("operations must not be empty")
	}
	operations := make([]usersOperation, 0, len(request.Operations))
	seenInbounds := make(map[string]struct{}, len(request.Operations))
	for _, operation := range request.Operations {
		inboundTag := strings.TrimSpace(operation.Inbound)
		if inboundTag == "" {
			return canonicalUsersAction{}, fmt.Errorf("missing inbound tag")
		}
		if _, loaded := seenInbounds[inboundTag]; loaded {
			return canonicalUsersAction{}, fmt.Errorf("duplicate inbound: %s", inboundTag)
		}
		seenInbounds[inboundTag] = struct{}{}
		upsert, err := normalizeRuntimeUsers(operation.Upsert)
		if err != nil {
			return canonicalUsersAction{}, fmt.Errorf("inbound %s: %w", inboundTag, err)
		}
		deleted := normalizeStrings(operation.Delete)
		if mode == "authoritative_replace" && len(deleted) > 0 {
			return canonicalUsersAction{}, fmt.Errorf("authoritative_replace cannot contain delete")
		}
		upsertPrincipals := make(map[string]struct{}, len(upsert))
		for _, runtimeUser := range upsert {
			upsertPrincipals[runtimeUser.Principal] = struct{}{}
		}
		for _, principal := range deleted {
			if _, loaded := upsertPrincipals[principal]; loaded {
				return canonicalUsersAction{}, fmt.Errorf("principal appears in both upsert and delete: %s", principal)
			}
		}
		operations = append(operations, usersOperation{Inbound: inboundTag, Upsert: upsert, Delete: deleted})
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Inbound < operations[j].Inbound })
	return canonicalUsersAction{
		SchemaVersion: strings.TrimSpace(request.SchemaVersion), ExpectedRuntimeInstanceID: strings.TrimSpace(request.ExpectedRuntimeInstanceID),
		Revision: request.Revision, Mode: mode, Scope: scope, Operations: operations,
	}, nil
}

func normalizePolicyAction(request applyPolicyRequest) (canonicalPolicyAction, error) {
	if len(request.Replace) != 0 {
		return canonicalPolicyAction{}, fmt.Errorf("replace is not supported by %s", runtimeSchemaVersion)
	}
	mode := strings.TrimSpace(request.Mode)
	if mode != "delta" && mode != "authoritative_replace" {
		return canonicalPolicyAction{}, fmt.Errorf("mode must be delta or authoritative_replace")
	}
	policies := append([]trafficontrol.PrincipalPolicy(nil), request.Policies...)
	if policies == nil {
		policies = []trafficontrol.PrincipalPolicy{}
	}
	seen := make(map[string]struct{}, len(policies))
	for index := range policies {
		policy := &policies[index]
		policy.Principal = strings.TrimSpace(policy.Principal)
		if policy.Principal == "" {
			return canonicalPolicyAction{}, fmt.Errorf("policy principal must not be empty")
		}
		if _, loaded := seen[policy.Principal]; loaded {
			return canonicalPolicyAction{}, fmt.Errorf("duplicate policy principal: %s", policy.Principal)
		}
		seen[policy.Principal] = struct{}{}
		if policy.MaxConnections < 0 || policy.UpBPS < 0 || policy.DownBPS < 0 {
			return canonicalPolicyAction{}, fmt.Errorf("policy values must not be negative: %s", policy.Principal)
		}
		if mode == "authoritative_replace" && policy.MaxConnections == 0 && policy.UpBPS == 0 && policy.DownBPS == 0 {
			return canonicalPolicyAction{}, fmt.Errorf("authoritative policy snapshot must contain only nonzero policies: %s", policy.Principal)
		}
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].Principal < policies[j].Principal })
	return canonicalPolicyAction{
		SchemaVersion: strings.TrimSpace(request.SchemaVersion), ExpectedRuntimeInstanceID: strings.TrimSpace(request.ExpectedRuntimeInstanceID),
		Revision: request.Revision, Mode: mode, Policies: policies,
	}, nil
}

func normalizeDisconnectAction(request disconnectPrincipalRequest) (canonicalDisconnectAction, error) {
	if request.Fence.UsersRevision < 0 || request.Fence.PolicyRevision < 0 {
		return canonicalDisconnectAction{}, fmt.Errorf("disconnect fence revisions must not be negative")
	}
	var selectors *runtimeSelectors
	if request.Selectors != nil {
		normalized := runtimeSelectors{
			Principals: normalizeStrings(request.Selectors.Principals), UserIDs: normalizeStrings(request.Selectors.UserIDs),
		}
		if len(normalized.Principals)+len(normalized.UserIDs) > maxRuntimeDisconnectSelectors {
			return canonicalDisconnectAction{}, fmt.Errorf("too many selectors: maximum is %d", maxRuntimeDisconnectSelectors)
		}
		if len(normalized.Principals)+len(normalized.UserIDs) > 0 {
			selectors = &normalized
		}
	}
	if request.All == (selectors != nil) {
		return canonicalDisconnectAction{}, fmt.Errorf("exactly one of all=true or non-empty selectors is required")
	}
	return canonicalDisconnectAction{
		SchemaVersion: strings.TrimSpace(request.SchemaVersion), ExpectedRuntimeInstanceID: strings.TrimSpace(request.ExpectedRuntimeInstanceID),
		Revision: request.Revision, Fence: request.Fence, Selectors: selectors, All: request.All,
	}, nil
}

func normalizeRuntimeUsers(users []adapter.RuntimeUser) ([]adapter.RuntimeUser, error) {
	result := make([]adapter.RuntimeUser, 0, len(users))
	seen := make(map[string]struct{}, len(users))
	for _, runtimeUser := range users {
		runtimeUser = normalizeRuntimeUser(runtimeUser)
		if runtimeUser.Principal == "" {
			return nil, fmt.Errorf("runtime user principal must not be empty")
		}
		if _, loaded := seen[runtimeUser.Principal]; loaded {
			return nil, fmt.Errorf("duplicate runtime user principal: %s", runtimeUser.Principal)
		}
		if runtimeUser.Enabled != nil && !*runtimeUser.Enabled {
			return nil, fmt.Errorf("disabled users must be expressed as delete: %s", runtimeUser.Principal)
		}
		seen[runtimeUser.Principal] = struct{}{}
		runtimeUser.UUID = strings.TrimSpace(runtimeUser.UUID)
		runtimeUser.Flow = strings.TrimSpace(runtimeUser.Flow)
		runtimeUser.Enabled = nil
		result = append(result, runtimeUser)
	}
	sortRuntimeUsers(result)
	return result, nil
}

func normalizeRuntimeEnvelope(envelope runtimeEnvelope) runtimeEnvelope {
	envelope.SchemaVersion = strings.TrimSpace(envelope.SchemaVersion)
	envelope.ExpectedRuntimeInstanceID = strings.TrimSpace(envelope.ExpectedRuntimeInstanceID)
	envelope.RequestID = strings.TrimSpace(envelope.RequestID)
	envelope.PayloadDigest = strings.TrimSpace(envelope.PayloadDigest)
	return envelope
}

func validateRuntimeEnvelope(w http.ResponseWriter, state *runtimeState, domain string, envelope runtimeEnvelope, computedDigest string, ready bool) bool {
	if strings.TrimSpace(envelope.SchemaVersion) != runtimeSchemaVersion {
		writeRuntimeError(w, state, http.StatusBadRequest, domain, "invalid_schema_version", "schema_version must be "+runtimeSchemaVersion, nil)
		return false
	}
	if strings.TrimSpace(envelope.ExpectedRuntimeInstanceID) != state.instanceID {
		writeRuntimeError(w, state, http.StatusConflict, domain, runtimeErrorInstanceMismatch, "runtime instance does not match", nil)
		return false
	}
	if !ready {
		writeRuntimeError(w, state, http.StatusServiceUnavailable, domain, runtimeErrorNotReady, "runtime domain is not ready", nil)
		return false
	}
	if envelope.Revision <= 0 {
		writeRuntimeError(w, state, http.StatusBadRequest, domain, "invalid_request", "revision must be positive", nil)
		return false
	}
	if strings.TrimSpace(envelope.RequestID) == "" {
		writeRuntimeError(w, state, http.StatusBadRequest, domain, "invalid_request", "request_id must not be empty", nil)
		return false
	}
	if !validSHA256Digest(envelope.PayloadDigest) || !equalDigest(envelope.PayloadDigest, computedDigest) {
		writeRuntimeError(w, state, http.StatusUnprocessableEntity, domain, runtimeErrorDigestMismatch, "payload digest does not match canonical action", nil)
		return false
	}
	return true
}

func handleDomainReplayOrConflict(w http.ResponseWriter, state *runtimeState, domainName string, domain *runtimeDomainState, envelope runtimeEnvelope) (runtimeCachedResult, bool) {
	requestID := strings.TrimSpace(envelope.RequestID)
	digest := strings.TrimSpace(envelope.PayloadDigest)
	if cached, loaded := domain.requests[requestID]; loaded {
		if cached.PayloadDigest != digest {
			writeRuntimeError(w, state, http.StatusConflict, domainName, runtimeErrorRequestConflict, "request_id was already used for a different payload", domain)
			return runtimeCachedResult{}, true
		}
		writeCachedRuntimeResult(w, cached)
		return cached, true
	}
	if envelope.Revision < domain.Revision {
		writeRuntimeError(w, state, http.StatusConflict, domainName, runtimeErrorStaleRevision, "revision is lower than the Runtime watermark", domain)
		return runtimeCachedResult{}, true
	}
	if envelope.Revision == domain.Revision {
		if domain.PayloadDigest != digest {
			writeRuntimeError(w, state, http.StatusConflict, domainName, runtimeErrorRevisionConflict, "revision was already used for a different payload", domain)
			return runtimeCachedResult{}, true
		}
		result := domain.lastResult
		var body map[string]any
		if err := stdjson.Unmarshal(result.Body, &body); err != nil {
			writeRuntimeError(w, state, http.StatusInternalServerError, domainName, "runtime_internal_error", "cached domain result is unavailable", domain)
			return runtimeCachedResult{}, true
		}
		body["idempotent"] = true
		body["request_id"] = requestID
		result = runtimeCachedResult{PayloadDigest: digest, Status: http.StatusOK, Body: marshalRuntimeJSON(body)}
		domain.requests[requestID] = result
		writeRuntimeJSONBytes(w, result.Status, result.Body)
		return result, true
	}
	return runtimeCachedResult{}, false
}

func cacheAndWriteDomainResult(w http.ResponseWriter, domain *runtimeDomainState, requestID string, payloadDigest string, status int, value any) {
	body := marshalRuntimeJSON(value)
	result := runtimeCachedResult{PayloadDigest: payloadDigest, Status: status, Body: body}
	domain.requests[requestID] = result
	domain.lastResult = result
	writeRuntimeJSONBytes(w, status, body)
}

func writeCachedRuntimeResult(w http.ResponseWriter, result runtimeCachedResult) {
	var body map[string]any
	if err := stdjson.Unmarshal(result.Body, &body); err == nil {
		body["idempotent"] = true
		writeRuntimeJSON(w, result.Status, body)
		return
	}
	writeRuntimeJSONBytes(w, result.Status, result.Body)
}

func decodeRuntimeJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRuntimeMutationBodyBytes)
	decoder := stdjson.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeRuntimeJSON(w, http.StatusBadRequest, map[string]any{"code": "invalid_request", "message": err.Error()})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeRuntimeJSON(w, http.StatusBadRequest, map[string]any{"code": "invalid_request", "message": "request body must contain one JSON object"})
		return false
	}
	return true
}

func writeRuntimeError(w http.ResponseWriter, state *runtimeState, status int, domain string, code string, message string, identity *runtimeDomainState) {
	body := map[string]any{"code": code, "domain": domain, "message": message, "runtime_instance_id": state.instanceID}
	if identity != nil {
		body["revision"] = identity.Revision
		body["request_id"] = identity.RequestID
		body["payload_digest"] = identity.PayloadDigest
		body["current_revision"] = identity.Revision
		body["current_request_id"] = identity.RequestID
		body["current_payload_digest"] = identity.PayloadDigest
	}
	writeRuntimeJSON(w, status, body)
}

func writeRuntimeJSON(w http.ResponseWriter, status int, value any) {
	writeRuntimeJSONBytes(w, status, marshalRuntimeJSON(value))
}

func marshalRuntimeJSON(value any) []byte {
	body, err := stdjson.Marshal(value)
	if err != nil {
		return []byte(`{"code":"runtime_internal_error"}`)
	}
	return body
}

func writeRuntimeJSONBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func validSHA256Digest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func equalDigest(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
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
	if result == nil {
		return []string{}
	}
	return result
}

func normalizeRuntimeUser(user adapter.RuntimeUser) adapter.RuntimeUser {
	user.Principal = strings.TrimSpace(user.Principal)
	user.Name = strings.TrimSpace(user.Name)
	if user.Principal == "" {
		user.Principal = user.Name
	}
	user.Name = ""
	return user
}

func runtimeUserMap(users []adapter.RuntimeUser) map[string]adapter.RuntimeUser {
	result := make(map[string]adapter.RuntimeUser, len(users))
	for _, runtimeUser := range users {
		runtimeUser = normalizeRuntimeUser(runtimeUser)
		if runtimeUser.Principal != "" {
			result[runtimeUser.Principal] = runtimeUser
		}
	}
	return result
}

func copyRuntimeUserMap(users map[string]adapter.RuntimeUser) map[string]adapter.RuntimeUser {
	result := make(map[string]adapter.RuntimeUser, len(users))
	for principal, runtimeUser := range users {
		result[principal] = runtimeUser
	}
	return result
}

func mergeRuntimeUsers(baseline, managed map[string]adapter.RuntimeUser) map[string]adapter.RuntimeUser {
	result := copyRuntimeUserMap(baseline)
	for principal, runtimeUser := range managed {
		result[principal] = runtimeUser
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
	sort.Slice(users, func(i, j int) bool { return users[i].Principal < users[j].Principal })
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
		status = append(status, runtimeInboundUserStatus{Inbound: inboundTag, Count: len(users), Digest: digestJSON(users)})
		allUsers = append(allUsers, users...)
	}
	if status == nil {
		status = []runtimeInboundUserStatus{}
	}
	if allUsers == nil {
		allUsers = []canonicalRuntimeUser{}
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
			Inbound: inboundTag, Principal: principal, UUID: strings.TrimSpace(runtimeUser.UUID), Password: runtimeUser.Password,
			Flow: strings.TrimSpace(runtimeUser.Flow), AlterID: runtimeUser.AlterID, Enabled: enabled,
		})
	}
	if result == nil {
		return []canonicalRuntimeUser{}
	}
	return result
}

func digestPolicies(policies []trafficontrol.PrincipalPolicy) string {
	copyPolicies := append([]trafficontrol.PrincipalPolicy{}, policies...)
	for index := range copyPolicies {
		copyPolicies[index].Principal = strings.TrimSpace(copyPolicies[index].Principal)
	}
	sort.Slice(copyPolicies, func(i, j int) bool { return copyPolicies[i].Principal < copyPolicies[j].Principal })
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
