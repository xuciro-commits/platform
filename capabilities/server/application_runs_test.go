package platformserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
	"platformserver/platform/authz"
)

func TestApplicationRunsCurrentFlowFailures(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		start := func(prefix string) platform.Start {
			return platform.Start{On: []string{"shop.order.place"}, Begin: func(_ platform.Caller, event platform.Event) (string, any, bool) {
				id := event.Record.GetSubmission().GetTarget().GetId()
				return id, nil, strings.HasPrefix(id, prefix)
			}}
		}
		failure := &platform.Evaluate{Run: func(platform.Caller, *platform.Run) (json.RawMessage, *kernel.Error) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Current failure")
		}}
		park := platform.Step{Name: "park", Wait: &platform.Wait{Until: func(platform.Caller, *platform.Run) bool { return false }}, Next: "end"}
		retry := platform.Flow{Name: "recover", Title: "Recover", Version: 1, Start: start("retry"), Owners: []string{"clerk"}, Steps: []platform.Step{
			{Name: "attempt", Retry: &platform.RetryPolicy{Attempts: 2, Backoff: time.Second}, Evaluate: &platform.Evaluate{Run: func(_ platform.Caller, run *platform.Run) (json.RawMessage, *kernel.Error) {
				if run.Now.Before(at.Add(time.Second)) {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transient failure")
				}
				return platform.Raw("recovered"), nil
			}}, Next: "park"}, park, {Name: "end", End: true},
		}}
		fault := platform.Flow{Name: "handled", Title: "Handled", Version: 1, Start: start("fault"), Owners: []string{"clerk"}, Steps: []platform.Step{
			{Name: "attempt", NoRetry: true, Evaluate: failure, Fault: "park"}, park, {Name: "end", End: true},
		}}
		undo := platform.Flow{Name: "undo", Title: "Undo", Version: 1, Start: start("undo"), Owners: []string{"clerk"}, Steps: []platform.Step{
			{Name: "reserve", Act: shopAct("reserve"), Undo: shopAct("release"), Next: "attempt"},
			{Name: "attempt", NoRetry: true, Evaluate: failure},
		}}
		return composeTenant(t, "application-current-flow", []Seat{seatOf("ana", "shop:clerk", "flow:admin")}, work.New("application-current-flow"), flow.New("application-current-flow"), newShop("application-current-flow", retry, fault, undo))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	root := platform.AssetRef{App: "shop", Kind: platform.AssetApp, Name: "shop"}
	read := func(tn *Tenant, id, state string, failed bool, now time.Time) ApplicationRun {
		t.Helper()
		page, err := tn.ApplicationRuns(memberOf(t, tn, "ana"), root, 0, 100, now, ApplicationRunFilter{Kind: "flow"})
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(page.Runs, func(run ApplicationRun) bool { return run.ID == id })
		if i < 0 || page.Runs[i].State != state || (page.Runs[i].Error != "") != failed {
			t.Fatalf("%s current run: %+v", id, page)
		}
		expectedFailed := 0
		for _, run := range page.Runs {
			if run.Error != "" || run.State == "stuck" {
				expectedFailed++
			}
		}
		if page.Summary.Failed != expectedFailed {
			t.Fatalf("current failure count: %+v", page)
		}
		return page.Runs[i]
	}
	decide(t, tn, "ana", "shop", "shop.order.place", "shop.order", "retry-one", map[string]any{"item": "ordinary"}, at)
	tn.Work(at)
	if run := read(tn, "shop.recover:retry-one", "waiting", true, at); run.Node != "attempt" {
		t.Fatalf("retry lost current step: %+v", run)
	}
	runJob(t, tn, flow.ID, "timers", at.Add(time.Second))
	read(tn, "shop.recover:retry-one", "waiting", false, at.Add(time.Second))
	decide(t, tn, "ana", "shop", "shop.order.place", "shop.order", "fault-one", map[string]any{"item": "ordinary"}, at.Add(time.Second))
	tn.Work(at.Add(time.Second))
	read(tn, "shop.handled:fault-one", "waiting", false, at.Add(time.Second))
	for _, id := range []string{"shop.recover:retry-one", "shop.handled:fault-one"} {
		instance, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), id)
		decide(t, tn, "ana", flow.ID, flow.SchemaFlowSkip, flow.InstanceType, id, map[string]any{"token": instance.Tokens[0].ID}, at.Add(2*time.Second))
		read(tn, id, "done", false, at.Add(2*time.Second))
	}
	decide(t, tn, "ana", "shop", "shop.order.place", "shop.order", "undo-done", map[string]any{"item": "ordinary"}, at.Add(2*time.Second))
	decide(t, tn, "ana", "shop", "shop.order.place", "shop.order", "undo-stuck", map[string]any{"item": "stuck"}, at.Add(2*time.Second))
	tn.Work(at.Add(2 * time.Second))
	read(tn, "shop.undo:undo-done", "compensated", true, at.Add(2*time.Second))
	read(tn, "shop.undo:undo-stuck", "compensating", true, at.Add(2*time.Second))
	for attempt := 1; attempt <= 4; attempt++ {
		runJob(t, tn, flow.ID, "timers", at.Add(time.Duration(attempt)*time.Hour))
	}
	read(tn, "shop.undo:undo-stuck", "stuck", true, at.Add(4*time.Hour))
	instance, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), "shop.undo:undo-stuck")
	decide(t, tn, "ana", flow.ID, flow.SchemaFlowSkip, flow.InstanceType, instance.ID, map[string]any{"token": instance.Tokens[0].ID}, at.Add(4*time.Hour))
	read(tn, instance.ID, "compensated", true, at.Add(4*time.Hour))
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	read(recovered, "shop.recover:retry-one", "done", false, at.Add(4*time.Hour))
	read(recovered, "shop.handled:fault-one", "done", false, at.Add(4*time.Hour))
	read(recovered, instance.ID, "compensated", true, at.Add(4*time.Hour))
}

type applicationRunsShop struct{ *shop }

func (s *applicationRunsShop) Manifest() platform.Manifest {
	m := s.shop.Manifest()
	schema := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"value": {Type: "integer"}}, Required: []string{"value"}}
	m.Operations = []platform.Operation{{Name: "calculate", Title: "Calculate", Description: "Return the supplied value", Roles: []string{"clerk"}, Input: schema, Output: schema, Binding: platform.OperationBinding{Kind: "native"}, Limits: platform.OperationLimits{TimeoutMillis: 1000, MemoryPages: 64, MaxInputBytes: 4096, MaxOutputBytes: 4096}}}
	return m
}

func (*applicationRunsShop) Compute(_ context.Context, _ string, input json.RawMessage) (json.RawMessage, error) {
	return slices.Clone(input), nil
}

func TestApplicationRunsComputeParentSubject(t *testing.T) {
	const tenant = "application-compute-subject"
	at := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		process := platform.Flow{Name: "calculate", Title: "Calculate", Version: 1, Start: platform.Start{On: []string{"shop.order.place"}, Begin: func(_ platform.Caller, event platform.Event) (string, any, bool) {
			return event.Record.GetSubmission().GetTarget().GetId(), nil, true
		}}, Steps: []platform.Step{{Name: "calculate", Operation: &platform.OperationStep{Request: func(platform.Caller, *platform.Run) (platform.OperationRequest, *kernel.Error) {
			return platform.OperationRequest{App: "shop", Name: "calculate", Inputs: platform.Raw(map[string]int{"value": 3})}, nil
		}}}}}
		return composeTenant(t, tenant, []Seat{seatOf("ana", "shop:clerk", "flow:member"), seatOf("admin", "shop:clerk", "flow:admin", "platform:admin")}, work.New(tenant), flow.New(tenant), &applicationRunsShop{newShop(tenant, process)})
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	decide(t, tn, "ana", "shop", "shop.order.place", "shop.order", "one", map[string]any{"item": "ordinary"}, at)
	tn.Work(at)
	root := platform.AssetRef{App: "shop", Kind: platform.AssetApp, Name: "shop"}
	parent := flow.InstanceType + "/shop.calculate:one"
	read := func(tn *Tenant, member, subject string, count int) ApplicationRunPage {
		t.Helper()
		page, err := tn.ApplicationRuns(memberOf(t, tn, member), root, 0, 100, at, ApplicationRunFilter{Kind: "compute", Subject: subject})
		if err != nil || page.Total != count {
			t.Fatalf("%s parent-filtered calls: %+v %v", member, page, err)
		}
		for _, run := range page.Runs {
			if run.Subject != parent {
				t.Fatalf("original parent was not retained: %+v", run)
			}
		}
		return page
	}
	page := read(tn, "ana", parent, 1)
	if page.Runs[0].Node != "calculate" {
		t.Fatalf("current compute step missing: %+v", page)
	}
	call, err := tn.InvokeOperation(memberOf(t, tn, "ana"), platform.OperationRequest{App: "shop", Name: "calculate", Key: "from-source", Inputs: platform.Raw(map[string]int{"value": 4}), Sources: []string{parent}}, at)
	if err != nil {
		t.Fatal(err)
	}
	read(tn, "ana", parent, 2)
	read(tn, "ana", parent+"-other", 0)
	read(tn, "admin", "", 0)
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	read(recovered, "ana", parent, 2)
	// Current source permission is still required, even for the member whose
	// invocation and parent were readable when the intent was accepted.
	tn.app(flow.ID).Manifest().Actions.UsePolicies(func() []authz.Policy {
		return []authz.Policy{{ID: "deny-flow-source", Effect: "deny", Permission: flow.InstanceType + ".read"}}
	})
	if _, err := tn.ReadOperation(memberOf(t, tn, "ana"), call.ID); err == nil {
		t.Fatal("revoked source remained readable")
	}
	read(tn, "ana", parent, 0)
}
