package vmess

import (
	"context"
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

func TestPrepareRuntimeUserReplaceRejectsInvalidCredentialsWithoutMutation(t *testing.T) {
	t.Parallel()

	initial := []option.VMessUser{{
		Name: "static", UUID: "11111111-1111-1111-1111-111111111111",
	}}
	testCases := []struct {
		name  string
		users []adapter.RuntimeUser
	}{
		{
			name: "invalid uuid",
			users: []adapter.RuntimeUser{
				{Principal: "u1:d1", UUID: "not-a-uuid"},
			},
		},
		{
			name: "duplicate uuid",
			users: []adapter.RuntimeUser{
				{Principal: "u1:d1", UUID: "22222222-2222-2222-2222-222222222222"},
				{Principal: "u2:d1", UUID: "22222222-2222-2222-2222-222222222222"},
			},
		},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			inbound := &Inbound{ctx: context.Background(), users: append([]option.VMessUser(nil), initial...)}
			before := inbound.SnapshotRuntimeUsers()
			transaction, err := inbound.PrepareRuntimeUserReplace(testCase.users)
			if err == nil {
				transaction.Abort()
				t.Fatal("invalid credential set was accepted")
			}
			if after := inbound.SnapshotRuntimeUsers(); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed prepare changed users: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestPreparedRuntimeUserReplaceApplyHasNoFallibleWork(t *testing.T) {
	t.Parallel()

	inbound := &Inbound{ctx: context.Background()}
	inbound.service = inbound.newService()
	transaction, err := inbound.PrepareRuntimeUserReplace([]adapter.RuntimeUser{{
		Principal: "u1:d1", UUID: "33333333-3333-3333-3333-333333333333",
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := transaction.Apply()
	transaction.Release()
	if result.Upserted != 1 || result.Deleted != 0 {
		t.Fatalf("unexpected replace result: %+v", result)
	}
	users := inbound.SnapshotRuntimeUsers()
	if len(users) != 1 || users[0].Principal != "u1:d1" {
		t.Fatalf("prepared snapshot was not installed: %+v", users)
	}
}
