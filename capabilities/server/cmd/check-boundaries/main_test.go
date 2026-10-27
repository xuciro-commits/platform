package main

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
		{"runtime UAF generator dependency", "capabilities/server/apps/enterprise/model.go", "package enterprise\nimport \"platformserver/internal/enterprise/uafgen\"", true},
		{"UAF runtime XML dependency", "capabilities/server/internal/enterprise/uaf/read.go", "package uaf\nimport \"encoding/xml\"", true},
		{"UAF runtime host dependency", "capabilities/server/internal/enterprise/uaf/read.go", "package uaf\nimport \"platformserver/internal/host/legacy\"", true},
		{"UAF pure model dependency", "capabilities/server/apps/enterprise/model.go", "package enterprise\nimport \"platformserver/internal/enterprise/uaf\"", false},
		{"UAF command generation dependency", "capabilities/server/cmd/uaf-gen/main.go", "package main\nimport \"platformserver/internal/enterprise/uafgen\"", false},
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

func TestAntiBloatRules(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < defaultFileCap+1; i++ {
		write(filepath.Join("capabilities/server", "pkg", "f"+strconv.Itoa(i)+".go"))
	}
	write(filepath.Join("capabilities/server", "other", "f0.go"))
	write(filepath.Join("capabilities/server", "ui", "page_kanban.go"))

	empty := newBudget()
	findings := append(append(append([]string{}, checkFileCaps(root, empty)...), checkDuplicateBasenames(root, empty)...), checkUIComponents(root, empty)...)
	if len(findings) != 3 {
		t.Fatalf("expected 3 debt findings, got %d: %v", len(findings), findings)
	}

	frozen := collectBudget(root)
	cleared := append(append(append([]string{}, checkFileCaps(root, frozen)...), checkDuplicateBasenames(root, frozen)...), checkUIComponents(root, frozen)...)
	if len(cleared) != 0 {
		t.Fatalf("frozen budget should clear recorded debt, got %v", cleared)
	}

	// A new file beyond the frozen count must fail the ratchet.
	write(filepath.Join("capabilities/server", "pkg", "extra.go"))
	if got := checkFileCaps(root, frozen); len(got) != 1 {
		t.Fatalf("expected the extra file to exceed the frozen budget, got %v", got)
	}
}

func TestBudgetDrift(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init", "-q")
	rel := "capabilities/server/cmd/check-boundaries/boundaries.budget"
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, rel)
	if err := os.WriteFile(path, []byte("cap capabilities/server 10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", rel)
	run("commit", "-q", "-m", "base")

	if err := os.WriteFile(path, []byte("cap capabilities/server 11\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkBudgetDrift(root, rel); len(got) != 1 {
		t.Fatalf("expected the grown budget to be reported, got %v", got)
	}

	if err := os.WriteFile(path, []byte("cap capabilities/server 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := checkBudgetDrift(root, rel); len(got) != 0 {
		t.Fatalf("expected the shrunk budget to pass, got %v", got)
	}
}

func TestUIToken(t *testing.T) {
	for _, name := range []string{"page_spacer.go", "PageKanban", "page_record_gantt.go", "HeatmapCard"} {
		if !uiToken(name) {
			t.Errorf("expected %q to be flagged as a front-end visual component", name)
		}
	}
	for _, name := range []string{"page_model.go", "page_queries.go", "page_rules.go", "accepted_result.go"} {
		if uiToken(name) {
			t.Errorf("expected %q to be a backend contract name", name)
		}
	}
}
