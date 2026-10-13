package lodging_test

import (
	"testing"

	"lodging"
	"lodging/lodgingtest"
	"platformserver"
	"platformserver/apps/relations"
	"platformserver/platform"
)

func TestMemoryConforms(t *testing.T) {
	member := platform.Member{ID: "k", Tenant: "t", Roles: map[string]string{"memstay": lodging.Keeper}}
	tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t", platformserver.Seat{Subjects: []string{"k"}, Member: member}), relations.New("t"), lodging.NewMemory("t"))
	if err != nil {
		t.Fatal(err)
	}
	lodgingtest.Conformance(t, tn, member, "any-room")
}
