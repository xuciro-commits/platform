package systemscope

import "testing"

func TestFrozenKernelOwnersAndTheirParents(t *testing.T) {
	for _, name := range []string{
		"contract/go/kernel/receive.go", "CONTRACT/spec/K4-change.md",
		"capabilities/server/host.go", "capabilities/server/submission_schema.go",
		"capabilities/server/staged_decision.go", "capabilities/server/runtime.go",
		"capabilities/server/routes_core.go", "capabilities/server/server.go",
		"capabilities/server/accepted_requests.go", "capabilities/server/committed.go",
		"capabilities/server/journal_bridge.go", "capabilities/server/runtime_snapshot.go",
		"capabilities/server/records_write.go", "capabilities/server/record_read_scope.go",
		"capabilities/server/authorization_decision.go", "capabilities/server/identity_roles.go",
		"capabilities/server/console_identity.go", "capabilities/server/platform/ledger.go",
		"capabilities/server/platform/record_image.go", "capabilities/server/platform/authz/authz.go",
		"capabilities/server/idp/local.go", "go.work", "go.work.sum",
		".", "contract", "capabilities", "capabilities/server",
		"capabilities/server/internal", "capabilities/server/platform", "capabilities/server/idp",
	} {
		t.Run(name, func(t *testing.T) {
			if Verdict(name) == "" {
				t.Fatalf("kernel owner or parent %q remained editable", name)
			}
		})
	}
}

func TestApplicationAndPresentationPathsRemainEditable(t *testing.T) {
	for _, name := range []string{
		"capabilities/server/apps/system/system.go", "capabilities/server/apps/build/objects.go",
		"capabilities/server/platform/definition.go", "capabilities/server/platform/revision.go",
		"capabilities/server/platform/page_model.go", "capabilities/server/cmd/platform-server/main.go",
		"capabilities/server/internal/systembuild/systembuild.go", "capabilities/server/page_navigation.go",
		"web/packages/platform/src/views.tsx", "web/apps/workspace/vite.config.ts",
		"scripts/build.mjs", "Makefile", "docs/Architecture.md",
	} {
		t.Run(name, func(t *testing.T) {
			if reason := Verdict(name); reason != "" {
				t.Fatalf("application path %q was frozen: %s", name, reason)
			}
		})
	}
}

func TestSensitivePathsShareTheSourceCaptureRule(t *testing.T) {
	for _, name := range []string{
		"deploy/local/.env", "web/.env.local", "web/.env.production", "local.env",
		"deploy/identity.KEY", "tls/server.pem", "tls/server.p12", "tls/server.pfx",
	} {
		t.Run(name, func(t *testing.T) {
			if !Sensitive(name) || Verdict(name) == "" {
				t.Fatalf("credential path %q was not excluded from capture and edits", name)
			}
		})
	}
	for _, name := range []string{"contract/go/kernel/receive.go", "web/src/App.tsx", "docs/environment.md"} {
		if Sensitive(name) {
			t.Fatalf("ordinary source %q was classified as a credential", name)
		}
	}
}

func TestSourcePathsHaveOneRecordIdentity(t *testing.T) {
	for _, name := range []string{"./web/x.ts", "web/a/../x.ts", "web//x.ts", "web/x.ts/"} {
		if Verdict(name) == "" {
			t.Fatalf("alias %q can bypass file revisions", name)
		}
	}
	if Verdict("web/x.ts") != "" {
		t.Fatal("canonical source path refused")
	}
}
