package build

import (
	"testing"

	buildapi "platformserver/internal/host/buildapi"
)

// A related update moves its target only along a transition the target declares
// (ADR-0098 B4): not an approval, not a creation, and only from a state it leaves.
func TestMoveActionOnlyDeclaredTransitions(t *testing.T) {
	o := Object{Name: "order", States: []State{{Name: "released"}, {Name: "completed"}, {Name: "closed"}}, Actions: []Action{
		{Name: "complete", From: []string{"released"}, To: "completed"},
		{Name: "approve", From: []string{"released"}, To: "closed", Approval: &buildapi.ActionApproval{Pending: "released"}},
		{Name: "release", New: true, To: "released"},
	}}
	if a, ok := moveAction(o, "released", "completed"); !ok || a.Name != "complete" {
		t.Fatalf("released -> completed should be the complete action, got %+v %v", a, ok)
	}
	if _, ok := moveAction(o, "completed", "released"); ok {
		t.Fatal("completed -> released has no declared transition")
	}
	if _, ok := moveAction(o, "released", "closed"); ok {
		t.Fatal("an approval-gated transition cannot be taken by a related update")
	}
}

// A pin names an integer field of the same object, and the version field is one too.
func TestVersioningPinsNameIntegerFields(t *testing.T) {
	ok := Object{Name: "sfc", VersionField: "version", Fields: []Field{
		{Name: "version", Type: "integer"},
		{Name: "routingversion", Type: "integer"},
		{Name: "routing", Type: "reference", Ref: "build.routing", Pins: "routingversion"},
	}}
	if err := checkVersioning(ok); err != nil {
		t.Fatalf("valid versioning refused: %v", err)
	}
	badPin := ok
	badPin.Fields = []Field{{Name: "routingversion", Type: "text"}, {Name: "routing", Type: "reference", Ref: "build.routing", Pins: "routingversion"}}
	if err := checkVersioning(badPin); err == nil {
		t.Fatal("a pin onto a text field must be refused")
	}
	notRef := Object{Name: "sfc", Fields: []Field{{Name: "routingversion", Type: "integer"}, {Name: "quantity", Type: "integer", Pins: "routingversion"}}}
	if err := checkVersioning(notRef); err == nil {
		t.Fatal("only a reference may pin a version")
	}
	badVersion := Object{Name: "routing", VersionField: "revisionno", Fields: []Field{{Name: "version", Type: "integer"}}}
	if err := checkVersioning(badVersion); err == nil {
		t.Fatal("a version field that is not declared must be refused")
	}
}

// A single-hop reference reads the version its own pinned field holds, and only that.
func TestPinnedByReadsDeclaredPin(t *testing.T) {
	o := Object{Name: "sfc", Fields: []Field{
		{Name: "routing", Type: "reference", Ref: "build.routing", Pins: "routingversion"},
		{Name: "product", Type: "reference", Ref: "build.product"},
	}}
	own := map[string]any{"routingversion": 2}
	if got := pinnedBy(o, own, []string{"routing"}); got != 2 {
		t.Fatalf("routing pinned at 2, got %d", got)
	}
	if got := pinnedBy(o, own, []string{"product"}); got != 0 {
		t.Fatalf("an unpinned reference reads no version, got %d", got)
	}
	if got := pinnedBy(o, own, []string{"routing", "operations"}); got != 0 {
		t.Fatalf("a multi-hop path takes its version from the final record, got %d", got)
	}
}
