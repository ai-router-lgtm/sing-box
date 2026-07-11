package clashapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	snapshot      []adapter.RuntimeUser
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

func (f *fakeRuntimeInbound) SnapshotRuntimeUsers() []adapter.RuntimeUser {
	return append([]adapter.RuntimeUser(nil), f.snapshot...)
}

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
	closes   atomic.Int64
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
	t.closes.Add(1)
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

func TestRuntimeUsersReplaceManagedPreservesStaticUsersAndStableDigest(t *testing.T) {
	staticUUID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	inbound := &fakeRuntimeInbound{
		tag: "vless-in",
		snapshot: []adapter.RuntimeUser{{
			Principal: "static-admin",
			UUID:      staticUUID,
		}},
	}
	_, server := newRuntimeTestServer(newFakeInboundManager(inbound))
	defer server.Close()

	applyRuntimePayload(t, server.URL, `{
		"revision":1,
		"request_id":"seed-managed",
		"operations":[{"inbound":"vless-in","upsert":[
			{"principal":"static-admin","uuid":"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"},
			{"principal":"u1:d1","uuid":"11111111-1111-1111-1111-111111111111"}
		]}]
	}`)
	applyRuntimePayload(t, server.URL, `{
		"revision":2,
		"request_id":"replace-managed-1",
		"replace_managed":true,
		"operations":[{"inbound":"vless-in","upsert":[
			{"principal":"u3:d1","uuid":"33333333-3333-3333-3333-333333333333"},
			{"principal":"u2:d1","uuid":"22222222-2222-2222-2222-222222222222"}
		]}]
	}`)

	if len(inbound.lastDelete) != 1 || inbound.lastDelete[0] != "u1:d1" {
		t.Fatalf("replace_managed must delete only stale managed users, got=%v", inbound.lastDelete)
	}
	upsertByPrincipal := make(map[string]adapter.RuntimeUser)
	for _, runtimeUser := range inbound.lastUpsert {
		upsertByPrincipal[runtimeUser.Principal] = runtimeUser
	}
	if restored, found := upsertByPrincipal["static-admin"]; !found || restored.UUID != staticUUID {
		t.Fatalf("expected original static user restored, got=%+v found=%v", restored, found)
	}
	if _, found := upsertByPrincipal["u2:d1"]; !found {
		t.Fatal("expected desired managed user u2:d1")
	}
	if _, found := upsertByPrincipal["u3:d1"]; !found {
		t.Fatal("expected desired managed user u3:d1")
	}

	firstStatusBody := getRuntimeStatusBody(t, server.URL)
	var firstStatus struct {
		RuntimeInstanceID string `json:"runtime_instance_id"`
		UserCount         int    `json:"user_count"`
		UserDigest        string `json:"user_digest"`
		UserState         []struct {
			Inbound string `json:"inbound"`
			Count   int    `json:"count"`
			Digest  string `json:"digest"`
		} `json:"user_state"`
	}
	if err := json.Unmarshal(firstStatusBody, &firstStatus); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if firstStatus.RuntimeInstanceID == "" || firstStatus.UserCount != 2 || len(firstStatus.UserDigest) != 64 {
		t.Fatalf("unexpected runtime status: %+v", firstStatus)
	}
	if len(firstStatus.UserState) != 1 || firstStatus.UserState[0].Inbound != "vless-in" || firstStatus.UserState[0].Count != 2 {
		t.Fatalf("unexpected inbound user state: %+v", firstStatus.UserState)
	}
	for _, secret := range []string{staticUUID, "22222222-2222-2222-2222-222222222222", "33333333-3333-3333-3333-333333333333"} {
		if bytes.Contains(firstStatusBody, []byte(secret)) {
			t.Fatalf("runtime status leaked credential %s", secret)
		}
	}

	applyRuntimePayload(t, server.URL, `{
		"revision":3,
		"request_id":"replace-managed-2",
		"replace_managed":true,
		"operations":[{"inbound":"vless-in","upsert":[
			{"principal":"u2:d1","uuid":"22222222-2222-2222-2222-222222222222"},
			{"principal":"u3:d1","uuid":"33333333-3333-3333-3333-333333333333"}
		]}]
	}`)
	secondStatusBody := getRuntimeStatusBody(t, server.URL)
	var secondStatus struct {
		RuntimeInstanceID string `json:"runtime_instance_id"`
		UserDigest        string `json:"user_digest"`
	}
	if err := json.Unmarshal(secondStatusBody, &secondStatus); err != nil {
		t.Fatalf("decode second runtime status: %v", err)
	}
	if secondStatus.RuntimeInstanceID != firstStatus.RuntimeInstanceID || secondStatus.UserDigest != firstStatus.UserDigest {
		t.Fatalf("expected stable instance and order-independent digest: first=%+v second=%+v", firstStatus, secondStatus)
	}
}

func TestRuntimeDisconnectBatchIsIdempotent(t *testing.T) {
	trafficManager, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()

	user := newFakeTracker("u1:d1")
	bareUser := newFakeTracker("u1")
	exact := newFakeTracker("u2:d1")
	other := newFakeTracker("u3:d1")
	for _, tracker := range []*fakeTracker{user, bareUser, exact, other} {
		trafficManager.Join(tracker)
	}
	payload := `{"request_id":"disconnect-batch-1","user_ids":["u1","u1"],"principals":["u2:d1","u2:d1"]}`
	responseBody := runtimeJSONRequest(t, http.MethodPost, server.URL+"/disconnect", payload, http.StatusOK)
	var response disconnectPrincipalResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		t.Fatalf("decode disconnect response: %v", err)
	}
	if response.SelectorCount != 2 || response.Disconnected != 3 || response.Idempotent {
		t.Fatalf("unexpected disconnect response: %+v", response)
	}
	if other.closed.Load() {
		t.Fatal("unexpected close for unmatched tracker")
	}

	late := newFakeTracker("u1:d2")
	trafficManager.Join(late)
	secondBody := runtimeJSONRequest(t, http.MethodPost, server.URL+"/disconnect", payload, http.StatusOK)
	var second disconnectPrincipalResponse
	if err := json.Unmarshal(secondBody, &second); err != nil {
		t.Fatalf("decode idempotent response: %v", err)
	}
	if !second.Idempotent || second.Disconnected != 3 {
		t.Fatalf("expected cached disconnect response, got %+v", second)
	}
	if late.closed.Load() {
		t.Fatal("idempotent retry must not rescan and close a later connection")
	}
	if user.closes.Load() != 1 || bareUser.closes.Load() != 1 || exact.closes.Load() != 1 {
		t.Fatalf("expected each original tracker closed once: user=%d bare=%d exact=%d", user.closes.Load(), bareUser.closes.Load(), exact.closes.Load())
	}
}

func TestRuntimeDisconnectRejectsMoreThanMaximumSelectors(t *testing.T) {
	_, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()
	principals := make([]string, maxRuntimeDisconnectSelectors+1)
	for index := range principals {
		principals[index] = fmt.Sprintf("u%d:d1", index)
	}
	payload, err := json.Marshal(map[string]any{"principals": principals})
	if err != nil {
		t.Fatalf("marshal selectors: %v", err)
	}
	responseBody := runtimeJSONRequest(t, http.MethodPost, server.URL+"/disconnect", string(payload), http.StatusBadRequest)
	if !strings.Contains(string(responseBody), "too many selectors") {
		t.Fatalf("unexpected selector limit response: %s", responseBody)
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

func TestRuntimePolicySnapshotMatchesStatusDigest(t *testing.T) {
	_, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()
	policyPayload := `{"revision":12,"request_id":"policy-snapshot","replace":true,"policies":[{"principal":"u2:*","up_bps":200},{"principal":"u1:d1","down_bps":100}]}`
	_ = runtimeJSONRequest(t, http.MethodPut, server.URL+"/policy", policyPayload, http.StatusOK)

	snapshotBody := runtimeJSONRequest(t, http.MethodGet, server.URL+"/policy/snapshot", "", http.StatusOK)
	var snapshot struct {
		Revision int64                           `json:"revision"`
		Count    int                             `json:"count"`
		Digest   string                          `json:"digest"`
		Policies []trafficontrol.PrincipalPolicy `json:"policies"`
	}
	if err := json.Unmarshal(snapshotBody, &snapshot); err != nil {
		t.Fatalf("decode policy snapshot: %v", err)
	}
	if snapshot.Revision != 12 || snapshot.Count != 2 || len(snapshot.Digest) != 64 {
		t.Fatalf("unexpected policy snapshot: %+v", snapshot)
	}
	if len(snapshot.Policies) != 2 || snapshot.Policies[0].Principal != "u1:d1" || snapshot.Policies[1].Principal != "u2:*" {
		t.Fatalf("expected sorted policies, got %+v", snapshot.Policies)
	}

	statusBody := getRuntimeStatusBody(t, server.URL)
	var status struct {
		Ready          bool     `json:"ready"`
		Capabilities   []string `json:"capabilities"`
		PolicyRevision int64    `json:"policy_revision"`
		PolicyCount    int      `json:"policy_count"`
		PolicyDigest   string   `json:"policy_digest"`
	}
	if err := json.Unmarshal(statusBody, &status); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if !status.Ready || status.PolicyRevision != snapshot.Revision || status.PolicyCount != snapshot.Count || status.PolicyDigest != snapshot.Digest {
		t.Fatalf("status and snapshot mismatch: status=%+v snapshot=%+v", status, snapshot)
	}
	if !containsString(status.Capabilities, "runtime_disconnect_batch") || !containsString(status.Capabilities, "runtime_users_replace_managed") {
		t.Fatalf("missing runtime v2 capabilities: %v", status.Capabilities)
	}
}

func TestRuntimeDigestFixtures(t *testing.T) {
	users := map[string]adapter.RuntimeUser{
		"u1:d1": {
			Principal: "u1:d1",
			UUID:      "11111111-1111-1111-1111-111111111111",
			Flow:      "xtls-rprx-vision",
		},
	}
	userDigest := digestJSON(canonicalRuntimeUsers("vless-in", users))
	if userDigest != "76767ee449b634a15a6412e7105c5790032af804bc3e39456b2b2eb2c6a88d9d" {
		t.Fatalf("unexpected user digest fixture: %s", userDigest)
	}
	policyDigest := digestPolicies([]trafficontrol.PrincipalPolicy{{
		Principal:      "u1:*",
		MaxConnections: 2,
		UpBPS:          125000,
		DownBPS:        250000,
	}})
	if policyDigest != "a26b9c164e60009f4da9782550771f098f76e2fa391520c18cb4aa1556bbc2ed" {
		t.Fatalf("unexpected policy digest fixture: %s", policyDigest)
	}
}

func TestRuntimeStatsSnapshotUsesPrincipalCumulativeTotals(t *testing.T) {
	trafficManager, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()

	tracker := newFakeTracker("u1:d1")
	tracker.metadata.Upload.Store(1)
	tracker.metadata.Download.Store(2)
	trafficManager.Join(tracker)
	trafficManager.PushUploaded(100)
	trafficManager.PushDownloaded(50)
	trafficManager.PushPrincipalUploaded("u1:d1", 100)
	trafficManager.PushPrincipalDownloaded("u1:d1", 50)

	resp, err := http.Get(server.URL + "/stats/snapshot")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	var body struct {
		UploadTotal    int64 `json:"upload_total"`
		DownloadTotal  int64 `json:"download_total"`
		PrincipalStats []struct {
			Principal string `json:"principal"`
			Active    int    `json:"active"`
			Upload    int64  `json:"upload"`
			Download  int64  `json:"download"`
		} `json:"principal_stats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.UploadTotal != 100 || body.DownloadTotal != 50 {
		t.Fatalf("unexpected totals: %+v", body)
	}

	for _, principalStat := range body.PrincipalStats {
		if principalStat.Principal != "u1:d1" {
			continue
		}
		if principalStat.Active != 1 {
			t.Fatalf("unexpected active count: %+v", principalStat)
		}
		if principalStat.Upload != 100 || principalStat.Download != 50 {
			t.Fatalf("expected principal cumulative totals, got %+v", principalStat)
		}
		return
	}
	t.Fatal("expected u1:d1 principal stats in snapshot")
}

func applyRuntimePayload(t *testing.T, serverURL, payload string) {
	t.Helper()
	_ = runtimeJSONRequest(t, http.MethodPut, serverURL+"/users", payload, http.StatusOK)
}

func getRuntimeStatusBody(t *testing.T, serverURL string) []byte {
	t.Helper()
	return runtimeJSONRequest(t, http.MethodGet, serverURL+"/status", "", http.StatusOK)
}

func runtimeJSONRequest(t *testing.T, method, targetURL, payload string, expectedStatus int) []byte {
	t.Helper()
	var body io.Reader
	if payload != "" {
		body = bytes.NewBufferString(payload)
	}
	req, err := http.NewRequest(method, targetURL, body)
	if err != nil {
		t.Fatalf("create %s request: %v", method, err)
	}
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, targetURL, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", method, err)
	}
	if resp.StatusCode != expectedStatus {
		t.Fatalf("expected status %d, got %d body=%s", expectedStatus, resp.StatusCode, responseBody)
	}
	return responseBody
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

var _ adapter.RuntimeUserInbound = (*fakeRuntimeInbound)(nil)
var _ adapter.InboundManager = (*fakeInboundManager)(nil)
var _ trafficontrol.Tracker = (*fakeTracker)(nil)
