package lodging

import (
	"testing"

	"platformserver"
)

func TestMemoryConforms(t *testing.T) {
	tn, err := platformserver.NewTenant("t", platformserver.NewDirectory("t"), platformserver.NewRelations("t"), NewMemory("t"))
	if err != nil {
		t.Fatal(err)
	}
	Conformance(t, tn, platformserver.Member{ID: "k", Tenant: "t", Roles: map[string]string{"memstay": Keeper}}, "any-room")
}
