package main

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
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
		{"runtime UAF generator dependency", "capabilities/server/apps/enterprise/model.go", "package enterprise\nimport \"platformserver/internal/enterprise/uafgen\"", true},
		{"UAF runtime XML dependency", "capabilities/server/internal/enterprise/uaf/read.go", "package uaf\nimport \"encoding/xml\"", true},
		{"UAF runtime host dependency", "capabilities/server/internal/enterprise/uaf/read.go", "package uaf\nimport \"platformserver/internal/host/legacy\"", true},
		{"UAF pure model dependency", "capabilities/server/apps/enterprise/model.go", "package enterprise\nimport \"platformserver/internal/enterprise/uaf\"", false},
		{"UAF command generation dependency", "capabilities/server/cmd/uaf-gen/main.go", "package main\nimport \"platformserver/internal/enterprise/uafgen\"", false},
		{"contract reverse import", "capabilities/server/internal/host/construction.go", "package host\nimport \"platformserver/apps/build\"", true},
		{"dot host avoids narrow role", "capabilities/server/apps/files/files.go", "package files\nimport . \"platformserver/internal/host\"\ntype Files struct{host Host}", true},
		{"direct member record projection", "capabilities/server/expose.go", "package runtime\nfunc (t *Tenant) exposed(m platform.Member) any { return t.records.types[\"x\"].rows[\"id\"].value.Interface() }", true},
		{"checked member projection", "capabilities/server/expose.go", "package runtime\nfunc (t *Tenant) exposed(m platform.Member) any { v:=t.records.types[\"x\"].rows[\"id\"].value; return t.narrowed(m, v.Interface(), now, false) }", false},
		{"SDK outside provider", "capabilities/server/apps/build/provider.go", "package build\nimport ai \"github.com/anthropics/anthropic-sdk-go\"\nvar _ = ai.NewClient", true},
		{"retired SDK path stays closed", "capabilities/server/anthropic.go", "package platformserver\nimport ai \"github.com/anthropics/anthropic-sdk-go\"\nvar _ = ai.NewClient", true},
		{"old transport path stays closed", "capabilities/server/aicall.go", "package platformserver\nconst address = \"https://model.test/v1/chat/completions\"", true},
		{"SDK inside provider", "capabilities/server/aigateway/providers/anthropic.go", "package providers\nimport ai \"github.com/anthropics/anthropic-sdk-go\"\nvar _ = ai.NewClient", false},
		{"SDK cannot hide in command", "capabilities/server/cmd/rogue/main.go", "package main\nimport ai \"github.com/openai/openai-go/v4\"\nvar _ = ai.NewClient", true},
		{"raw model endpoint", "capabilities/server/apps/build/client.go", "package build\nconst address = \"https://model.test/v1/chat/completions\"", true},
		{"raw model method", "capabilities/server/apps/build/client.go", "package build\nfunc call(t any) { t.aiRequest() }", true},
		{"inbound model fixture", "capabilities/server/internal/sink/sink.go", "package sink\nconst route = \"POST /v1/chat/completions\"", false},
		{"rule network dependency", "capabilities/server/internal/rules/eval.go", "package rules\nimport _ \"net/http\"", true},
		{"rule storage dependency", "capabilities/server/internal/rules/eval.go", "package rules\nimport _ \"database/sql\"", true},
		{"rule process dependency", "capabilities/server/internal/rules/eval.go", "package rules\nimport _ \"os/exec\"", true},
		{"rule hidden host dependency", "capabilities/server/internal/rules/eval.go", "package rules\nimport _ \"platformserver/platform\"", true},
		{"rule random dependency", "capabilities/server/internal/rules/eval.go", "package rules\nimport _ \"math/rand/v2\"", true},
		{"rule aliased live clock", "capabilities/server/internal/rules/eval.go", "package rules\nimport clock \"time\"\nvar current = clock.Now", true},
		{"rule explicit timestamp", "capabilities/server/internal/rules/eval.go", "package rules\nimport \"time\"\nvar epoch = time.Unix(0, 0)", false},
		{"kernel cannot own studio", "capabilities/server/kernel/acceptance_pipeline.go", "package kernel\nimport _ \"platformserver/studio\"", true},
		{"engine cannot own gateway", "capabilities/server/engine/agents.go", "package engine\nimport _ \"platformserver/aigateway/providers\"", true},
		{"domain cannot revive host", "capabilities/server/studio/authoring.go", "package studio\nimport _ \"platformserver/internal/host\"", true},
		{"public API cannot own engine", "capabilities/server/platform/roles.go", "package platform\nimport _ \"platformserver/engine\"", true},
		{"old app cannot import concrete studio", "capabilities/server/apps/ai/ai.go", "package ai\nimport _ \"platformserver/studio\"", true},
		{"domain uses public role", "capabilities/server/aigateway/dispatch.go", "package aigateway\nimport _ \"platformserver/platform\"", false},
		{"provider within its domain", "capabilities/server/aigateway/dispatch.go", "package aigateway\nimport _ \"platformserver/aigateway/providers\"", false},
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

func TestStructuralBudgets(t *testing.T) {
	root := t.TempDir()
	put := func(path, source string) {
		t.Helper()
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 25 {
		put("capabilities/server/small/part"+string(rune('a'+i))+".go", "package small")
	}
	inspect := func() sourceInventory {
		t.Helper()
		current, findings, err := inspectSources(root)
		if err != nil || len(findings) != 0 {
			t.Fatalf("inventory: %v %v", findings, err)
		}
		return current
	}
	baseline := inspect()
	if got := checkInventory(baseline, baseline); len(got) != 0 {
		t.Fatal(got)
	}
	put("capabilities/server/small/partz.go", "//go:build windows\npackage small")
	if got := strings.Join(checkInventory(inspect(), baseline), "\n"); !strings.Contains(got, "exceed package budget 25") {
		t.Fatal(got)
	}
	put("capabilities/server/one/formula.go", "package one")
	put("capabilities/server/two/formula.go", "package two")
	if got := strings.Join(checkInventory(inspect(), baseline), "\n"); !strings.Contains(got, "new cross-package filename") {
		t.Fatal(got)
	}
	put("capabilities/server/internal/host/extra.go", "package host")
	if got := strings.Join(checkInventory(inspect(), baseline), "\n"); !strings.Contains(got, "new file in retiring directory") {
		t.Fatal(got)
	}
}

func TestVisualDebtIsFrozen(t *testing.T) {
	read := func(source string) map[string]string {
		t.Helper()
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, "widget.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		return visualDeclarations("capabilities/server/platform/widget.go", file, set)
	}
	old := sourceInventory{Visual: read("package platform\ntype PageSpacer struct { Size int }")}
	if len(old.Visual) != 1 {
		t.Fatal(old.Visual)
	}
	if got := checkInventory(old, old); len(got) != 0 {
		t.Fatal(got)
	}
	changed := sourceInventory{Visual: read("package platform\ntype PageSpacer struct { Size, Extra int }")}
	if got := strings.Join(checkInventory(changed, old), "\n"); !strings.Contains(got, "changed backend visual declaration") {
		t.Fatal(got)
	}
	if got := checkRatchet(old, changed); len(got) == 0 {
		t.Fatal("baseline refresh hid a changed visual definition")
	}
	if got := checkRatchet(old, sourceInventory{}); len(got) != 0 {
		t.Fatal(got)
	}
	if got := checkInventory(sourceInventory{}, old); len(got) == 0 {
		t.Fatal("stale debt must be removed so it cannot return later")
	}
	if got := read("package platform\nfunc capture() { _ = filepath.Separator; _ = m.Int64ObservableGauge(\"depth\") }"); len(got) != 0 {
		t.Fatal(got)
	}
	if got := read("package platform\nfunc render() string { return \"record-gantt\" }"); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestStructuralBaselineOnlyShrinks(t *testing.T) {
	old := sourceInventory{Packages: map[string]int{"capabilities/server": 263}, Duplicates: map[string][]string{"formula.go": {"a/formula.go", "b/formula.go"}}, Retiring: map[string][]string{"legacy": {"legacy/a.go"}}}
	lower := sourceInventory{Packages: map[string]int{"capabilities/server": 200}}
	if got := checkRatchet(old, lower); len(got) != 0 {
		t.Fatal(got)
	}
	for _, next := range []sourceInventory{
		{Packages: map[string]int{"capabilities/server": 264}},
		{Packages: map[string]int{"new/package": 26}},
		{Duplicates: map[string][]string{"formula.go": {"a/formula.go", "c/formula.go"}}},
		{Retiring: map[string][]string{"legacy": {"legacy/b.go"}}},
	} {
		if got := checkRatchet(old, next); len(got) == 0 {
			t.Fatalf("baseline growth was accepted: %+v", next)
		}
	}
}

func TestCommittingDebtCannotApproveItself(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root, "-c", "core.hooksPath=" + root, "-c", "user.name=Boundary test", "-c", "user.email=boundary@example.invalid"}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, output)
		}
	}
	git("init", "-q")
	path := filepath.Join(root, "capabilities/server/cmd/check-boundaries/baseline.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	commit := func(data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "--quiet", "--no-gpg-sign", "-m", "Boundary fixture")
	}
	commit(`{"packages":{"capabilities/server":263}}`)
	commit(`{"packages":{"capabilities/server":264}}`)
	previous, err := committedBaselines(root)
	if err != nil || len(previous) != 2 {
		t.Fatalf("snapshots: %+v %v", previous, err)
	}
	next := sourceInventory{Packages: map[string]int{"capabilities/server": 264}}
	if len(checkRatchet(previous[0], next)) != 0 || len(checkRatchet(previous[1], next)) == 0 {
		t.Fatal("a raised committed baseline approved itself")
	}
}
