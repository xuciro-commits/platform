package platformserver

import (
	"errors"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func TestPrivateControlSavepointKeepsParentSettings(t *testing.T) {
	const id = "control-savepoint"
	at := time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)
	live := composeTenant(t, id, []platform.Seat{seatOf("ana", "platform:admin")}, probe{newNotes(id, "p")})
	d := live.newStagedDecision()
	member := memberOf(t, live, "ana")
	c := platform.NewCaller(d, member, controlowner.PlatformApp, false, false)
	set := func(value string) (*pb.ChangeRecord, *kernel.Error) {
		return platform.Decide(c, live.app(controlowner.PlatformApp), submission(live, member, controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "p/limit", map[string]string{"value": value}), at)
	}
	if _, err := set("1"); err != nil {
		t.Fatal(err)
	}
	reading := platform.RouteCaller(c, "p")
	expect := func(value string) {
		t.Helper()
		if got := reading.Setting("limit"); got != value {
			t.Fatalf("private setting=%q want=%q", got, value)
		}
		if got := live.setting(live.automation("p", false), "limit"); got != "0" {
			t.Fatalf("private setting leaked live %q", got)
		}
	}
	expect("1")
	_, err := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		r, err := set("2")
		if err != nil {
			t.Fatal(err)
		}
		expect("2")
		return r, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "refused")
	})
	if err == nil {
		t.Fatal("child did not refuse")
	}
	expect("1")
	if err := platform.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, err := platform.Decide(probe, live.app(controlowner.PlatformApp), submission(live, member, controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "p/limit", map[string]string{"value": "3"}), at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	expect("1")
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("child panic missing")
			}
		}()
		platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
			if _, err := set("5"); err != nil {
				t.Fatal(err)
			}
			panic("private child")
		})
	}()
	expect("1")
	_, err = platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) { return set("4") })
	if err != nil {
		t.Fatal(err)
	}
	expect("4")
	saved, controlErr := d.savedControl()
	if controlErr != nil || saved == nil {
		t.Fatalf("control image missing: %v", controlErr)
	}
	if err := live.validateAcceptedControl(saved); err != nil {
		t.Fatal(err)
	}
	if err := live.applyAcceptedControl(saved); err != nil {
		t.Fatal(err)
	}
	if got := live.setting(live.automation("p", false), "limit"); got != "4" {
		t.Fatalf("accepted setting=%q", got)
	}
}

func TestControlAppendFailureAndRecoveryKeepOwnerState(t *testing.T) {
	const id = "control-commit"
	at := time.Date(2026, 10, 11, 14, 0, 0, 0, time.UTC)
	build := func() *Tenant {
		return composeTenant(t, id, []platform.Seat{seatOf("ana", "platform:admin")}, probe{newNotes(id, "p")})
	}
	live := build()
	member := memberOf(t, live, "ana")
	request := submission(live, member, controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "p/limit", map[string]string{"value": "3"})
	live.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	if _, err := live.Submit(member, request, at); err == nil || live.setting(live.automation("p", false), "limit") != "0" {
		t.Fatal("control escaped append failure")
	}
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); panic("crash after append") }
	func() { defer func() { _ = recover() }(); live.Submit(member, request, at) }()
	if len(entries) != 1 || live.setting(live.automation("p", false), "limit") != "0" {
		t.Fatal("control exposed before saved-result application")
	}
	recovered := build()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.setting(recovered.automation("p", false), "limit") != "3" {
		t.Fatal("saved control not recovered")
	}
	seq, _, _ := recovered.changeState()
	if _, err := recovered.Submit(member, request, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	after, _, _ := recovered.changeState()
	if after != seq {
		t.Fatal("control retry ran again")
	}
}

func TestControlComputeDiscardCancelsOnlyAfterAcceptance(t *testing.T) {
	const id = "control-cancel"
	at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	build := func() *Tenant {
		tenant := composeTenant(t, id, []platform.Seat{seatOf("ana", "platform:admin")})
		generation, _, refusal := tenant.works.Start(id+":compute", "compute")
		if refusal != nil {
			t.Fatal(refusal)
		}
		tenant.outbound = append(tenant.outbound, &effect{Effect: platform.Effect{ID: id + ":compute", App: controlowner.PlatformApp, Endpoint: acceptedformat.OperationEndpoint, Body: "{}", Generation: generation, State: "pending", At: at, Due: at}})
		return tenant
	}
	live := build()
	cancelled := false
	live.cancels.Add(id+":compute", func() { cancelled = true })
	member := memberOf(t, live, "ana")
	request := submission(live, member, controlowner.PlatformApp, controlowner.SchemaEffectDiscard, controlowner.EffectType, id+":compute", map[string]any{})
	live.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	if _, refusal := live.Submit(member, request, at); refusal == nil || cancelled || live.outbound[0].State != "pending" {
		t.Fatal("operation discard escaped failed acceptance")
	}
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if cancelled || live.outbound[0].State != "pending" {
			t.Fatal("operation cancelled before append")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	if _, refusal := live.Submit(member, request, at); refusal != nil {
		t.Fatal(refusal)
	}
	if !cancelled || live.outbound[0].State != "discarded" {
		t.Fatal("accepted discard did not cancel operation")
	}
	recovered := build()
	if err := recovered.Replay(entries); err != nil || recovered.outbound[0].State != "discarded" {
		t.Fatalf("operation cancellation recovery: %v", err)
	}
}

func TestPrivateControlConnectorSharesDecisionAndIsolatesSavepoint(t *testing.T) {
	const id = "control-connector"
	at := time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)
	live := composeTenant(t, id, []platform.Seat{seatOf("ana", "platform:admin"), seatOf("gateway")}, probe{newNotes(id, "p")})
	if err := live.Connect(&pb.ConnectorDescriptor{ConnectorId: "gateway", Direction: pb.ConnectorDirection_CONNECTOR_DIRECTION_PUSH, DataClasses: []string{"p.topic"}}); err != nil {
		t.Fatal(err)
	}
	d := live.newStagedDecision()
	admin := memberOf(t, live, "ana")
	c := platform.NewCaller(d, admin, controlowner.PlatformApp, false, false)
	toggle := func(schema string) (*pb.ChangeRecord, *kernel.Error) {
		return platform.Decide(c, live.app(controlowner.PlatformApp), submission(live, admin, controlowner.PlatformApp, schema, controlowner.ConnectorType, "gateway", map[string]any{}), at)
	}
	if _, err := toggle(controlowner.SchemaConnectorOff); err != nil {
		t.Fatal(err)
	}
	if live.connectors.byID["gateway"].Disabled || !d.control.host.connectors.byID["gateway"].Disabled {
		t.Fatal("connector descriptor leaked or did not stage")
	}
	gateway := platform.NewCaller(d, memberOf(t, live, "gateway"), "p", false, false)
	_, refusal := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		r, err := toggle(controlowner.SchemaConnectorOn)
		if err != nil {
			t.Fatal(err)
		}
		if err := gateway.Deliver("p.topic", "", "", at); err != nil {
			t.Fatalf("private enabled connector refused delivery: %v", err)
		}
		return r, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "child refuses")
	})
	if refusal == nil || !d.control.host.connectors.byID["gateway"].Disabled || live.connectors.byID["gateway"].Disabled || len(d.deliveries) != 0 {
		t.Fatal("child connector control escaped savepoint")
	}
	saved, err := d.savedControl()
	if err != nil {
		t.Fatal(err)
	}
	if err := live.validateAcceptedControl(saved); err != nil {
		t.Fatal(err)
	}
	if err := live.applyAcceptedControl(saved); err != nil {
		t.Fatal(err)
	}
	if !live.connectors.byID["gateway"].Disabled {
		t.Fatal("accepted control did not disable connector")
	}
}

func TestControlDecisionRejectsWrongOwnerBeforeFork(t *testing.T) {
	const id = "control-owner"
	at := time.Date(2026, 10, 11, 20, 0, 0, 0, time.UTC)
	live := composeTenant(t, id, []platform.Seat{seatOf("ana", "platform:admin")}, probe{newNotes(id, "p")})
	member := memberOf(t, live, "ana")
	for _, invalid := range []string{"app", "authority", "principal", "caller tenant", "request tenant"} {
		t.Run(invalid, func(t *testing.T) {
			draft := live.newStagedDecision()
			caller := platform.NewCaller(draft, member, controlowner.PlatformApp, false, false)
			request := submission(live, member, controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "p/limit", map[string]string{"value": "3"})
			switch invalid {
			case "app":
				caller = platform.RouteCaller(caller, "p")
			case "authority":
				request.Authority = "p"
			case "principal":
				request.PrincipalId = "other"
			case "caller tenant":
				caller.Tenant = "other"
			case "request tenant":
				request.TenantId = "other"
			}
			callback, refusal := platform.ControlDecision(caller, request, at)
			if callback != nil || refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED || draft.control != nil || live.setting(live.automation("p", false), "limit") != "0" {
				t.Fatalf("control owner check ran a callback or fork: %v", refusal)
			}
		})
	}
}
