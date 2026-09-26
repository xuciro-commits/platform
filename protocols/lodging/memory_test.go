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
	tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t"), relations.New("t"), lodging.NewMemory("t"))
	if err != nil {
		t.Fatal(err)
	}
	lodgingtest.Conformance(t, tn, platform.Member{ID: "k", Tenant: "t", Roles: map[string]string{"memstay": lodging.Keeper}}, "any-room")
}
