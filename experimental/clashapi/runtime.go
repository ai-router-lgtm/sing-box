package clashapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/experimental/clashapi/trafficontrol"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

type applyPolicyRequest struct {
	Revision  int64                           `json:"revision"`
	RequestID string                          `json:"request_id"`
	Replace   *bool                           `json:"replace"`
	Policies  []trafficontrol.PrincipalPolicy `json:"policies"`
}

type disconnectPrincipalRequest struct {
	Principal string `json:"principal"`
	UserID    string `json:"user_id"`
	DeviceID  string `json:"device_id"`
	User      string `json:"user"`
}

type applyUsersRequest struct {
	Revision   int64                 `json:"revision"`
	RequestID  string                `json:"request_id"`
	Inbound    string                `json:"inbound"`
	Operations []usersOperation      `json:"operations"`
	Upsert     []adapter.RuntimeUser `json:"upsert"`
	Delete     []string              `json:"delete"`
}

type usersOperation struct {
	Inbound string                `json:"inbound"`
	Upsert  []adapter.RuntimeUser `json:"upsert"`
	Delete  []string              `json:"delete"`
}

type runtimeState struct {
	traffic *trafficontrol.Manager
	inbound adapter.InboundManager

	userAccess   sync.Mutex
	userRevision int64
	requests     map[string]int64
}

func runtimeRouter(trafficManager *trafficontrol.Manager, inboundManager adapter.InboundManager) http.Handler {
	state := &runtimeState{
		traffic:  trafficManager,
		inbound:  inboundManager,
		requests: make(map[string]int64),
	}
	r := chi.NewRouter()
	r.Put("/policy", applyPolicy(state.traffic))
	r.Post("/disconnect", disconnectPrincipal(state.traffic))
	r.Get("/stats/snapshot", getPrincipalSnapshot(state.traffic))
	r.Put("/users", applyUsers(state))
	r.Delete("/users/{principal}", deleteRuntimeUser(state))
	return r
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

func disconnectPrincipal(trafficManager *trafficontrol.Manager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var request disconnectPrincipalRequest
		if err := render.DecodeJSON(r.Body, &request); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}
		principal := resolvePrincipal(request)
		if principal == "" && strings.TrimSpace(request.UserID) == "" {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, ErrBadRequest)
			return
		}

		var disconnected int
		if request.Principal == "" && request.User == "" && strings.TrimSpace(request.UserID) != "" && strings.TrimSpace(request.DeviceID) == "" {
			principal = strings.TrimSpace(request.UserID)
			disconnected = trafficManager.DisconnectUser(principal)
		} else {
			disconnected = trafficManager.DisconnectPrincipal(principal)
		}
		render.JSON(w, r, map[string]any{
			"principal":    principal,
			"disconnected": disconnected,
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
		state.userAccess.Lock()
		if request.Revision > 0 && request.Revision < state.userRevision {
			current := state.userRevision
			state.userAccess.Unlock()
			render.JSON(w, r, map[string]any{
				"applied":   false,
				"revision":  current,
				"rejected":  "stale_revision",
				"requestId": strings.TrimSpace(request.RequestID),
			})
			return
		}
		requestID := strings.TrimSpace(request.RequestID)
		if requestID != "" {
			if _, loaded := state.requests[requestID]; loaded {
				current := state.userRevision
				state.userAccess.Unlock()
				render.JSON(w, r, map[string]any{
					"applied":    false,
					"revision":   current,
					"idempotent": true,
					"requestId":  requestID,
				})
				return
			}
		}
		state.userAccess.Unlock()

		totalUpserted := 0
		totalDeleted := 0
		for _, operation := range request.Operations {
			inboundTag := strings.TrimSpace(operation.Inbound)
			if inboundTag == "" {
				render.Status(r, http.StatusBadRequest)
				render.JSON(w, r, map[string]any{"error": "missing inbound tag"})
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
			if len(operation.Upsert) > 0 {
				upsertUsers := normalizeRuntimeUsers(operation.Upsert)
				upserted, err := userInbound.UpsertRuntimeUsers(upsertUsers)
				if err != nil {
					render.Status(r, http.StatusBadRequest)
					render.JSON(w, r, map[string]any{"error": err.Error(), "inbound": inboundTag})
					return
				}
				totalUpserted += upserted
			}
			if len(operation.Delete) > 0 {
				deleted, err := userInbound.DeleteRuntimeUsers(operation.Delete)
				if err != nil {
					render.Status(r, http.StatusBadRequest)
					render.JSON(w, r, map[string]any{"error": err.Error(), "inbound": inboundTag})
					return
				}
				totalDeleted += deleted
			}
		}

		state.userAccess.Lock()
		if request.Revision > 0 {
			state.userRevision = request.Revision
		}
		if requestID != "" {
			if len(state.requests) > 2048 {
				state.requests = make(map[string]int64, 1024)
			}
			state.requests[requestID] = request.Revision
		}
		current := state.userRevision
		state.userAccess.Unlock()
		render.JSON(w, r, map[string]any{
			"applied":  true,
			"revision": current,
			"upserted": totalUpserted,
			"deleted":  totalDeleted,
		})
	}
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
		deleted, err := userInbound.DeleteRuntimeUsers([]string{principal})
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]any{"error": err.Error(), "inbound": inboundTag})
			return
		}
		render.JSON(w, r, map[string]any{
			"inbound":   inboundTag,
			"principal": principal,
			"deleted":   deleted,
		})
	}
}

func resolvePrincipal(request disconnectPrincipalRequest) string {
	if principal := strings.TrimSpace(request.Principal); principal != "" {
		return principal
	}
	if userID := strings.TrimSpace(request.UserID); userID != "" {
		if deviceID := strings.TrimSpace(request.DeviceID); deviceID != "" {
			return userID + ":" + deviceID
		}
		return userID
	}
	return strings.TrimSpace(request.User)
}

func normalizeRuntimeUsers(users []adapter.RuntimeUser) []adapter.RuntimeUser {
	if len(users) == 0 {
		return users
	}
	normalized := make([]adapter.RuntimeUser, 0, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.Principal) == "" && strings.TrimSpace(user.Name) != "" {
			user.Principal = strings.TrimSpace(user.Name)
		}
		normalized = append(normalized, user)
	}
	return normalized
}
