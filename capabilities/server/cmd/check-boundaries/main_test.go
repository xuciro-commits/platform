package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestSourceBoundaryRules(t *testing.T) {
	for _, tc := range []struct {
		name, path, source string
		blocked            bool
	}{
		{"reverse import with inactive build tag", "capabilities/server/read_windows.go", "//go:build windows\npackage runtime\nimport app \"platformserver/apps/build\"\nvar _ = app.ID", true},
		{"multiline anonymous runtime", "capabilities/server/platform/caller.go", "package platform\nfunc f(c any) { _, _ = c.(interface {\n Ready() bool\n}) }", true},
		{"concrete identity owner", "capabilities/server/accessor.go", "package runtime\nfunc f(c any) { _ = c.(*Console) }", true},
		{"concrete agent owner", "capabilities/server/accessor.go", "package runtime\nfunc f(c any) { _ = c.(*Agents) }", true},
		{"named runtime", "capabilities/server/platform/caller.go", "package platform\ntype ReadOwner interface { Ready() bool }; func f(c any) { _, _ = c.(ReadOwner) }", false},
		{"aliased wide consumer", "capabilities/server/apps/files/files.go", "package files\nimport h \"platformserver/internal/host\"\ntype Files struct { host h.Host }", true},
		{"sibling app", "capabilities/server/apps/build/pipeline.go", "package build\nimport \"platformserver/apps/enterprise\"", true},
		{"owner's own subpackage", "capabilities/server/apps/enterprise/model.go", "package enterprise\nimport \"platformserver/apps/enterprise/uaf\"", false},
		{"binary composition", "capabilities/server/cmd/demo/main.go", "package main\nimport \"platformserver/apps/build\"", false},
		{"construction reverse import", "capabilities/server/internal/construction/process.go", "package construction\nimport \"platformserver/apps/build\"", true},
		{"construction host dependency", "capabilities/server/internal/construction/process.go", "package construction\nimport \"platformserver/internal/host\"", true},
		{"enterprise host dependency", "capabilities/server/internal/enterprise/model.go", "package enterprise\nimport \"platformserver/internal/host\"", true},
		{"construction host child dependency", "capabilities/server/internal/construction/process.go", "package construction\nimport \"platformserver/internal/host/consumer\"", true},
		{"records host dependency", "capabilities/server/internal/records/query.go", "package records\nimport \"platformserver/internal/host\"", true},
		{"records host child dependency", "capabilities/server/internal/records/query.go", "package records\nimport \"platformserver/internal/host/legacy\"", true},
		{"acceptance host dependency", "capabilities/server/internal/acceptance/result.go", "package acceptance\nimport \"platformserver/internal/host\"", true},
		{"agent prompt host dependency", "capabilities/server/internal/agents/prompt.go", "package agents\nimport \"platformserver/internal/host\"", true},
		{"agent prompt host child dependency", "capabilities/server/internal/agents/prompt.go", "package agents\nimport \"platformserver/internal/host/runtime\"", true},
		{"agent prompt public model", "capabilities/server/internal/agents/prompt.go", "package agents\nimport \"platformserver/platform\"", false},
		{"records public model", "capabilities/server/internal/records/query.go", "package records\nimport \"platformserver/platform\"", false},
		{"enterprise host dependency", "capabilities/server/internal/enterprise/model.go", "package enterprise\nimport \"platformserver/internal/host\"", true},
		{"enterprise public model", "capabilities/server/internal/enterprise/model.go", "package enterprise\nimport \"platformserver/platform\"", false},
		{"construction public model", "capabilities/server/internal/construction/process.go", "package construction\nimport \"platformserver/platform\"", false},
		{"UAF runtime XML dependency", "capabilities/server/internal/enterprise/uaf/read.go", "package uaf\nimport \"encoding/xml\"", true},
		{"UAF runtime host dependency", "capabilities/server/internal/enterprise/uaf/read.go", "package uaf\nimport \"platformserver/internal/host/legacy\"", true},
		{"UAF pure model dependency", "capabilities/server/apps/enterprise/model.go", "package enterprise\nimport \"platformserver/internal/enterprise/uaf\"", false},
		{"contract reverse import", "capabilities/server/internal/host/construction.go", "package host\nimport \"platformserver/apps/build\"", true},
		{"dot host avoids narrow role", "capabilities/server/apps/files/files.go", "package files\nimport . \"platformserver/internal/host\"\ntype Files struct{host Host}", true},
		{"direct member record projection", "capabilities/server/expose.go", "package runtime\nfunc (t *Tenant) exposed(m platform.Member) any { return t.records.types[\"x\"].rows[\"id\"].value.Interface() }", true},
		{"checked member projection", "capabilities/server/expose.go", "package runtime\nfunc (t *Tenant) exposed(m platform.Member) any { v:=t.records.types[\"x\"].rows[\"id\"].value; return t.narrowed(m, v.Interface(), now, false) }", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := token.NewFileSet()
			file, err := parser.ParseFile(set, tc.path, tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			failures := checkFile(tc.path, file, set)
			if (len(failures) != 0) != tc.blocked {
				t.Fatalf("blocked=%t findings=%v", tc.blocked, failures)
			}
		})
	}
}

func TestVisualWidgetDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{"widget type and its field", "package platform\ntype PageSpacer struct{ Spacer *float64 }", 2},
		{"check method", "package platform\nfunc (d *PageDocument) checkTreemap(s Section) error { return nil }", 1},
		{"compound name names the widget", "package platform\ntype PageButtonGroup struct{} ", 1},
		{"reading an existing widget field is not a declaration", "package platform\nfunc f(s Section) any { return s.Spacer }", 0},
		{"unrelated name containing no widget", "package platform\ntype PageVariables struct{ Gaugeless int }", 1},
		{"no widget", "package platform\ntype PageModel struct{ Fields []FieldRef }", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "x.go", tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got := visualWidgetDeclarations(file); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestRatchetOnlyDecreases(t *testing.T) {
	// A new scope, a grown scope and an unrecorded step down all fail;
	// only the recorded count passes.
	ceilings := map[string]int{
		kindFiles + "\tcapabilities/server/platform":             2,
		kindFiles + "\tcapabilities/server/journal":              3,
		kindDup + "\tformula.go":                                 2,
		kindUI + "\tcapabilities/server/platform/page_spacer.go": 2,
	}
	measures := &measures{
		files: []growth{{kindFiles, "capabilities/server/platform", 2}, {kindFiles, "capabilities/server/apps/build", 1}, {kindFiles, "capabilities/server/journal", 9}},
		dup:   []growth{{kindDup, "formula.go", 3}},
		ui:    []growth{{kindUI, "capabilities/server/platform/page_spacer.go", 1}},
	}
	failures, counted := ratchet(measures, ceilings)
	if counted != 5 {
		t.Fatalf("counted %d want 5", counted)
	}
	wanted := []string{
		"capabilities/server/apps/build is new", // a package that appeared
		"capabilities/server/journal grew to 9", // a package that gained files
		"formula.go grew to 3",                  // a name that spread to another package
		"is down to 1; record the new ceiling",  // a fix that forgot to lower its ceiling
	}
	for _, want := range wanted {
		found := false
		for _, failure := range failures {
			if strings.Contains(failure, want) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %q in %v", want, failures)
		}
	}
	if len(failures) != len(wanted) {
		t.Fatalf("unexpected findings: %v", failures)
	}
}
