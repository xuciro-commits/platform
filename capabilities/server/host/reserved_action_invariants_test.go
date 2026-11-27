package host

import (
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"strings"
	"testing"
)

func TestInvariantK49ReservedObjectActions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k49-reserved")
	for _, name := range []string{"create", "edit", "archive", "restore", "reverse", "publish"} {
		t.Run(name, func(t *testing.T) {
			draft := invariantObject("entry"+name, "Entry")
			id := "DRAFT-" + name
			live.accept(t, "build.object.create", build.ObjectType, id, draft)
			typ := "invariantproject.entry" + name
			action := ontology.Action{Name: name, Title: "Reserved", Transaction: &platform.BusinessTxn{}}
			live.accept(t, "build.actiontype.create", build.ActionTypeType, "ACTION-"+name, map[string]any{"name": typ + "." + name, "object": typ, "definition": action})
			_, refusal := live.Submit(live.member(t), live.submission("build.actiontype.publish", build.ActionTypeType, "ACTION-"+name, map[string]any{}), invariantClock)
			if refusal == nil || !strings.Contains(refusal.Message, typ) || !strings.Contains(refusal.Message, name) || !strings.Contains(refusal.Message, "rename") {
				t.Fatalf("reserved action diagnostic: %+v", refusal)
			}
		})
	}
}
