package platform

import (
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type decisionContractApp struct {
	App
	called bool
	caller Caller
}

func (*decisionContractApp) Manifest() Manifest { return Manifest{ID: "child"} }

func (a *decisionContractApp) Submit(c Caller, _ *pb.Submission, _ time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a.called, a.caller = true, c
	return nil, nil
}

type decisionContractRuntime struct {
	Runtime
	caller    Caller
	decided   bool
	attempted bool
	refusal   *kernel.Error
}

type authoritativeDraftRuntime struct{ Runtime }

func (authoritativeDraftRuntime) Probing() bool { return false }

func (authoritativeDraftRuntime) DraftChanges(l *Ledger, _ func() *kernel.ChangeLog) *kernel.ChangeLog {
	return l.changes
}

func TestStagedRuntimeCannotSupplyAuthoritativeLedger(t *testing.T) {
	ledger := NewLedger("tenant", "child", NewCatalog())
	caller := NewCaller(authoritativeDraftRuntime{}, Member{ID: "member", Tenant: "tenant"}, "child", true, false)
	called := false
	_, err := ledger.Receive(caller, nil, time.Time{}, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		called = true
		return nil, nil
	})
	if err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || called || !caller.Staging() {
		t.Fatal("staged runtime used the authoritative ledger")
	}
}

func (r *decisionContractRuntime) Decide(c Caller, _ App, _ *pb.Submission, _ time.Time) (*pb.ChangeRecord, *kernel.Error) {
	r.decided, r.caller = true, c
	return nil, r.refusal
}

func (r *decisionContractRuntime) Attempt(func() (*pb.ChangeRecord, *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	r.attempted = true
	return nil, r.refusal
}

func TestDecisionOwnerRefusalDoesNotRunApplicationOrAttempt(t *testing.T) {
	runtime := &decisionContractRuntime{refusal: &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}}
	caller := NewCaller(runtime, Member{ID: "member", Tenant: "tenant"}, "parent", false, true)
	app := &decisionContractApp{}
	if _, err := Decide(caller, app, nil, time.Time{}); err != runtime.refusal {
		t.Fatalf("decision owner refusal changed: %v", err)
	}
	if !runtime.decided || app.called || runtime.caller.App != "child" ||
		runtime.caller.rt != runtime || runtime.caller.ID != caller.ID || !runtime.caller.Automation {
		t.Fatal("nested decision lost its owner or bypassed its refusal")
	}
	called := false
	_, err := Attempt(caller, func() (*pb.ChangeRecord, *kernel.Error) {
		called = true
		return nil, nil
	})
	if err != runtime.refusal || !runtime.attempted || called {
		t.Fatal("attempt bypassed its decision owner")
	}
}

func TestDecisionWithoutHostRetainsSingleApplicationPath(t *testing.T) {
	caller := As("parent", Member{ID: "member", Tenant: "tenant"})
	app := &decisionContractApp{}
	if _, err := Decide(caller, app, nil, time.Time{}); err != nil || !app.called ||
		app.caller.App != "child" || app.caller.ID != caller.ID || app.caller.rt != nil || app.caller.Staging() {
		t.Fatal("single application caller acquired a host or lost its identity")
	}
	called := false
	_, err := Attempt(caller, func() (*pb.ChangeRecord, *kernel.Error) {
		called = true
		return nil, nil
	})
	if err != nil || !called {
		t.Fatal("single application attempt did not run")
	}
}
