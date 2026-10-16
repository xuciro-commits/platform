package platformserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestCapabilityActionPreservesApprovalReceipt(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	tenant, err := NewTenant("cap-approval", NewConsole("cap-approval", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder, flow.ID: flow.Admin}}}, Seat{Subjects: []string{"user"}, Member: platform.Member{ID: "user", Roles: map[string]string{build.ID: build.User}}}), work.New("cap-approval"), flow.New("cap-approval"), build.New("cap-approval"))
	if err != nil {
		t.Fatal(err)
	}
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { return entry.Body, nil }
	builder, _ := tenant.Member("builder")
	user, _ := tenant.Member("user")
	submit := func(member platform.Member, key, schema, typ, id string, payload any) {
		t.Helper()
		if _, problem := tenant.Submit(member, &pb.Submission{TenantId: tenant.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at); problem != nil {
			t.Fatal(problem.Message)
		}
	}
	object := map[string]any{"name": "review", "title": "Review", "fields": []any{}, "states": []build.State{{Name: "open", Title: "Open"}, {Name: "pending", Title: "Pending"}, {Name: "approved", Title: "Approved"}}, "actions": []map[string]any{{"name": "approve", "title": "Approve", "from": []string{"open"}, "to": "approved", "roles": []string{build.User}, "approval": map[string]any{"pending": "pending", "levels": []map[string]string{{"title": "Builder", "role": build.Builder}}}}}}
	submit(builder, "object", build.ObjectType+".create", build.ObjectType, "O", object)
	submit(builder, "publish", build.SchemaPublish, build.ObjectType, "O", struct{}{})
	submit(user, "row", "build.review.create", "build.review", "R", struct{}{})
	request := platform.CapabilityInvocation{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: "build.review.approve"}, Key: "approve", Target: "R", Inputs: json.RawMessage(`{}`)}
	result, problem := tenant.InvokeCapability(user, request, at)
	if problem != nil {
		t.Fatal(problem.Message)
	}
	var receipt pb.ChangeRecord
	if result.State != "pending" || protojson.Unmarshal(result.Result, &receipt) != nil || receipt.GetSubmission().GetTarget().GetType() != work.ApprovalType {
		t.Fatalf("approval was mislabeled as an accepted business change: %+v", result)
	}
	again, problem := tenant.InvokeCapability(user, request, at.Add(time.Minute))
	if problem != nil || string(again.Result) != string(result.Result) || again.State != "pending" {
		t.Fatal("retry replaced the canonical approval receipt")
	}
	submit(builder, "process", build.ProcessType+".create", build.ProcessType, "P", map[string]any{"name": "review", "title": "Review flow", "object": "build.review", "when": "open", "steps": []build.ProcessStep{{Name: "approve", Kind: "action", Act: "approve", Next: "end"}, {Name: "end", Kind: "end"}}})
	submit(builder, "process-publish", build.SchemaProcess, build.ProcessType, "P", struct{}{})
	submit(user, "second-row", "build.review.create", "build.review", "R2", struct{}{})
	tenant.Work(at)
	process, _ := tenant.RecordOf(builder, flow.InstanceType, "build.review:R2", at)
	pending := process.Record.(flow.FlowInstance)
	if pending.State != "waiting" || len(pending.Tokens) != 1 || pending.Tokens[0].Waits != "approval" {
		t.Fatalf("flow ran past an unapproved owner action: %+v", pending)
	}
	payload := platform.Raw(map[string]string{"note": "Approved"})
	if _, problem := tenant.Submit(builder, &pb.Submission{TenantId: tenant.ID, PrincipalId: builder.ID, Authority: work.ID, IdempotencyKey: "decision", Target: &pb.EntityRef{Type: work.ApprovalType, Id: pending.Tokens[0].Child}, Schema: &pb.SchemaRef{Name: work.ApprovalType + ".approve", Version: 1}, Payload: payload}, at.Add(time.Second)); problem != nil {
		t.Fatal(problem.Message)
	}
	tenant.Work(at.Add(2 * time.Second))
	final, _ := tenant.RecordOf(builder, flow.InstanceType, pending.ID, at.Add(2*time.Second))
	if final.Record.(flow.FlowInstance).State != "done" {
		t.Fatal("the original approved action did not resume its waiting flow token")
	}

}

func TestApprovalDoesNotBindUnrelatedRelease(t *testing.T) {
	const id = "approval-unrelated-release"
	at := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return builderTenant(t, id, work.New(id), flow.New(id)) }
	tenant := compose()
	var entries []Entry
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	object := map[string]any{
		"name": "review", "title": "Review", "fields": []any{},
		"states": []build.State{{Name: "open", Title: "Open"}, {Name: "pending", Title: "Pending"}, {Name: "approved", Title: "Approved"}, {Name: "rejected", Title: "Rejected"}},
		"actions": []build.Action{{
			Name: "approve", Title: "Approve", From: []string{"open"}, To: "approved", Roles: []string{build.User},
			Approval: &build.ActionApproval{Pending: "pending", Levels: []build.ApproverLevel{{Title: "Builder", Role: build.Builder}}},
		}},
	}
	publishObject(t, tenant, "builder", "REVIEW", object, at)
	publishObject(t, tenant, "builder", "OTHER", map[string]any{"name": "other", "title": "Other", "fields": []any{}}, at)
	builder := memberOf(t, tenant, "builder")
	activate := func(id, key string) string {
		t.Helper()
		preview, err := tenant.PreviewRelease(builder, platform.AssetObject, id)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("release %s: %+v %v", id, preview, err)
		}
		if _, err := tenant.SaveReleaseCandidate(builder, platform.AssetObject, id, preview.CandidateID, "save-"+key, at); err != nil {
			t.Fatal(err)
		}
		if _, err := tenant.ActivateRelease(builder, preview.CandidateID, "activate-"+key, at); err != nil {
			t.Fatal(err)
		}
		return preview.CandidateID
	}
	activate("OTHER", "other")
	decide(t, tenant, "user", build.ID, "build.review.create", "build.review", "R", map[string]any{}, at)
	result, problem := tenant.InvokeCapability(memberOf(t, tenant, "user"), platform.CapabilityInvocation{
		Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: "build.review.approve"},
		Key: "approve", Target: "R", Inputs: json.RawMessage(`{}`),
	}, at)
	if problem != nil || result.State != "pending" {
		t.Fatalf("unrelated release blocked approval: %+v %v", result, problem)
	}
	request, ok := platform.Get[work.ApprovalRequest](tenant.automation(work.ID, false), "build.approve")
	if !ok || request.Release != "" {
		t.Fatalf("approval selected an unrelated candidate: %+v", request)
	}
	decide(t, tenant, "builder", work.ID, work.ApprovalType+".approve", work.ApprovalType, request.ID, map[string]any{}, at)
	view, failure := tenant.RecordOf(builder, "build.review", "R", at)
	if failure != nil || view.Values["state"] != "approved" {
		t.Fatalf("approval did not execute its original action: %+v %v", view, failure)
	}
	// A remains installed after another application's B is activated. A new
	// waiting approval must retain A even when A is subsequently upgraded.
	source := activate("REVIEW", "review")
	activate("OTHER", "other-again")
	decide(t, tenant, "user", build.ID, "build.review.create", "build.review", "FROZEN", map[string]any{}, at)
	result, problem = tenant.InvokeCapability(memberOf(t, tenant, "user"), platform.CapabilityInvocation{
		Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: "build.review.approve"},
		Key: "approve-frozen", Target: "FROZEN", Inputs: json.RawMessage(`{}`),
	}, at)
	if problem != nil || result.State != "pending" {
		t.Fatalf("installed approval: %+v %v", result, problem)
	}
	request, ok = platform.Get[work.ApprovalRequest](tenant.automation(work.ID, false), "build.approve-frozen")
	if !ok || request.Release != source {
		t.Fatalf("approval lost the still-installed release: %+v", request)
	}
	actions := object["actions"].([]build.Action)
	actions[0].To = "rejected"
	decide(t, tenant, "builder", build.ID, "build.object.edit", build.ObjectType, "REVIEW", map[string]any{"actions": actions}, at)
	next, err := tenant.PreviewRelease(builder, platform.AssetObject, "REVIEW")
	if err != nil || next.Diagnostic != "" {
		t.Fatalf("upgraded release: %+v %v", next, err)
	}
	if _, err := tenant.SaveReleaseCandidate(builder, platform.AssetObject, "REVIEW", next.CandidateID, "save-review-next", at); err != nil {
		t.Fatal(err)
	}
	if _, err := tenant.ActivateRelease(builder, next.CandidateID, "blocked-review-next", at); err == nil || !strings.Contains(err.Error(), "holds build.review.approve under another release") {
		t.Fatalf("upgrade replaced a pending approval's retained action: %v", err)
	}
	decide(t, tenant, "builder", work.ID, work.ApprovalType+".approve", work.ApprovalType, request.ID, map[string]any{}, at)
	view, failure = tenant.RecordOf(builder, "build.review", "FROZEN", at)
	if failure != nil || view.Values["state"] != "approved" {
		t.Fatalf("upgrade changed the waiting approval's original action: %+v %v", view, failure)
	}
	if _, err := tenant.ActivateRelease(builder, next.CandidateID, "activate-review-next", at); err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, tenant, entries, compose)
}

func TestNativeAIFunctionUsesOwnerGrantsWithoutBuildRole(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	native := newSampleOwner("native-ai", platform.Manifest{ID: "source", Title: "Source", Version: "1", Actions: platform.NewCatalog(), Roles: []string{"viewer", "reader"}, Entities: []platform.Entity{{Type: "source.item", Title: "Item", Model: querySampleRow{}, Scope: platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"viewer": platform.ScopeTenant, "reader": platform.ScopeTenant}}, Seed: []any{querySampleRow{Record: platform.Record{ID: "S"}, Label: "Synthetic"}}}}, Functions: []platform.AIFunction{platform.RecordAdviceFunction("source.item", []string{"label"}, []string{"viewer"})}})
	tenant, err := NewTenant("native-ai", NewConsole("native-ai", Seat{Subjects: []string{"viewer"}, Member: platform.Member{ID: "viewer", Roles: map[string]string{"source": "viewer"}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{"source": "reader"}}}, seatOf("app:candidate-test", "platform:admin", "ai:admin")), ai.New("native-ai"), native, build.New("native-ai"))
	if err != nil {
		t.Fatal(err)
	}
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { return entry.Body, nil }
	if err := configureFunctionFixtureModel(tenant, "probe/model", at); err != nil {
		t.Fatal(err)
	}
	viewer, _ := tenant.Member("viewer")
	reader, _ := tenant.Member("reader")
	request := platform.CapabilityInvocation{Ref: platform.AssetRef{App: "source", Kind: platform.AssetFunction, Name: "record-advice"}, Key: "call", Inputs: json.RawMessage(`{"source":"S"}`)}
	result, problem := tenant.InvokeCapability(viewer, request, at)
	if problem != nil || result.State != "pending" || result.Call == "" {
		t.Fatalf("native owner grant incorrectly required a Builder role: %+v %v", result, problem)
	}
	count := len(tenant.outbound)
	request.Key = "denied"
	if _, problem := tenant.InvokeCapability(reader, request, at); problem == nil || len(tenant.outbound) != count {
		t.Fatal("gateway bypassed the native function owner grant")
	}
}

type querySampleRow struct {
	platform.Record
	Label string `json:"label"`
}
