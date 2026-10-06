package build

import (
	"strings"
	"testing"
)

// An interface no app declares, and an extension without its base field,
// are refused while the object is still a draft (ADR-0058 A2, A3).
func TestCheckShape(t *testing.T) {
	b := &Build{}
	o := Object{Name: "vendor", Title: "Vendor", Fields: []Field{{Name: "code", Type: "text", Search: true}}, Implements: []string{"core.coded"}}
	if err := b.checkShape(o); err == nil || !strings.Contains(err.Error(), "core.coded") {
		t.Fatalf("undeclared interface accepted: %v", err)
	}
	o = Object{Name: "employee", Title: "Employee", Extends: "build.employee"}
	if err := b.checkShape(o); err == nil || !strings.Contains(err.Error(), "cannot extend") {
		t.Fatalf("self extension accepted: %v", err)
	}
}
