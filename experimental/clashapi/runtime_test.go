package clashapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/experimental/clashapi/trafficontrol"
	"github.com/sagernet/sing-box/log"

	"github.com/gofrs/uuid/v5"
)

type fakeRuntimeInbound struct {
	tag           string
	lastUpsert    []adapter.RuntimeUser
	lastDelete    []string
	upsertCount   int
	deleteCount   int
	upsertInvoked int
	deleteInvoked int
}

func (f *fakeRuntimeInbound) Start(stage adapter.StartStage) error { return nil }
func (f *fakeRuntimeInbound) Close() error                         { return nil }
func (f *fakeRuntimeInbound) Type() string                         { return "vless" }
func (f *fakeRuntimeInbound) Tag() string                          { return f.tag }

func (f *fakeRuntimeInbound) UpsertRuntimeUsers(users []adapter.RuntimeUser) (int, error) {
	f.upsertInvoked++
	f.lastUpsert = append([]adapter.RuntimeUser(nil), users...)
	f.upsertCount += len(users)
	return len(users), nil
}

func (f *fakeRuntimeInbound) DeleteRuntimeUsers(principals []string) (int, error) {
	f.deleteInvoked++
	f.lastDelete = append([]string(nil), principals...)
	f.deleteCount += len(principals)
	return len(principals), nil
}

type fakeInboundManager struct {
	mu       sync.RWMutex
	inbounds map[string]adapter.Inbound
}

func newFakeInboundManager(inbounds ...adapter.Inbound) *fakeInboundManager {
	result := &fakeInboundManager{
		inbounds: make(map[string]adapter.Inbound, len(inbounds)),
	}
	for _, inbound := range inbounds {
		result.inbounds[inbound.Tag()] = inbound
	}
	return result
}

func (m *fakeInboundManager) Start(stage adapter.StartStage) error { return nil }
func (m *fakeInboundManager) Close() error                         { return nil }
func (m *fakeInboundManager) Inbounds() []adapter.Inbound {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]adapter.Inbound, 0, len(m.inbounds))
	for _, inbound := range m.inbounds {
		result = append(result, inbound)
	}
	return result
}

func (m *fakeInboundManager) Get(tag string) (adapter.Inbound, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inbound, loaded := m.inbounds[tag]
	return inbound, loaded
}

func (m *fakeInboundManager) Remove(tag string) error { return nil }
func (m *fakeInboundManager) Create(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, inboundType string, options any) error {
	return nil
}

type fakeTracker struct {
	metadata trafficontrol.TrackerMetadata
	closed   atomic.Bool
}

func newFakeTracker(principal string) *fakeTracker {
	id, _ := uuid.NewV4()
	return &fakeTracker{
		metadata: trafficontrol.TrackerMetadata{
			ID: id,
			Metadata: adapter.InboundContext{
				User: principal,
			},
			Upload:   &atomic.Int64{},
			Download: &atomic.Int64{},
		},
	}
}

func (t *fakeTracker) Metadata() *trafficontrol.TrackerMetadata { return &t.metadata }
func (t *fakeTracker) Close() error {
	t.closed.Store(true)
	return nil
}

func newRuntimeTestServer(inboundManager adapter.InboundManager) (*trafficontrol.Manager, *httptest.Server) {
	trafficManager := trafficontrol.NewManager()
	server := httptest.NewServer(runtimeRouter(trafficManager, inboundManager))
	return trafficManager, server
}

func TestRuntimeUsersBadRequest(t *testing.T) {
	_, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPut, server.URL+"/users", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestRuntimeUsersInboundNotFound(t *testing.T) {
	_, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()

	payload := `{"revision":1,"operations":[{"inbound":"missing","upsert":[{"principal":"u1:d1","uuid":"11111111-1111-1111-1111-111111111111"}]}]}`
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/users", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

func TestRuntimeUsersRevisionAndIdempotent(t *testing.T) {
	inbound := &fakeRuntimeInbound{tag: "vless-in"}
	_, server := newRuntimeTestServer(newFakeInboundManager(inbound))
	defer server.Close()

	newPayload := `{"revision":10,"request_id":"req-1","operations":[{"inbound":"vless-in","upsert":[{"principal":"u1:d1","uuid":"11111111-1111-1111-1111-111111111111"}]}]}`
	newReq, _ := http.NewRequest(http.MethodPut, server.URL+"/users", bytes.NewBufferString(newPayload))
	newReq.Header.Set("Content-Type", "application/json")
	newResp, err := http.DefaultClient.Do(newReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer newResp.Body.Close()
	if newResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status for new revision: %d", newResp.StatusCode)
	}

	stalePayload := `{"revision":9,"request_id":"req-2","operations":[{"inbound":"vless-in","upsert":[{"principal":"u1:d2","uuid":"22222222-2222-2222-2222-222222222222"}]}]}`
	staleReq, _ := http.NewRequest(http.MethodPut, server.URL+"/users", bytes.NewBufferString(stalePayload))
	staleReq.Header.Set("Content-Type", "application/json")
	staleResp, err := http.DefaultClient.Do(staleReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status for stale revision: %d", staleResp.StatusCode)
	}
	var staleBody map[string]any
	if err := json.NewDecoder(staleResp.Body).Decode(&staleBody); err != nil {
		t.Fatalf("decode stale response: %v", err)
	}
	if staleBody["rejected"] != "stale_revision" {
		t.Fatalf("expected stale rejection, got=%v", staleBody)
	}

	idemReq, _ := http.NewRequest(http.MethodPut, server.URL+"/users", bytes.NewBufferString(newPayload))
	idemReq.Header.Set("Content-Type", "application/json")
	idemResp, err := http.DefaultClient.Do(idemReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer idemResp.Body.Close()
	if idemResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status for idempotent request: %d", idemResp.StatusCode)
	}
	var idemBody map[string]any
	if err := json.NewDecoder(idemResp.Body).Decode(&idemBody); err != nil {
		t.Fatalf("decode idempotent response: %v", err)
	}
	if idemBody["idempotent"] != true {
		t.Fatalf("expected idempotent response, got=%v", idemBody)
	}
	if inbound.upsertInvoked != 1 {
		t.Fatalf("expected one upsert invocation, got=%d", inbound.upsertInvoked)
	}
}

func TestRuntimeUsersUpsertNameAlias(t *testing.T) {
	inbound := &fakeRuntimeInbound{tag: "vless-in"}
	_, server := newRuntimeTestServer(newFakeInboundManager(inbound))
	defer server.Close()

	payload := `{"revision":1,"operations":[{"inbound":"vless-in","upsert":[{"name":"u1:d1","uuid":"11111111-1111-1111-1111-111111111111"}]}]}`
	req, _ := http.NewRequest(http.MethodPut, server.URL+"/users", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
	if len(inbound.lastUpsert) != 1 {
		t.Fatalf("expected one upsert user, got=%d", len(inbound.lastUpsert))
	}
	if inbound.lastUpsert[0].Principal != "u1:d1" {
		t.Fatalf("expected principal mapped from name, got=%q", inbound.lastUpsert[0].Principal)
	}
}

func TestRuntimeDisconnectByUserIDIncludesWildcard(t *testing.T) {
	trafficManager, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()

	user := newFakeTracker("u1")
	device := newFakeTracker("u1:d1")
	other := newFakeTracker("u2:d1")
	trafficManager.Join(user)
	trafficManager.Join(device)
	trafficManager.Join(other)

	payload := `{"user_id":"u1"}`
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/disconnect", bytes.NewBufferString(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	if !user.closed.Load() || !device.closed.Load() {
		t.Fatal("expected both u1 and u1:* closed")
	}
	if other.closed.Load() {
		t.Fatal("unexpected close for other user")
	}
}

func TestRuntimePolicyRequestIDAndStale(t *testing.T) {
	_, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()

	applyPayload := `{"revision":10,"request_id":"policy-1","replace":false,"policies":[{"principal":"u1:*","max_connections":2}]}`
	applyReq, _ := http.NewRequest(http.MethodPut, server.URL+"/policy", bytes.NewBufferString(applyPayload))
	applyReq.Header.Set("Content-Type", "application/json")
	applyResp, err := http.DefaultClient.Do(applyReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer applyResp.Body.Close()
	if applyResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected apply status: %d", applyResp.StatusCode)
	}
	var applyBody map[string]any
	if err := json.NewDecoder(applyResp.Body).Decode(&applyBody); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	if applyBody["requestId"] != "policy-1" {
		t.Fatalf("unexpected requestId: %v", applyBody["requestId"])
	}

	stalePayload := `{"revision":9,"request_id":"policy-old","replace":false,"policies":[{"principal":"u1:d1","max_connections":1}]}`
	staleReq, _ := http.NewRequest(http.MethodPut, server.URL+"/policy", bytes.NewBufferString(stalePayload))
	staleReq.Header.Set("Content-Type", "application/json")
	staleResp, err := http.DefaultClient.Do(staleReq)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stale status: %d", staleResp.StatusCode)
	}
	var staleBody map[string]any
	if err := json.NewDecoder(staleResp.Body).Decode(&staleBody); err != nil {
		t.Fatalf("decode stale response: %v", err)
	}
	if staleBody["rejected"] != "stale_revision" {
		t.Fatalf("expected stale rejection, got=%v", staleBody)
	}
	if staleBody["requestId"] != "policy-old" {
		t.Fatalf("unexpected stale requestId: %v", staleBody["requestId"])
	}
}

var _ adapter.RuntimeUserInbound = (*fakeRuntimeInbound)(nil)
var _ adapter.InboundManager = (*fakeInboundManager)(nil)
var _ trafficontrol.Tracker = (*fakeTracker)(nil)
