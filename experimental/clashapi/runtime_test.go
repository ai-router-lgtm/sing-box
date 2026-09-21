package clashapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/experimental/clashapi/trafficontrol"
	"github.com/sagernet/sing-box/log"

	"github.com/gofrs/uuid/v5"
)

type fakeRuntimeInbound struct {
	access        sync.RWMutex
	tag           string
	snapshot      []adapter.RuntimeUser
	lastReplace   []adapter.RuntimeUser
	prepareError  error
	prepareCount  int
	commitCount   int
	abortCount    int
	releaseCount  int
	applyStarted  chan struct{}
	applyContinue <-chan struct{}
}

func (f *fakeRuntimeInbound) Start(stage adapter.StartStage) error { return nil }
func (f *fakeRuntimeInbound) Close() error                         { return nil }
func (f *fakeRuntimeInbound) Type() string                         { return "vless" }
func (f *fakeRuntimeInbound) Tag() string                          { return f.tag }

func (f *fakeRuntimeInbound) SnapshotRuntimeUsers() []adapter.RuntimeUser {
	f.access.RLock()
	defer f.access.RUnlock()
	return append([]adapter.RuntimeUser(nil), f.snapshot...)
}

func (f *fakeRuntimeInbound) UpsertRuntimeUsers(users []adapter.RuntimeUser) (int, error) {
	f.access.Lock()
	defer f.access.Unlock()
	current := runtimeUserMap(f.snapshot)
	for principal, runtimeUser := range runtimeUserMap(users) {
		current[principal] = runtimeUser
	}
	f.snapshot = runtimeUserMapValues(current)
	return len(users), nil
}

func (f *fakeRuntimeInbound) DeleteRuntimeUsers(principals []string) (int, error) {
	f.access.Lock()
	defer f.access.Unlock()
	current := runtimeUserMap(f.snapshot)
	deleted := 0
	for _, principal := range principals {
		if _, loaded := current[principal]; loaded {
			delete(current, principal)
			deleted++
		}
	}
	f.snapshot = runtimeUserMapValues(current)
	return deleted, nil
}

func (f *fakeRuntimeInbound) PrepareRuntimeUserReplace(users []adapter.RuntimeUser) (adapter.RuntimeUserReplaceTransaction, error) {
	f.access.Lock()
	f.prepareCount++
	if f.prepareError != nil {
		f.access.Unlock()
		return nil, f.prepareError
	}
	current := runtimeUserMap(f.snapshot)
	desired := runtimeUserMap(users)
	deleted := 0
	for principal := range current {
		if _, loaded := desired[principal]; !loaded {
			deleted++
		}
	}
	return &fakeRuntimeUserReplaceTransaction{
		inbound: f,
		users:   runtimeUserMapValues(desired),
		result:  adapter.RuntimeUserReplaceResult{Upserted: len(desired), Deleted: deleted},
	}, nil
}

type fakeRuntimeUserReplaceTransaction struct {
	inbound *fakeRuntimeInbound
	users   []adapter.RuntimeUser
	result  adapter.RuntimeUserReplaceResult
}

func (t *fakeRuntimeUserReplaceTransaction) Apply() adapter.RuntimeUserReplaceResult {
	if t.inbound.applyStarted != nil {
		close(t.inbound.applyStarted)
	}
	if t.inbound.applyContinue != nil {
		<-t.inbound.applyContinue
	}
	t.inbound.commitCount++
	t.inbound.snapshot = append([]adapter.RuntimeUser(nil), t.users...)
	t.inbound.lastReplace = append([]adapter.RuntimeUser(nil), t.users...)
	return t.result
}

func (t *fakeRuntimeUserReplaceTransaction) Release() {
	t.inbound.releaseCount++
	t.inbound.access.Unlock()
}

func (t *fakeRuntimeUserReplaceTransaction) Abort() {
	t.inbound.abortCount++
	t.inbound.access.Unlock()
}

type nonAtomicRuntimeInbound struct {
	tag string
}

func (f *nonAtomicRuntimeInbound) Start(stage adapter.StartStage) error { return nil }
func (f *nonAtomicRuntimeInbound) Close() error                         { return nil }
func (f *nonAtomicRuntimeInbound) Type() string                         { return "test" }
func (f *nonAtomicRuntimeInbound) Tag() string                          { return f.tag }
func (f *nonAtomicRuntimeInbound) SnapshotRuntimeUsers() []adapter.RuntimeUser {
	return nil
}
func (f *nonAtomicRuntimeInbound) UpsertRuntimeUsers(users []adapter.RuntimeUser) (int, error) {
	return len(users), nil
}
func (f *nonAtomicRuntimeInbound) DeleteRuntimeUsers(principals []string) (int, error) {
	return len(principals), nil
}

type fakeInboundManager struct {
	mu       sync.RWMutex
	inbounds map[string]adapter.Inbound
}

func newFakeInboundManager(inbounds ...adapter.Inbound) *fakeInboundManager {
	result := &fakeInboundManager{inbounds: make(map[string]adapter.Inbound, len(inbounds))}
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
	return &fakeTracker{metadata: trafficontrol.TrackerMetadata{
		ID:       id,
		Metadata: adapter.InboundContext{User: principal},
		Upload:   &atomic.Int64{}, Download: &atomic.Int64{},
	}}
}

func (t *fakeTracker) Metadata() *trafficontrol.TrackerMetadata { return &t.metadata }
func (t *fakeTracker) Close() error {
	t.closed.Store(true)
	t.closes.Add(1)
	return nil
}

type runtimeTestStatus struct {
	ProtocolVersion   string                  `json:"protocol_version"`
	DigestVersion     string                  `json:"digest_version"`
	Ready             bool                    `json:"ready"`
	RuntimeInstanceID string                  `json:"runtime_instance_id"`
	Capabilities      []string                `json:"capabilities"`
	Users             runtimeDomainTestStatus `json:"users"`
	Policy            runtimeDomainTestStatus `json:"policy"`
	Disconnect        runtimeDomainIdentity   `json:"disconnect"`
}

type runtimeDomainTestStatus struct {
	Revision      int64                      `json:"revision"`
	RequestID     string                     `json:"request_id"`
	PayloadDigest string                     `json:"payload_digest"`
	Count         int                        `json:"count"`
	Digest        string                     `json:"digest"`
	Inbounds      []runtimeInboundUserStatus `json:"inbounds"`
}

type runtimeTestServer struct {
	traffic  *trafficontrol.Manager
	server   *httptest.Server
	instance string
}

func newRuntimeTestServer(t *testing.T, inboundManager adapter.InboundManager) *runtimeTestServer {
	t.Helper()
	trafficManager := trafficontrol.NewManager()
	server := httptest.NewServer(runtimeRouter(trafficManager, inboundManager))
	status := fetchRuntimeStatus(t, server.URL)
	return &runtimeTestServer{traffic: trafficManager, server: server, instance: status.RuntimeInstanceID}
}

func (s *runtimeTestServer) Close() { s.server.Close() }

func TestRuntimeStatusAdvertisesOnlyV3AuthorityCapabilities(t *testing.T) {
	t.Parallel()

	first := newRuntimeTestServer(t, newFakeInboundManager(&fakeRuntimeInbound{tag: "vless-in"}))
	defer first.Close()
	second := newRuntimeTestServer(t, newFakeInboundManager(&fakeRuntimeInbound{tag: "vless-in"}))
	defer second.Close()

	status := fetchRuntimeStatus(t, first.server.URL)
	if status.ProtocolVersion != runtimeSchemaVersion || status.DigestVersion != runtimeDigestVersion || !status.Ready {
		t.Fatalf("unexpected runtime status: %+v", status)
	}
	if status.RuntimeInstanceID == "" || status.RuntimeInstanceID == second.instance {
		t.Fatalf("runtime instance IDs must be non-empty and process-local: first=%q second=%q", status.RuntimeInstanceID, second.instance)
	}
	for _, capability := range []string{
		"runtime_users_authoritative_replace",
		"runtime_policy_authoritative_replace",
		"runtime_disconnect_fence",
		"runtime_disconnect_selectors",
		"runtime_disconnect_all",
		"runtime_request_payload_digest",
		"runtime_state_digest",
		"runtime_policy_zero_delete",
	} {
		if !containsString(status.Capabilities, capability) {
			t.Fatalf("missing v3 capability %q in %v", capability, status.Capabilities)
		}
	}
	for _, forbidden := range []string{"runtime_users_replace_managed", "runtime_users_batch_upsert"} {
		if containsString(status.Capabilities, forbidden) {
			t.Fatalf("legacy capability must not be advertised: %s", forbidden)
		}
	}
	if status.Users.Digest != digestJSON([]canonicalRuntimeUser{}) || status.Policy.Digest != digestPolicies(nil) {
		t.Fatalf("empty state digests are not canonical: %+v", status)
	}
}

func TestRuntimeRejectsLegacyMutationShapes(t *testing.T) {
	t.Parallel()

	inbound := &fakeRuntimeInbound{tag: "vless-in"}
	harness := newRuntimeTestServer(t, newFakeInboundManager(inbound))
	defer harness.Close()

	legacyUsers := `{"revision":1,"request_id":"legacy","operations":[{"inbound":"vless-in","upsert":[]}]}`
	body := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", legacyUsers, http.StatusBadRequest)
	if codeFromBody(t, body) == "" || inbound.commitCount != 0 {
		t.Fatalf("legacy users request was not rejected safely: body=%s commits=%d", body, inbound.commitCount)
	}

	request := applyUsersRequest{
		runtimeEnvelope: runtimeEnvelope{SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: harness.instance, Revision: 1, RequestID: "replace-managed"},
		Mode:            "delta", ReplaceManaged: json.RawMessage("true"),
		Operations: []usersOperation{{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{}}},
	}
	request.PayloadDigest = digestJSON(canonicalUsersAction{
		SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: harness.instance, Revision: 1, Mode: "delta",
		Operations: []usersOperation{{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{}, Delete: []string{}}},
	})
	body = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, request), http.StatusBadRequest)
	if !strings.Contains(string(body), "replace_managed") {
		t.Fatalf("replace_managed rejection is not explicit: %s", body)
	}

	legacyPolicy := fmt.Sprintf(`{"schema_version":%q,"expected_runtime_instance_id":%q,"revision":1,"request_id":"legacy-policy","payload_digest":"%s","mode":"delta","replace":true,"policies":[]}`,
		runtimeSchemaVersion, harness.instance, strings.Repeat("0", 64))
	body = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/policy", legacyPolicy, http.StatusBadRequest)
	if !strings.Contains(string(body), "replace is not supported") {
		t.Fatalf("replace rejection is not explicit: %s", body)
	}

	nullReplaceManaged := fmt.Sprintf(`{"schema_version":%q,"expected_runtime_instance_id":%q,"revision":1,"request_id":"null-replace-managed","payload_digest":"%s","mode":"delta","replace_managed":null,"operations":[{"inbound":"vless-in","upsert":[],"delete":[]}]}`,
		runtimeSchemaVersion, harness.instance, strings.Repeat("0", 64))
	body = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", nullReplaceManaged, http.StatusBadRequest)
	if !strings.Contains(string(body), "replace_managed is not supported") {
		t.Fatalf("replace_managed:null must still be rejected explicitly: %s", body)
	}

	nullReplacePolicy := fmt.Sprintf(`{"schema_version":%q,"expected_runtime_instance_id":%q,"revision":1,"request_id":"null-replace","payload_digest":"%s","mode":"delta","replace":null,"policies":[]}`,
		runtimeSchemaVersion, harness.instance, strings.Repeat("0", 64))
	body = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/policy", nullReplacePolicy, http.StatusBadRequest)
	if !strings.Contains(string(body), "replace is not supported") {
		t.Fatalf("replace:null must still be rejected explicitly: %s", body)
	}
}

func TestRuntimeUsersAuthoritativeReplacePreservesStaticAndCoversAllInbounds(t *testing.T) {
	t.Parallel()

	staticUUID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	first := &fakeRuntimeInbound{tag: "vless-in", snapshot: []adapter.RuntimeUser{{Principal: "static-admin", UUID: staticUUID}}}
	second := &fakeRuntimeInbound{tag: "vmess-in"}
	harness := newRuntimeTestServer(t, newFakeInboundManager(second, first))
	defer harness.Close()

	request := usersRequest(t, harness.instance, 10, "users-recovery-10", "authoritative_replace", "all_runtime_inbounds", []usersOperation{
		{Inbound: "vmess-in", Upsert: []adapter.RuntimeUser{}},
		{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{{Principal: "u1:d1", UUID: "11111111-1111-1111-1111-111111111111"}}},
	})
	body := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, request), http.StatusOK)
	var response runtimeDomainTestStatus
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode users response: %v", err)
	}
	if response.Revision != 10 || response.RequestID != "users-recovery-10" || response.Count != 1 || len(response.Digest) != 64 {
		t.Fatalf("unexpected users response: %s", body)
	}
	if first.commitCount != 1 || second.commitCount != 1 {
		t.Fatalf("each inbound must commit once: first=%d second=%d", first.commitCount, second.commitCount)
	}
	actual := runtimeUserMap(first.lastReplace)
	if actual["static-admin"].UUID != staticUUID || actual["u1:d1"].UUID == "" {
		t.Fatalf("static and managed users were not merged correctly: %+v", actual)
	}
	status := fetchRuntimeStatus(t, harness.server.URL)
	if status.Users.Revision != 10 || status.Users.Count != 1 || len(status.Users.Inbounds) != 2 {
		t.Fatalf("status did not expose actual managed state: %+v", status.Users)
	}
	statusBody := runtimeJSONRequest(t, http.MethodGet, harness.server.URL+"/status", "", http.StatusOK)
	for _, secret := range []string{staticUUID, "11111111-1111-1111-1111-111111111111"} {
		if bytes.Contains(statusBody, []byte(secret)) {
			t.Fatalf("runtime status leaked credential %s", secret)
		}
	}

	omitted := usersRequest(t, harness.instance, 11, "users-omitted", "authoritative_replace", "all_runtime_inbounds", []usersOperation{
		{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{}},
	})
	body = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, omitted), http.StatusUnprocessableEntity)
	if codeFromBody(t, body) != runtimeErrorUnsupportedInbound || first.commitCount != 1 || second.commitCount != 1 {
		t.Fatalf("omitted inbound must fail before commit: body=%s first=%d second=%d", body, first.commitCount, second.commitCount)
	}

	empty := usersRequest(t, harness.instance, 12, "users-drain-12", "authoritative_replace", "all_runtime_inbounds", []usersOperation{
		{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{}},
		{Inbound: "vmess-in", Upsert: []adapter.RuntimeUser{}},
	})
	_ = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, empty), http.StatusOK)
	status = fetchRuntimeStatus(t, harness.server.URL)
	if status.Users.Revision != 12 || status.Users.Count != 0 || status.Users.Digest != digestJSON([]canonicalRuntimeUser{}) {
		t.Fatalf("authoritative empty snapshot did not clear controlled state: %+v", status.Users)
	}
	actual = runtimeUserMap(first.lastReplace)
	if len(actual) != 1 || actual["static-admin"].UUID != staticUUID || len(second.lastReplace) != 0 {
		t.Fatalf("empty controlled snapshot did not preserve only static users: first=%+v second=%+v", actual, second.lastReplace)
	}
}

func TestRuntimeUsersMultiInboundPrepareFailureHasNoPartialCommit(t *testing.T) {
	t.Parallel()

	first := &fakeRuntimeInbound{tag: "a-in"}
	second := &fakeRuntimeInbound{tag: "b-in", prepareError: errors.New("invalid credential")}
	harness := newRuntimeTestServer(t, newFakeInboundManager(first, second))
	defer harness.Close()

	request := usersRequest(t, harness.instance, 1, "atomic-failure", "authoritative_replace", "all_runtime_inbounds", []usersOperation{
		{Inbound: "b-in", Upsert: []adapter.RuntimeUser{{Principal: "u2:d1", UUID: "22222222-2222-2222-2222-222222222222"}}},
		{Inbound: "a-in", Upsert: []adapter.RuntimeUser{{Principal: "u1:d1", UUID: "11111111-1111-1111-1111-111111111111"}}},
	})
	body := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, request), http.StatusUnprocessableEntity)
	if codeFromBody(t, body) != "invalid_request" {
		t.Fatalf("unexpected prepare error: %s", body)
	}
	if first.prepareCount != 1 || first.abortCount != 1 || first.commitCount != 0 || second.commitCount != 0 {
		t.Fatalf("prepared inbounds were not aborted without commit: first=%+v second=%+v", first, second)
	}
	status := fetchRuntimeStatus(t, harness.server.URL)
	if status.Users.Revision != 0 || status.Users.Count != 0 {
		t.Fatalf("failed multi-inbound replace advanced users state: %+v", status.Users)
	}
}

func TestRuntimeUsersMultiInboundAppliesAllBeforeReleasingReaders(t *testing.T) {
	t.Parallel()

	secondApplyStarted := make(chan struct{})
	continueSecondApply := make(chan struct{})
	first := &fakeRuntimeInbound{tag: "a-in"}
	second := &fakeRuntimeInbound{
		tag: "b-in", applyStarted: secondApplyStarted, applyContinue: continueSecondApply,
	}
	harness := newRuntimeTestServer(t, newFakeInboundManager(first, second))
	defer harness.Close()
	request := usersRequest(t, harness.instance, 1, "atomic-release", "authoritative_replace", "all_runtime_inbounds", []usersOperation{
		{Inbound: "a-in", Upsert: []adapter.RuntimeUser{{Principal: "u1:d1", UUID: "11111111-1111-1111-1111-111111111111"}}},
		{Inbound: "b-in", Upsert: []adapter.RuntimeUser{{Principal: "u2:d1", UUID: "22222222-2222-2222-2222-222222222222"}}},
	})
	payload := mustJSON(t, request)

	type httpResult struct {
		status int
		body   []byte
		err    error
	}
	httpResultChannel := make(chan httpResult, 1)
	go func() {
		httpRequest, err := http.NewRequest(http.MethodPut, harness.server.URL+"/users", strings.NewReader(payload))
		if err != nil {
			httpResultChannel <- httpResult{err: err}
			return
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(httpRequest)
		if err != nil {
			httpResultChannel <- httpResult{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		httpResultChannel <- httpResult{status: response.StatusCode, body: body, err: err}
	}()

	select {
	case <-secondApplyStarted:
	case <-time.After(2 * time.Second):
		close(continueSecondApply)
		t.Fatal("second inbound did not enter apply")
	}
	readerDone := make(chan []adapter.RuntimeUser, 1)
	go func() { readerDone <- first.SnapshotRuntimeUsers() }()
	select {
	case users := <-readerDone:
		close(continueSecondApply)
		t.Fatalf("first inbound became observable before all inbounds applied: %+v", users)
	case <-time.After(100 * time.Millisecond):
	}
	close(continueSecondApply)

	result := <-httpResultChannel
	if result.err != nil || result.status != http.StatusOK {
		t.Fatalf("authoritative replace failed: status=%d err=%v body=%s", result.status, result.err, result.body)
	}
	select {
	case users := <-readerDone:
		if len(users) != 1 || users[0].Principal != "u1:d1" {
			t.Fatalf("reader did not observe the final snapshot: %+v", users)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first inbound reader remained blocked after release")
	}
	if first.releaseCount != 1 || second.releaseCount != 1 || first.commitCount != 1 || second.commitCount != 1 {
		t.Fatalf("unexpected three-phase counts: first=%+v second=%+v", first, second)
	}
}

func TestRuntimeUsersIdempotencyAndConflictMatrix(t *testing.T) {
	t.Parallel()

	inbound := &fakeRuntimeInbound{tag: "vless-in"}
	harness := newRuntimeTestServer(t, newFakeInboundManager(inbound))
	defer harness.Close()

	base := usersRequest(t, harness.instance, 10, "users-10", "delta", "", []usersOperation{{
		Inbound: "vless-in", Upsert: []adapter.RuntimeUser{{Principal: "u1:d1", UUID: "11111111-1111-1111-1111-111111111111"}},
	}})
	firstBody := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, base), http.StatusOK)
	secondBody := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, base), http.StatusOK)
	if !boolFromBody(t, secondBody, "idempotent") || inbound.commitCount != 1 {
		t.Fatalf("same request retry was not idempotent: first=%s second=%s commits=%d", firstBody, secondBody, inbound.commitCount)
	}

	alias := base
	alias.RequestID = "users-10-alias"
	aliasBody := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, alias), http.StatusOK)
	if !boolFromBody(t, aliasBody, "idempotent") || stringFromBody(t, aliasBody, "digest") == "" || inbound.commitCount != 1 {
		t.Fatalf("equal revision/digest alias did not return the complete result: %s", aliasBody)
	}

	requestConflict := usersRequest(t, harness.instance, 11, "users-10", "delta", "", []usersOperation{{
		Inbound: "vless-in", Upsert: []adapter.RuntimeUser{{Principal: "u2:d1", UUID: "22222222-2222-2222-2222-222222222222"}},
	}})
	assertRuntimeError(t, harness.server.URL+"/users", mustJSON(t, requestConflict), http.StatusConflict, runtimeErrorRequestConflict)

	revisionConflict := usersRequest(t, harness.instance, 10, "users-revision-conflict", "delta", "", []usersOperation{{
		Inbound: "vless-in", Delete: []string{"u1:d1"},
	}})
	conflictBody := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, revisionConflict), http.StatusConflict)
	if codeFromBody(t, conflictBody) != runtimeErrorRevisionConflict || intFromBody(t, conflictBody, "current_revision") != 10 || stringFromBody(t, conflictBody, "current_request_id") != "users-10" {
		t.Fatalf("conflict evidence did not expose current domain identity: %s", conflictBody)
	}

	stale := usersRequest(t, harness.instance, 9, "users-stale", "delta", "", []usersOperation{{Inbound: "vless-in", Delete: []string{"u1:d1"}}})
	assertRuntimeError(t, harness.server.URL+"/users", mustJSON(t, stale), http.StatusConflict, runtimeErrorStaleRevision)

	digestMismatch := usersRequest(t, harness.instance, 11, "users-digest", "delta", "", []usersOperation{{Inbound: "vless-in", Delete: []string{"u1:d1"}}})
	digestMismatch.PayloadDigest = strings.Repeat("0", 64)
	assertRuntimeError(t, harness.server.URL+"/users", mustJSON(t, digestMismatch), http.StatusUnprocessableEntity, runtimeErrorDigestMismatch)

	wrongInstance := usersRequest(t, "old-runtime-instance", 11, "users-old-instance", "delta", "", []usersOperation{{Inbound: "vless-in", Delete: []string{"u1:d1"}}})
	assertRuntimeError(t, harness.server.URL+"/users", mustJSON(t, wrongInstance), http.StatusConflict, runtimeErrorInstanceMismatch)

	unsupported := usersRequest(t, harness.instance, 11, "users-missing", "delta", "", []usersOperation{{Inbound: "missing", Delete: []string{"u1:d1"}}})
	assertRuntimeError(t, harness.server.URL+"/users", mustJSON(t, unsupported), http.StatusUnprocessableEntity, runtimeErrorUnsupportedInbound)

	status := fetchRuntimeStatus(t, harness.server.URL)
	if status.Users.Revision != 10 || status.Users.RequestID != "users-10" || inbound.commitCount != 1 {
		t.Fatalf("failed/idempotent requests changed the applied identity: status=%+v commits=%d", status.Users, inbound.commitCount)
	}
}

func TestRuntimePolicyAuthoritativeAndZeroDelete(t *testing.T) {
	t.Parallel()

	harness := newRuntimeTestServer(t, newFakeInboundManager(&fakeRuntimeInbound{tag: "vless-in"}))
	defer harness.Close()

	replace := policyRequest(t, harness.instance, 20, "policy-20", "authoritative_replace", []trafficontrol.PrincipalPolicy{
		{Principal: "u2:*", UpBPS: 200}, {Principal: "u1:d1", DownBPS: 100},
	})
	body := runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/policy", mustJSON(t, replace), http.StatusOK)
	if intFromBody(t, body, "count") != 2 {
		t.Fatalf("unexpected policy replace response: %s", body)
	}

	zeroDelete := policyRequest(t, harness.instance, 21, "policy-21", "delta", []trafficontrol.PrincipalPolicy{{Principal: "u1:d1"}})
	_ = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/policy", mustJSON(t, zeroDelete), http.StatusOK)
	if _, loaded := harness.traffic.PolicyForPrincipal("u1:d1"); loaded {
		t.Fatal("all-zero policy delta did not delete the principal")
	}
	status := fetchRuntimeStatus(t, harness.server.URL)
	if status.Policy.Revision != 21 || status.Policy.RequestID != "policy-21" || status.Policy.Count != 1 {
		t.Fatalf("unexpected policy status after zero-delete: %+v", status.Policy)
	}
	snapshotBody := runtimeJSONRequest(t, http.MethodGet, harness.server.URL+"/policy/snapshot", "", http.StatusOK)
	if intFromBody(t, snapshotBody, "count") != 1 || stringFromBody(t, snapshotBody, "payload_digest") != status.Policy.PayloadDigest {
		t.Fatalf("policy snapshot identity does not match status: snapshot=%s status=%+v", snapshotBody, status.Policy)
	}
}

func TestRuntimeDisconnectFenceSelectorsAllAndIdempotency(t *testing.T) {
	t.Parallel()

	inbound := &fakeRuntimeInbound{tag: "vless-in"}
	harness := newRuntimeTestServer(t, newFakeInboundManager(inbound))
	defer harness.Close()

	users := usersRequest(t, harness.instance, 30, "users-30", "delta", "", []usersOperation{{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{}}})
	_ = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, users), http.StatusOK)
	policy := policyRequest(t, harness.instance, 30, "policy-30", "authoritative_replace", nil)
	_ = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/policy", mustJSON(t, policy), http.StatusOK)

	user := newFakeTracker("u1:d1")
	exact := newFakeTracker("u2:d1")
	other := newFakeTracker("u3:d1")
	for _, tracker := range []*fakeTracker{user, exact, other} {
		harness.traffic.Join(tracker)
	}
	disconnect := disconnectRequest(t, harness.instance, 30, "disconnect-30", runtimeFence{UsersRevision: 30, PolicyRevision: 30}, &runtimeSelectors{
		UserIDs: []string{"u1", "u1"}, Principals: []string{"u2:d1", "u2:d1"},
	}, false)
	body := runtimeJSONRequest(t, http.MethodPost, harness.server.URL+"/disconnect", mustJSON(t, disconnect), http.StatusOK)
	if intFromBody(t, body, "selector_count") != 2 || intFromBody(t, body, "disconnected") != 2 || other.closed.Load() {
		t.Fatalf("unexpected selector disconnect: %s", body)
	}

	late := newFakeTracker("u1:d2")
	harness.traffic.Join(late)
	replay := runtimeJSONRequest(t, http.MethodPost, harness.server.URL+"/disconnect", mustJSON(t, disconnect), http.StatusOK)
	if !boolFromBody(t, replay, "idempotent") || late.closed.Load() {
		t.Fatalf("idempotent disconnect rescanned connections: %s", replay)
	}

	unreached := disconnectRequest(t, harness.instance, 31, "disconnect-unreached", runtimeFence{UsersRevision: 31, PolicyRevision: 31}, nil, true)
	assertRuntimeError(t, harness.server.URL+"/disconnect", mustJSON(t, unreached), http.StatusServiceUnavailable, runtimeErrorNotReady)
	if other.closed.Load() || late.closed.Load() {
		t.Fatal("unreached fence disconnected connections")
	}

	users31 := usersRequest(t, harness.instance, 31, "users-31", "delta", "", []usersOperation{{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{}}})
	_ = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/users", mustJSON(t, users31), http.StatusOK)
	policy31 := policyRequest(t, harness.instance, 31, "policy-31", "authoritative_replace", nil)
	_ = runtimeJSONRequest(t, http.MethodPut, harness.server.URL+"/policy", mustJSON(t, policy31), http.StatusOK)
	all := disconnectRequest(t, harness.instance, 31, "disconnect-all-31", runtimeFence{UsersRevision: 31, PolicyRevision: 31}, nil, true)
	body = runtimeJSONRequest(t, http.MethodPost, harness.server.URL+"/disconnect", mustJSON(t, all), http.StatusOK)
	if intFromBody(t, body, "disconnected") != 4 || !other.closed.Load() || !late.closed.Load() {
		t.Fatalf("disconnect all did not close every point-in-time tracker: %s", body)
	}
	status := fetchRuntimeStatus(t, harness.server.URL)
	if status.Disconnect.Revision != 31 || status.Disconnect.RequestID != "disconnect-all-31" {
		t.Fatalf("disconnect identity missing from status: %+v", status.Disconnect)
	}
}

func TestRuntimeDisconnectRejectsAmbiguousAndOversizedScopes(t *testing.T) {
	t.Parallel()

	harness := newRuntimeTestServer(t, newFakeInboundManager())
	defer harness.Close()
	ambiguous := disconnectPrincipalRequest{
		runtimeEnvelope: runtimeEnvelope{
			SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: harness.instance, Revision: 1,
			RequestID: "ambiguous", PayloadDigest: strings.Repeat("0", 64),
		},
		Fence: runtimeFence{}, Selectors: &runtimeSelectors{Principals: []string{"u1:d1"}}, All: true,
	}
	body := runtimeJSONRequest(t, http.MethodPost, harness.server.URL+"/disconnect", mustJSON(t, ambiguous), http.StatusBadRequest)
	if codeFromBody(t, body) != "invalid_request" {
		t.Fatalf("ambiguous disconnect scope was not rejected: %s", body)
	}

	principals := make([]string, maxRuntimeDisconnectSelectors+1)
	for index := range principals {
		principals[index] = fmt.Sprintf("u%d:d1", index)
	}
	oversized := ambiguous
	oversized.RequestID = "oversized"
	oversized.All = false
	oversized.Selectors = &runtimeSelectors{Principals: principals}
	body = runtimeJSONRequest(t, http.MethodPost, harness.server.URL+"/disconnect", mustJSON(t, oversized), http.StatusBadRequest)
	if codeFromBody(t, body) != "invalid_request" || !strings.Contains(string(body), "too many selectors") {
		t.Fatalf("oversized disconnect scope was not rejected: %s", body)
	}
}

func TestRuntimeNotReadyDoesNotAdvanceState(t *testing.T) {
	t.Parallel()

	state := newRuntimeState(trafficontrol.NewManager(), nil)
	server := httptest.NewServer(runtimeRouter(state.traffic, nil))
	defer server.Close()
	status := fetchRuntimeStatus(t, server.URL)
	request := usersRequest(t, status.RuntimeInstanceID, 1, "not-ready", "delta", "", []usersOperation{{Inbound: "missing"}})
	assertRuntimeError(t, server.URL+"/users", mustJSON(t, request), http.StatusServiceUnavailable, runtimeErrorNotReady)
	status = fetchRuntimeStatus(t, server.URL)
	if status.Users.Revision != 0 {
		t.Fatalf("not-ready request advanced users state: %+v", status.Users)
	}
}

func TestRuntimeUnsupportedNonAtomicInbound(t *testing.T) {
	t.Parallel()

	inbound := &nonAtomicRuntimeInbound{tag: "legacy-in"}
	harness := newRuntimeTestServer(t, newFakeInboundManager(inbound))
	defer harness.Close()
	if status := fetchRuntimeStatus(t, harness.server.URL); status.Ready {
		t.Fatalf("Runtime with non-atomic user inbound must not advertise ready: %+v", status)
	}
	request := usersRequest(t, harness.instance, 1, "unsupported-atomic", "authoritative_replace", "all_runtime_inbounds", []usersOperation{{Inbound: "legacy-in"}})
	assertRuntimeError(t, harness.server.URL+"/users", mustJSON(t, request), http.StatusServiceUnavailable, runtimeErrorNotReady)
}

func TestRuntimeWithNoControllableInboundIsNotReady(t *testing.T) {
	t.Parallel()

	harness := newRuntimeTestServer(t, newFakeInboundManager())
	defer harness.Close()
	status := fetchRuntimeStatus(t, harness.server.URL)
	if status.Ready {
		t.Fatalf("Runtime without a controllable inbound must not advertise ready: %+v", status)
	}
	request := policyRequest(t, harness.instance, 1, "no-inbound-policy", "authoritative_replace", nil)
	assertRuntimeError(t, harness.server.URL+"/policy", mustJSON(t, request), http.StatusServiceUnavailable, runtimeErrorNotReady)
}

func TestRuntimeCanonicalDigestFixtures(t *testing.T) {
	t.Parallel()

	first := applyUsersRequest{
		runtimeEnvelope: runtimeEnvelope{SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: "instance-1", Revision: 7},
		Mode:            "authoritative_replace", Scope: "all_runtime_inbounds",
		Operations: []usersOperation{
			{Inbound: "vmess-in", Upsert: []adapter.RuntimeUser{}},
			{Inbound: "vless-in", Upsert: []adapter.RuntimeUser{
				{Principal: "u2:d1", UUID: "22222222-2222-2222-2222-222222222222"},
				{Principal: "u1:d1", UUID: "11111111-1111-1111-1111-111111111111", Flow: "xtls-rprx-vision"},
			}},
		},
	}
	second := first
	second.Operations = []usersOperation{first.Operations[1], first.Operations[0]}
	second.Operations[0].Upsert = []adapter.RuntimeUser{first.Operations[1].Upsert[1], first.Operations[1].Upsert[0]}
	firstCanonical, err := normalizeUsersAction(first)
	if err != nil {
		t.Fatal(err)
	}
	secondCanonical, err := normalizeUsersAction(second)
	if err != nil {
		t.Fatal(err)
	}
	firstDigest := digestJSON(firstCanonical)
	secondDigest := digestJSON(secondCanonical)
	if firstDigest != secondDigest {
		t.Fatalf("canonical digest depends on operation/user order: first=%s second=%s", firstDigest, secondDigest)
	}
	const expectedUsersDigest = "302fa86563ab4b2a241d7ed8a6f408df0e2df88f58d9bb4ba769861385d9553a"
	const expectedUsersCanonicalJSON = `{"schema_version":"runtime-control/v3","expected_runtime_instance_id":"instance-1","revision":7,"mode":"authoritative_replace","scope":"all_runtime_inbounds","operations":[{"inbound":"vless-in","upsert":[{"principal":"u1:d1","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"},{"principal":"u2:d1","uuid":"22222222-2222-2222-2222-222222222222"}],"delete":[]},{"inbound":"vmess-in","upsert":[],"delete":[]}]}`
	if canonicalJSON := string(marshalRuntimeJSON(firstCanonical)); canonicalJSON != expectedUsersCanonicalJSON {
		t.Fatalf("unexpected users canonical JSON: got=%s want=%s", canonicalJSON, expectedUsersCanonicalJSON)
	}
	if firstDigest != expectedUsersDigest {
		t.Fatalf("unexpected users wire fixture digest: got=%s want=%s", firstDigest, expectedUsersDigest)
	}
	policyCanonical, err := normalizePolicyAction(applyPolicyRequest{
		runtimeEnvelope: runtimeEnvelope{SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: "instance-1", Revision: 7},
		Mode:            "authoritative_replace",
		Policies: []trafficontrol.PrincipalPolicy{
			{Principal: "u2:*", UpBPS: 2_000_000},
			{Principal: "u1:d1", MaxConnections: 2, DownBPS: 1_000_000},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	const expectedPolicyDigest = "02716874df2c98fa89802165716622dda73437614093472279cb3111b0cacf82"
	const expectedPolicyCanonicalJSON = `{"schema_version":"runtime-control/v3","expected_runtime_instance_id":"instance-1","revision":7,"mode":"authoritative_replace","policies":[{"principal":"u1:d1","max_connections":2,"up_bps":0,"down_bps":1000000},{"principal":"u2:*","max_connections":0,"up_bps":2000000,"down_bps":0}]}`
	if canonicalJSON := string(marshalRuntimeJSON(policyCanonical)); canonicalJSON != expectedPolicyCanonicalJSON {
		t.Fatalf("unexpected policy canonical JSON: got=%s want=%s", canonicalJSON, expectedPolicyCanonicalJSON)
	}
	if policyDigest := digestJSON(policyCanonical); policyDigest != expectedPolicyDigest {
		t.Fatalf("unexpected policy wire fixture digest: got=%s want=%s", policyDigest, expectedPolicyDigest)
	}
	disconnectCanonical, err := normalizeDisconnectAction(disconnectPrincipalRequest{
		runtimeEnvelope: runtimeEnvelope{SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: "instance-1", Revision: 7},
		Fence:           runtimeFence{UsersRevision: 7, PolicyRevision: 7},
		Selectors:       &runtimeSelectors{Principals: []string{"u2:d1", "u1:d1"}, UserIDs: []string{"u3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const expectedDisconnectDigest = "43f50d4edbb04014e18101d235c557cd0c400950b0df64cbad36cf807714cde4"
	const expectedDisconnectCanonicalJSON = `{"schema_version":"runtime-control/v3","expected_runtime_instance_id":"instance-1","revision":7,"fence":{"users_revision":7,"policy_revision":7},"selectors":{"principals":["u1:d1","u2:d1"],"user_ids":["u3"]}}`
	if canonicalJSON := string(marshalRuntimeJSON(disconnectCanonical)); canonicalJSON != expectedDisconnectCanonicalJSON {
		t.Fatalf("unexpected disconnect canonical JSON: got=%s want=%s", canonicalJSON, expectedDisconnectCanonicalJSON)
	}
	if disconnectDigest := digestJSON(disconnectCanonical); disconnectDigest != expectedDisconnectDigest {
		t.Fatalf("unexpected disconnect wire fixture digest: got=%s want=%s", disconnectDigest, expectedDisconnectDigest)
	}
	if digestPolicies(nil) != "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945" {
		t.Fatalf("empty policy digest must be SHA-256 of []")
	}
}

func TestRuntimeStatsSnapshotUsesPrincipalCumulativeTotals(t *testing.T) {
	t.Parallel()

	harness := newRuntimeTestServer(t, newFakeInboundManager())
	defer harness.Close()
	tracker := newFakeTracker("u1:d1")
	tracker.metadata.Upload.Store(1)
	tracker.metadata.Download.Store(2)
	harness.traffic.Join(tracker)
	harness.traffic.PushUploaded(100)
	harness.traffic.PushDownloaded(50)
	harness.traffic.PushPrincipalUploaded("u1:d1", 100)
	harness.traffic.PushPrincipalDownloaded("u1:d1", 50)

	body := runtimeJSONRequest(t, http.MethodGet, harness.server.URL+"/stats/snapshot", "", http.StatusOK)
	var response struct {
		UploadTotal    int64                             `json:"upload_total"`
		DownloadTotal  int64                             `json:"download_total"`
		PrincipalStats []trafficontrol.PrincipalSnapshot `json:"principal_stats"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if response.UploadTotal != 100 || response.DownloadTotal != 50 {
		t.Fatalf("unexpected totals: %+v", response)
	}
	for _, principal := range response.PrincipalStats {
		if principal.Principal == "u1:d1" && principal.Active == 1 && principal.Upload == 100 && principal.Download == 50 {
			return
		}
	}
	t.Fatalf("principal cumulative totals missing: %+v", response.PrincipalStats)
}

func usersRequest(t *testing.T, instance string, revision int64, requestID, mode, scope string, operations []usersOperation) applyUsersRequest {
	t.Helper()
	request := applyUsersRequest{
		runtimeEnvelope: runtimeEnvelope{
			SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: instance, Revision: revision, RequestID: requestID,
		},
		Mode: mode, Scope: scope, Operations: operations,
	}
	canonical, err := normalizeUsersAction(request)
	if err != nil {
		t.Fatalf("normalize users fixture: %v", err)
	}
	request.PayloadDigest = digestJSON(canonical)
	return request
}

func policyRequest(t *testing.T, instance string, revision int64, requestID, mode string, policies []trafficontrol.PrincipalPolicy) applyPolicyRequest {
	t.Helper()
	request := applyPolicyRequest{
		runtimeEnvelope: runtimeEnvelope{
			SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: instance, Revision: revision, RequestID: requestID,
		},
		Mode: mode, Policies: policies,
	}
	canonical, err := normalizePolicyAction(request)
	if err != nil {
		t.Fatalf("normalize policy fixture: %v", err)
	}
	request.PayloadDigest = digestJSON(canonical)
	return request
}

func disconnectRequest(t *testing.T, instance string, revision int64, requestID string, fence runtimeFence, selectors *runtimeSelectors, all bool) disconnectPrincipalRequest {
	t.Helper()
	request := disconnectPrincipalRequest{
		runtimeEnvelope: runtimeEnvelope{
			SchemaVersion: runtimeSchemaVersion, ExpectedRuntimeInstanceID: instance, Revision: revision, RequestID: requestID,
		},
		Fence: fence, Selectors: selectors, All: all,
	}
	canonical, err := normalizeDisconnectAction(request)
	if err != nil {
		t.Fatalf("normalize disconnect fixture: %v", err)
	}
	request.PayloadDigest = digestJSON(canonical)
	return request
}

func fetchRuntimeStatus(t *testing.T, serverURL string) runtimeTestStatus {
	t.Helper()
	body := runtimeJSONRequest(t, http.MethodGet, serverURL+"/status", "", http.StatusOK)
	var status runtimeTestStatus
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("decode runtime status: %v body=%s", err, body)
	}
	return status
}

func assertRuntimeError(t *testing.T, targetURL, payload string, expectedStatus int, expectedCode string) {
	t.Helper()
	method := http.MethodPut
	if strings.HasSuffix(targetURL, "/disconnect") {
		method = http.MethodPost
	}
	body := runtimeJSONRequest(t, method, targetURL, payload, expectedStatus)
	if code := codeFromBody(t, body); code != expectedCode {
		t.Fatalf("expected runtime error %q, got %q body=%s", expectedCode, code, body)
	}
}

func codeFromBody(t *testing.T, body []byte) string {
	t.Helper()
	return stringFromBody(t, body, "code")
}

func stringFromBody(t *testing.T, body []byte, key string) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode response: %v body=%s", err, body)
	}
	result, _ := value[key].(string)
	return result
}

func boolFromBody(t *testing.T, body []byte, key string) bool {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode response: %v body=%s", err, body)
	}
	result, _ := value[key].(bool)
	return result
}

func intFromBody(t *testing.T, body []byte, key string) int {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(body, &value); err != nil {
		t.Fatalf("decode response: %v body=%s", err, body)
	}
	result, _ := value[key].(float64)
	return int(result)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return string(body)
}

func runtimeJSONRequest(t *testing.T, method, targetURL, payload string, expectedStatus int) []byte {
	t.Helper()
	var body io.Reader
	if payload != "" {
		body = bytes.NewBufferString(payload)
	}
	request, err := http.NewRequest(method, targetURL, body)
	if err != nil {
		t.Fatalf("create %s request: %v", method, err)
	}
	if payload != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request %s %s failed: %v", method, targetURL, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s response: %v", method, err)
	}
	if response.StatusCode != expectedStatus {
		t.Fatalf("expected status %d, got %d body=%s", expectedStatus, response.StatusCode, responseBody)
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

var (
	_ adapter.AtomicRuntimeUserInbound = (*fakeRuntimeInbound)(nil)
	_ adapter.RuntimeUserInbound       = (*nonAtomicRuntimeInbound)(nil)
	_ adapter.InboundManager           = (*fakeInboundManager)(nil)
	_ trafficontrol.Tracker            = (*fakeTracker)(nil)
)
