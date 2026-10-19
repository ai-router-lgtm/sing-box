package clashapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/sagernet/sing-box/experimental/clashapi/trafficontrol"
)

// TestRuntimeZeroPolicyDeltaConverges checks HTTP policy writes against both digest endpoints.
func TestRuntimeZeroPolicyDeltaConverges(t *testing.T) {
	t.Parallel()

	_, server := newRuntimeTestServer(newFakeInboundManager())
	defer server.Close()

	apply := func(payload string) {
		t.Helper()
		_ = runtimeJSONRequest(t, http.MethodPut, server.URL+"/policy", payload, http.StatusOK)
	}
	check := func(expected []trafficontrol.PrincipalPolicy) {
		t.Helper()
		var snapshot struct {
			Revision int64                           `json:"revision"`
			Count    int                             `json:"count"`
			Digest   string                          `json:"digest"`
			Policies []trafficontrol.PrincipalPolicy `json:"policies"`
		}
		body := runtimeJSONRequest(t, http.MethodGet, server.URL+"/policy/snapshot", "", http.StatusOK)
		if err := json.Unmarshal(body, &snapshot); err != nil {
			t.Fatal(err)
		}
		var status struct {
			Count    int    `json:"policy_count"`
			Digest   string `json:"policy_digest"`
			Revision int64  `json:"policy_revision"`
		}
		if err := json.Unmarshal(getRuntimeStatusBody(t, server.URL), &status); err != nil {
			t.Fatal(err)
		}
		if snapshot.Count != len(expected) || len(snapshot.Policies) != len(expected) || snapshot.Digest != digestPolicies(expected) {
			t.Fatalf("unexpected policy state: %+v; expected=%+v", snapshot, expected)
		}
		if status.Count != snapshot.Count || status.Digest != snapshot.Digest || status.Revision != snapshot.Revision {
			t.Fatalf("status and snapshot differ: %+v / %+v", status, snapshot)
		}
	}

	apply(`{"revision":1,"request_id":"recovery","replace":true,"policies":[]}`)
	check(nil)
	for revision := 2; revision <= 4; revision++ {
		apply(fmt.Sprintf(`{"revision":%d,"request_id":"refresh-%d","replace":false,"policies":[{"principal":"trial:device","max_connections":0,"up_bps":0,"down_bps":0}]}`, revision, revision))
		check(nil)
	}
	apply(`{"revision":5,"request_id":"limits","replace":false,"policies":[{"principal":"clear:device","max_connections":2},{"principal":"keep:device","down_bps":2048}]}`)
	check([]trafficontrol.PrincipalPolicy{{Principal: "clear:device", MaxConnections: 2}, {Principal: "keep:device", DownBPS: 2048}})
	clear := `{"revision":6,"request_id":"clear","replace":false,"policies":[{"principal":"clear:device","max_connections":0,"up_bps":0,"down_bps":0}]}`
	apply(clear)
	check([]trafficontrol.PrincipalPolicy{{Principal: "keep:device", DownBPS: 2048}})
	apply(clear)
	check([]trafficontrol.PrincipalPolicy{{Principal: "keep:device", DownBPS: 2048}})
	apply(`{"revision":7,"request_id":"new-limit","replace":false,"policies":[{"principal":"clear:device","up_bps":512}]}`)
	apply(clear) // Replaying a cached clear must not erase a newer limit.
	apply(`{"revision":6,"request_id":"stale-clear","replace":false,"policies":[{"principal":"keep:device"}]}`)
	check([]trafficontrol.PrincipalPolicy{{Principal: "clear:device", UpBPS: 512}, {Principal: "keep:device", DownBPS: 2048}})
}
