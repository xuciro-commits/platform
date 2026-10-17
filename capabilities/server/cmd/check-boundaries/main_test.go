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
		{"multiline anonymous runtime", "capabilities/server/platform/caller.go", "package platform\nfunc f(c any) { _, _ = c.(interface {\n Ready() bool\n}) }", true},
		{"concrete identity owner", "capabilities/server/accessor.go", "package runtime\nfunc f(c any) { _ = c.(*Console) }", true},
		{"concrete agent owner", "capabilities/server/accessor.go", "package runtime\nfunc f(c any) { _ = c.(*Agents) }", true},
		{"named runtime", "capabilities/server/platform/caller.go", "package platform\ntype ReadOwner interface { Ready() bool }; func f(c any) { _, _ = c.(ReadOwner) }", false},
		{"aliased wide consumer", "capabilities/server/apps/files/files.go", "package files\nimport h \"platformserver/internal/host\"\ntype Files struct { host h.Host }", true},
		{"sibling app", "capabilities/server/apps/files/files.go", "package files\nimport \"platformserver/apps/enterprise\"", true},
		{"owner's own subpackage", "capabilities/server/apps/enterprise/model.go", "package enterprise\nimport \"platformserver/apps/enterprise/uaf\"", false},
		{"binary composition", "capabilities/server/cmd/demo/main.go", "package main\nimport \"platformserver/apps/build\"", false},
		{"contract reverse import", "capabilities/server/internal/host/buildapi.go", "package host\nimport \"platformserver/apps/build\"", true},
		{"business inactive runtime", "apps/mes/server/bridge_windows.go", "//go:build windows\npackage mes\nimport \"platformserver\"", true},
		{"protocol forbidden concrete owner", "protocols/orders/bridge.go", "package orders\nimport \"platformserver/apps/work\"", true},
		{"separate protocol test harness", "protocols/orders/orderstest/check.go", "package orderstest\nimport \"platformserver\"", false},
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
