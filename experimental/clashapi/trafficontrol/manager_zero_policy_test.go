package trafficontrol

import "testing"

// TestZeroPolicyDeltaDeletesLimits covers clearing each limit without losing other principals.
func TestZeroPolicyDeltaDeletesLimits(t *testing.T) {
	t.Parallel()

	for _, initial := range []PrincipalPolicy{
		{MaxConnections: 2}, {UpBPS: 1024}, {DownBPS: 2048},
	} {
		manager := NewManager()
		initial.Principal = "clear:device"
		retained := PrincipalPolicy{Principal: "keep:device", UpBPS: 4096}
		if !manager.ApplyPolicyRevision(1, true, []PrincipalPolicy{initial, retained}) {
			t.Fatal("initial replacement rejected")
		}
		if !manager.ApplyPolicyRevision(2, false, []PrincipalPolicy{{Principal: initial.Principal}}) {
			t.Fatal("clear delta rejected")
		}
		if _, found := manager.PolicyForPrincipal(initial.Principal); found {
			t.Fatalf("zero delta retained cleared policy: %+v", initial)
		}
		if policies := manager.PolicySnapshot(); len(policies) != 1 || policies[0] != retained {
			t.Fatalf("unrelated limit changed: %+v", policies)
		}
	}
}

// TestZeroPolicyDeltaAfterEmptySnapshot reproduces subscription refreshes following recovery.
func TestZeroPolicyDeltaAfterEmptySnapshot(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	if !manager.ApplyPolicyRevision(10, true, nil) {
		t.Fatal("empty recovery replacement rejected")
	}
	for revision := int64(11); revision <= 15; revision++ {
		if !manager.ApplyPolicyRevision(revision, false, []PrincipalPolicy{{Principal: "trial:device"}}) {
			t.Fatal("zero refresh rejected")
		}
		if policies := manager.PolicySnapshot(); len(policies) != 0 {
			t.Fatalf("zero refresh accumulated policies: %+v", policies)
		}
	}
	if manager.CurrentPolicyRevision() != 15 {
		t.Fatal("zero delta did not advance revision")
	}
}

// TestZeroPolicyDeltaRejectsStaleClear ensures an older clear cannot remove a new limit.
func TestZeroPolicyDeltaRejectsStaleClear(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	limit := PrincipalPolicy{Principal: "user:device", MaxConnections: 3}
	if !manager.ApplyPolicyRevision(20, false, []PrincipalPolicy{limit}) {
		t.Fatal("limit rejected")
	}
	if manager.ApplyPolicyRevision(19, false, []PrincipalPolicy{{Principal: limit.Principal}}) {
		t.Fatal("stale clear accepted")
	}
	if policy, found := manager.PolicyForPrincipal(limit.Principal); !found || policy != limit {
		t.Fatalf("stale clear modified new limit: %+v, found=%v", policy, found)
	}
}

// TestZeroPolicyReplacementCompatibility preserves the existing full replacement contract.
func TestZeroPolicyReplacementCompatibility(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	zero := PrincipalPolicy{Principal: "user:device"}
	if !manager.ApplyPolicyRevision(1, true, []PrincipalPolicy{zero}) {
		t.Fatal("replacement rejected")
	}
	if policies := manager.PolicySnapshot(); len(policies) != 1 || policies[0] != zero {
		t.Fatalf("full replacement behavior changed: %+v", policies)
	}
}
