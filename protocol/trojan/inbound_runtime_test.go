package trojan

import (
	"reflect"
	"testing"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

func TestPrepareRuntimeUserReplaceRejectsDuplicateCredentialsWithoutMutation(t *testing.T) {
	t.Parallel()

	inbound := &Inbound{users: []option.TrojanUser{{Name: "static", Password: "static-password"}}}
	before := inbound.SnapshotRuntimeUsers()
	transaction, err := inbound.PrepareRuntimeUserReplace([]adapter.RuntimeUser{
		{Principal: "u1:d1", Password: "shared-password"},
		{Principal: "u2:d1", Password: "shared-password"},
	})
	if err == nil {
		transaction.Abort()
		t.Fatal("duplicate credential set was accepted")
	}
	if after := inbound.SnapshotRuntimeUsers(); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed prepare changed users: before=%+v after=%+v", before, after)
	}
}

func TestPreparedRuntimeUserReplaceApplyHasNoFallibleWork(t *testing.T) {
	t.Parallel()

	inbound := &Inbound{}
	transaction, err := inbound.PrepareRuntimeUserReplace([]adapter.RuntimeUser{{
		Principal: "u1:d1", Password: "new-password",
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
