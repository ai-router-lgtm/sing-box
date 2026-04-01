package trafficontrol

import (
	"sync/atomic"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/sagernet/sing-box/adapter"
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

func TestApplyPolicyRevisionRejectStale(t *testing.T) {
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

func TestPolicyForPrincipalPreferExactOverWildcard(t *testing.T) {
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
