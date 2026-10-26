package main

import (
	"go/parser"
	"go/token"
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
