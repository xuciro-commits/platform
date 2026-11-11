package host

import (
	"reflect"
	"testing"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func TestInvariantOwnedRelationsReadWithoutNestedRecordLock(t *testing.T) {
	live := newInvariantTenant(t, "invariant-owned-relations", platform.Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: "user"}}})
	info, err := platform.Describe(build.ID, platform.RelationEntity(build.ID), func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if err := live.Projection.Install(info); err != nil {
		t.Fatal(err)
	}
	caller := platform.NewCaller(runtime{t: live.Tenant}, live.member(t), build.ID, false, false)
	done := make(chan []platform.RelationLink, 1)
	go func() {
		done <- live.ownedRelations(live.Projection, caller, "", invariantClock)
	}()
	select {
	case links := <-done:
		if len(links) != 0 {
			t.Fatalf("owned relationships on an empty tenant: %+v", links)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owned relationship read nested the record store lock")
	}
}
