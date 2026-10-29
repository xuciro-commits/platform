package platformserver

import (
	"errors"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"strings"
	"testing"
	"time"

	"platformserver/studio/build"

	"platformserver/platform"
	"platformserver/platform/authz"
)

type controlledRuleProvider struct {
	platform.NativeRules
	failing bool
	calls   int
}

func (p *controlledRuleProvider) CompileExpr(e platform.Expr, s platform.ExprSchema) (platform.ExprProgram, error) {
	p.calls++
	if p.failing {
		return nil, errors.New("selected rule provider is unavailable")
	}
	return p.NativeRules.CompileExpr(e, s)
}

func (p *controlledRuleProvider) CompileFrozenFormula(e platform.Expr, s platform.ExprSchema) (platform.ExprProgram, error) {
	p.calls++
	if p.failing {
		return nil, errors.New("selected rule provider is unavailable")
	}
	return p.NativeRules.CompileFrozenFormula(e, s)
}

type controlledAuthorization struct {
	authz.Standard
	failing bool
	calls   int
}

type controlledKernel struct {
	platform.NativeKernel
	failing bool
	calls   int
}

func (p *controlledKernel) Receive(log platform.ChangeLog, authorities platform.Authorities, policy kernel.Policy, caller kernel.Caller, s *pb.Submission, at time.Time, domain func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	p.calls++
	if p.failing {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Selected kernel is unavailable")
	}
	return p.NativeKernel.Receive(log, authorities, policy, caller, s, at, domain)
}

func (p *controlledAuthorization) Decide(q authz.Request, policies ...authz.Policy) authz.Verdict {
	p.calls++
	if p.failing {
		return authz.Verdict{Rule: "none", Reason: "selected authorization provider is unavailable", Layers: []authz.Layer{{Name: "provider", Allowed: false, Reason: "unavailable"}}}
	}
	return p.Standard.Decide(q, policies...)
}

func TestSelectedRuntimeProvidersReachDecisionsAndDoNotRunDuringResultRecovery(t *testing.T) {
	rules := &controlledRuleProvider{}
	authorization := &controlledAuthorization{}
	selectedKernel := &controlledKernel{}
	services := platform.DefaultRuntimeCapabilities()
	services.Rules, services.Authorization = rules, authorization
	services.Kernel = selectedKernel
	services.KernelModule.ID = "test.selected-kernel"
	services.RuleModule.ID = "test.selected-rules"
	services.AuthModule.ID = "test.selected-authorization"
	newTenant := func() *Tenant {
		return composeTenantWithCapabilities(t, "selected-runtime", services, []platform.Seat{seatOf("builder", "build:builder"), seatOf("user", "build:user")}, build.New("selected-runtime"))
	}
	tn := newTenant()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	at := time.Now().UTC()
	expression := platform.Expr{Op: "+", Kind: "integer", Args: []platform.Expr{platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 1)}}
	publishObject(t, tn, "builder", "MODEL", map[string]any{
		"name": "calculation", "title": "Calculation",
		"fields":      []build.Field{{Name: "qty", Title: "Quantity", Type: "integer"}, {Name: "total", Title: "Total", Type: "integer", FormulaExpr: &expression}},
		"constraints": []platform.Constraint{{Name: "positive", Kind: "expression", Fields: []string{"qty"}, Message: "Quantity must be positive", Expr: &platform.Expr{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("integer", "qty"), platform.ExprLiteralValue("integer", 0)}}}},
		"access":      []build.Access{{Role: "user", Read: "all", Create: true, Edit: true}},
	}, at)
	decide(t, tn, "builder", "build", "build.table.create", build.TableType, "TABLE", map[string]any{
		"name": "gate", "title": "Gate", "inputs": []build.TableColumn{{Name: "value", Type: "text"}},
		"outputs": []build.TableColumn{{Name: "allowed", Type: "boolean"}}, "rows": []build.TableRow{{When: []string{"ready"}, Then: []string{"true"}}},
	}, at)
	decide(t, tn, "builder", "build", build.SchemaTablePublish, build.TableType, "TABLE", map[string]any{}, at)
	rules.failing = true
	if err := tn.validateAssetSet(rules); err == nil || !strings.Contains(err.Error(), "selected rule provider") {
		t.Fatalf("publication set checks bypassed selected provider: %v", err)
	}
	if refusal := refuse(t, tn, "builder", "build", "build.object.publish", "build.object", "MODEL", map[string]any{}, at); refusal == "ok" || !strings.Contains(refusal, "selected rule provider") {
		t.Fatalf("authoring bypassed selected provider: %s", refusal)
	}
	if refusal := refuse(t, tn, "user", "build", "build.calculation.create", "build.calculation", "R", map[string]any{"qty": 1}, at); refusal == "ok" || !strings.Contains(refusal, "selected rule provider") {
		t.Fatalf("formula bypassed selected provider: %s", refusal)
	}
	rules.failing = false
	selectedKernel.failing = true
	if refusal := refuse(t, tn, "user", "build", "build.calculation.create", "build.calculation", "R", map[string]any{"qty": 1}, at); !strings.Contains(refusal, "Selected kernel") {
		t.Fatalf("kernel bypassed selected implementation: %s", refusal)
	}
	selectedKernel.failing = false
	decide(t, tn, "user", "build", "build.calculation.create", "build.calculation", "R", map[string]any{"qty": 1}, at)
	if rules.calls == 0 || authorization.calls == 0 {
		t.Fatal("selected implementations never received the actual decision")
	}
	authorization.failing = true
	if _, refusal := tn.RecordOf(memberOf(t, tn, "user"), "build.calculation", "R", at); refusal == nil {
		t.Fatal("current record and history read bypassed selected authorization")
	}
	if refusal := refuse(t, tn, "user", "build", "build.calculation.create", "build.calculation", "OTHER", map[string]any{"qty": 1}, at); refusal == "ok" || !strings.Contains(refusal, "selected authorization provider") {
		t.Fatalf("decision fell back to role-only authorization: %s", refusal)
	}
	if memberOf(t, tn, "user").May("build", "build.calculation.create", []string{"user"}).Allow {
		t.Fatal("member permission preview bypassed its bound authorization provider")
	}
	if len(tn.Catalog(memberOf(t, tn, "user"))) != 0 {
		t.Fatal("action catalogue bypassed the selected authorization provider")
	}
	rules.failing = true
	beforeRules, beforeAuthorization, beforeKernel := rules.calls, authorization.calls, selectedKernel.calls
	selectedKernel.failing = true
	recovered := newTenant()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if rules.calls != beforeRules || authorization.calls != beforeAuthorization || selectedKernel.calls != beforeKernel {
		t.Fatal("saved result recovery re-executed rules or authorization")
	}
	authorization.failing, selectedKernel.failing = false, false
	if refusal := refuse(t, recovered, "user", "build", "build.calculation.create", "build.calculation", "AFTER-RECOVERY", map[string]any{"qty": 2}, at); refusal == "ok" || !strings.Contains(refusal, "selected rule provider") {
		t.Fatal("restored runtime reused another generation's compiled program", refusal)
	}
	if recovered.services.RuleModule.ID != "test.selected-rules" || recovered.records.Rules() != rules {
		t.Fatal("recovery silently installed default providers")
	}
}
