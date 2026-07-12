package trafficontrol

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"

	"github.com/gofrs/uuid/v5"
)

type fakeTracker struct {
	metadata TrackerMetadata
	closed   atomic.Bool
}

func newFakeTracker(principal string) *fakeTracker {
	id, _ := uuid.NewV4()
	return &fakeTracker{
		metadata: TrackerMetadata{
			ID: id,
			Metadata: adapter.InboundContext{
				User: principal,
			},
			Upload:   &atomic.Int64{},
			Download: &atomic.Int64{},
		},
	}
}

func (t *fakeTracker) Metadata() *TrackerMetadata {
	return &t.metadata
}

func (t *fakeTracker) Close() error {
	t.closed.Store(true)
	return nil
}

func findPrincipalSnapshot(snapshots []PrincipalSnapshot, principal string) *PrincipalSnapshot {
	for i := range snapshots {
		if snapshots[i].Principal == principal {
			return &snapshots[i]
		}
	}
	return nil
}

func TestApplyPolicyRevisionRejectStale(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	ok := manager.ApplyPolicyRevision(10, true, []PrincipalPolicy{
		{
			Principal:      "u1:d1",
			MaxConnections: 2,
		},
	})
	if !ok {
		t.Fatal("expected first policy revision applied")
	}

	ok = manager.ApplyPolicyRevision(9, true, []PrincipalPolicy{
		{
			Principal:      "u1:d1",
			MaxConnections: 1,
		},
	})
	if ok {
		t.Fatal("expected stale policy revision rejected")
	}

	policy, found := manager.PolicyForPrincipal("u1:d1")
	if !found {
		t.Fatal("expected policy present after stale rejection")
	}
	if policy.MaxConnections != 2 {
		t.Fatalf("unexpected max connections after stale rejection: %d", policy.MaxConnections)
	}
}

func TestJoinRejectWhenMaxConnectionsReached(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.ApplyPolicyRevision(1, true, []PrincipalPolicy{
		{
			Principal:      "u1:d1",
			MaxConnections: 1,
		},
	})

	first := newFakeTracker("u1:d1")
	if !manager.Join(first) {
		t.Fatal("expected first join accepted")
	}
	if first.closed.Load() {
		t.Fatal("first tracker should not be closed")
	}

	second := newFakeTracker("u1:d1")
	if manager.Join(second) {
		t.Fatal("expected second join rejected")
	}
	if !second.closed.Load() {
		t.Fatal("second tracker should be closed on rejection")
	}
}

func TestDisconnectUserMatchesPrefix(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	u := newFakeTracker("user-1")
	d1 := newFakeTracker("user-1:device-a")
	d2 := newFakeTracker("user-1:device-b")
	other := newFakeTracker("user-2:device-a")

	manager.Join(u)
	manager.Join(d1)
	manager.Join(d2)
	manager.Join(other)

	disconnected := manager.DisconnectUser("user-1")
	if disconnected != 3 {
		t.Fatalf("expected 3 disconnected, got %d", disconnected)
	}
	if !u.closed.Load() || !d1.closed.Load() || !d2.closed.Load() {
		t.Fatal("expected user-1 and user-1:* trackers closed")
	}
	if other.closed.Load() {
		t.Fatal("unexpected close for other user")
	}
}

func TestDisconnectSelectorsScansConnectionsOnce(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	var scans atomic.Int64
	manager.disconnectScanHook = func() { scans.Add(1) }

	trackers := []*fakeTracker{
		newFakeTracker("user-1"),
		newFakeTracker("user-1:device-a"),
		newFakeTracker("user-2:device-a"),
		newFakeTracker("user-3:device-a"),
	}
	for _, tracker := range trackers {
		manager.Join(tracker)
	}

	disconnected := manager.DisconnectSelectors(
		[]string{"user-2:device-a", "user-2:device-a"},
		[]string{"user-1", "user-1"},
	)
	if disconnected != 3 {
		t.Fatalf("expected 3 disconnected, got %d", disconnected)
	}
	if scans.Load() != 1 {
		t.Fatalf("expected one connection scan, got %d", scans.Load())
	}
	if !trackers[0].closed.Load() || !trackers[1].closed.Load() || !trackers[2].closed.Load() {
		t.Fatal("expected matching trackers closed")
	}
	if trackers[3].closed.Load() {
		t.Fatal("unexpected close for unmatched tracker")
	}
}

func TestDisconnectSelectorsHundredUsersSingleScan(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	var scans atomic.Int64
	manager.disconnectScanHook = func() { scans.Add(1) }
	userIDs := make([]string, 0, 100)
	trackers := make([]*fakeTracker, 0, 100)
	for index := 0; index < 100; index++ {
		userID := fmt.Sprintf("user-%03d", index)
		userIDs = append(userIDs, userID)
		tracker := newFakeTracker(userID + ":device-1")
		trackers = append(trackers, tracker)
		manager.Join(tracker)
	}

	if disconnected := manager.DisconnectSelectors(nil, userIDs); disconnected != 100 {
		t.Fatalf("expected 100 disconnected, got %d", disconnected)
	}
	if scans.Load() != 1 {
		t.Fatalf("expected one scan for 100 users, got %d", scans.Load())
	}
	for index, tracker := range trackers {
		if !tracker.closed.Load() {
			t.Fatalf("tracker %d was not closed", index)
		}
	}
}

func TestPolicySnapshotIsSortedAndDetached(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.ApplyPolicyRevision(2, true, []PrincipalPolicy{
		{Principal: "u2:*", UpBPS: 200},
		{Principal: "u1:d1", DownBPS: 100},
	})

	snapshot := manager.PolicySnapshot()
	if len(snapshot) != 2 || snapshot[0].Principal != "u1:d1" || snapshot[1].Principal != "u2:*" {
		t.Fatalf("unexpected policy snapshot: %+v", snapshot)
	}
	snapshot[0].DownBPS = 999
	policy, found := manager.PolicyForPrincipal("u1:d1")
	if !found || policy.DownBPS != 100 {
		t.Fatalf("snapshot must not mutate manager state: %+v", policy)
	}
}

func TestPolicyForPrincipalPreferExactOverWildcard(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	ok := manager.ApplyPolicyRevision(1, true, []PrincipalPolicy{
		{
			Principal:      "u1:*",
			MaxConnections: 1,
			UpBPS:          1024,
		},
		{
			Principal:      "u1:d1",
			MaxConnections: 2,
			UpBPS:          2048,
		},
	})
	if !ok {
		t.Fatal("expected policy revision applied")
	}

	exact, found := manager.PolicyForPrincipal("u1:d1")
	if !found {
		t.Fatal("expected exact policy")
	}
	if exact.MaxConnections != 2 || exact.UpBPS != 2048 {
		t.Fatalf("unexpected exact policy: %+v", exact)
	}

	wildcard, found := manager.PolicyForPrincipal("u1:d2")
	if !found {
		t.Fatal("expected wildcard policy")
	}
	if wildcard.MaxConnections != 1 || wildcard.UpBPS != 1024 {
		t.Fatalf("unexpected wildcard policy: %+v", wildcard)
	}
}

func TestSnapshotByPrincipalApplyWildcardPolicy(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	ok := manager.ApplyPolicyRevision(1, true, []PrincipalPolicy{
		{
			Principal:      "u2:*",
			MaxConnections: 3,
			UpBPS:          333,
			DownBPS:        666,
		},
	})
	if !ok {
		t.Fatal("expected policy revision applied")
	}

	tracker := newFakeTracker("u2:d1")
	manager.Join(tracker)

	snapshots := manager.SnapshotByPrincipal()
	if len(snapshots) == 0 {
		t.Fatal("expected snapshots")
	}

	var principalSnapshot *PrincipalSnapshot
	for i := range snapshots {
		if snapshots[i].Principal == "u2:d1" {
			principalSnapshot = &snapshots[i]
			break
		}
	}
	if principalSnapshot == nil {
		t.Fatal("expected u2:d1 snapshot")
	}
	if principalSnapshot.MaxConnections != 3 || principalSnapshot.UpBPS != 333 || principalSnapshot.DownBPS != 666 {
		t.Fatalf("unexpected wildcard-applied snapshot: %+v", *principalSnapshot)
	}
}

func TestPolicyForPrincipalResolvedReturnsWildcardKey(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	ok := manager.ApplyPolicyRevision(1, true, []PrincipalPolicy{
		{
			Principal: "u3:*",
			UpBPS:     1024,
		},
	})
	if !ok {
		t.Fatal("expected policy revision applied")
	}

	policy, key, found := manager.PolicyForPrincipalResolved("u3:d1")
	if !found {
		t.Fatal("expected wildcard policy resolved")
	}
	if key != "u3:*" {
		t.Fatalf("unexpected policy key: %q", key)
	}
	if policy.UpBPS != 1024 {
		t.Fatalf("unexpected policy value: %+v", policy)
	}
}

func TestDynamicRateLimitAggregatesByWildcardPolicyKey(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	ok := manager.ApplyPolicyRevision(1, true, []PrincipalPolicy{
		{
			Principal: "u4:*",
			UpBPS:     1000,
		},
	})
	if !ok {
		t.Fatal("expected policy revision applied")
	}

	count1 := buildDynamicRateLimitCountFunc(manager, "u4:d1", directionUpload)
	count2 := buildDynamicRateLimitCountFunc(manager, "u4:d2", directionUpload)

	var wg sync.WaitGroup
	wg.Add(2)
	start := time.Now()
	go func() {
		defer wg.Done()
		count1(1000)
	}()
	go func() {
		defer wg.Done()
		count2(1000)
	}()
	wg.Wait()
	elapsed := time.Since(start)

	// 1000B/s with 2*1000B shared by same wildcard key should serialize to around 2s.
	if elapsed < 1800*time.Millisecond {
		t.Fatalf("expected aggregated rate limit to serialize shared principal traffic, elapsed=%s", elapsed)
	}
}

func TestSnapshotByPrincipalMonotonicCounters(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.PushPrincipalUploaded("u1:d1", 10)
	manager.PushPrincipalDownloaded("u1:d1", 5)

	first := findPrincipalSnapshot(manager.SnapshotByPrincipal(), "u1:d1")
	if first == nil {
		t.Fatal("expected principal snapshot after first push")
	}
	if first.Upload != 10 || first.Download != 5 {
		t.Fatalf("unexpected first snapshot: %+v", *first)
	}

	manager.PushPrincipalUploaded("u1:d1", 7)
	manager.PushPrincipalDownloaded("u1:d1", 11)
	second := findPrincipalSnapshot(manager.SnapshotByPrincipal(), "u1:d1")
	if second == nil {
		t.Fatal("expected principal snapshot after second push")
	}
	if second.Upload != 17 || second.Download != 16 {
		t.Fatalf("unexpected second snapshot: %+v", *second)
	}
	if second.Upload < first.Upload || second.Download < first.Download {
		t.Fatalf("expected monotonic counters, first=%+v second=%+v", *first, *second)
	}
}

func TestSnapshotByPrincipalReconnectKeepsTotals(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	tracker := newFakeTracker("u1:d1")
	manager.Join(tracker)
	manager.PushPrincipalUploaded("u1:d1", 42)
	manager.PushPrincipalDownloaded("u1:d1", 24)

	connected := findPrincipalSnapshot(manager.SnapshotByPrincipal(), "u1:d1")
	if connected == nil {
		t.Fatal("expected connected principal snapshot")
	}
	if connected.Active != 1 || connected.Upload != 42 || connected.Download != 24 {
		t.Fatalf("unexpected connected snapshot: %+v", *connected)
	}

	manager.Leave(tracker)
	disconnected := findPrincipalSnapshot(manager.SnapshotByPrincipal(), "u1:d1")
	if disconnected == nil {
		t.Fatal("expected disconnected principal snapshot")
	}
	if disconnected.Active != 0 {
		t.Fatalf("expected active=0 after disconnect, got %+v", *disconnected)
	}
	if disconnected.Upload != 42 || disconnected.Download != 24 {
		t.Fatalf("expected totals preserved after disconnect, got %+v", *disconnected)
	}

	reconnected := newFakeTracker("u1:d1")
	manager.Join(reconnected)
	manager.PushPrincipalUploaded("u1:d1", 8)
	manager.PushPrincipalDownloaded("u1:d1", 6)
	afterReconnect := findPrincipalSnapshot(manager.SnapshotByPrincipal(), "u1:d1")
	if afterReconnect == nil {
		t.Fatal("expected principal snapshot after reconnect")
	}
	if afterReconnect.Active != 1 || afterReconnect.Upload != 50 || afterReconnect.Download != 30 {
		t.Fatalf("unexpected snapshot after reconnect: %+v", *afterReconnect)
	}
}

func TestPrincipalCounterConcurrentAccumulation(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	principals := []string{"u1:d1", "u1:d2", "u2:d1"}
	const iterations = 200

	var wg sync.WaitGroup
	for _, principal := range principals {
		principal := principal
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				manager.PushUploaded(3)
				manager.PushDownloaded(5)
				manager.PushPrincipalUploaded(principal, 3)
				manager.PushPrincipalDownloaded(principal, 5)
			}
		}()
	}
	wg.Wait()

	uploadTotal, downloadTotal := manager.Total()
	expectedUpload := int64(len(principals) * iterations * 3)
	expectedDownload := int64(len(principals) * iterations * 5)
	if uploadTotal != expectedUpload || downloadTotal != expectedDownload {
		t.Fatalf("unexpected global totals: upload=%d download=%d", uploadTotal, downloadTotal)
	}

	var principalUpload int64
	var principalDownload int64
	for _, snapshot := range manager.SnapshotByPrincipal() {
		principalUpload += snapshot.Upload
		principalDownload += snapshot.Download
	}
	if principalUpload != uploadTotal || principalDownload != downloadTotal {
		t.Fatalf("principal totals do not match globals: principalUpload=%d globalUpload=%d principalDownload=%d globalDownload=%d", principalUpload, uploadTotal, principalDownload, downloadTotal)
	}
}

func TestPrincipalCounterTTLPrune(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	manager.PushPrincipalUploaded("stale:d1", 10)
	manager.PushPrincipalUploaded("fresh:d1", 20)

	manager.principalCountersAccess.RLock()
	staleCounter := manager.principalCounters["stale:d1"]
	freshCounter := manager.principalCounters["fresh:d1"]
	manager.principalCountersAccess.RUnlock()
	if staleCounter == nil || freshCounter == nil {
		t.Fatal("expected principal counters present")
	}

	now := time.Now()
	staleCounter.lastSeenUnix.Store(now.Add(-principalCounterTTL - time.Hour).Unix())
	freshCounter.lastSeenUnix.Store(now.Unix())
	manager.lastPrincipalPruneUnix.Store(0)
	manager.maybePrunePrincipalCounters(now)

	snapshots := manager.SnapshotByPrincipal()
	if findPrincipalSnapshot(snapshots, "stale:d1") != nil {
		t.Fatal("expected stale principal removed by TTL prune")
	}
	fresh := findPrincipalSnapshot(snapshots, "fresh:d1")
	if fresh == nil || fresh.Upload != 20 {
		t.Fatalf("expected fresh principal preserved, got %+v", fresh)
	}
}
