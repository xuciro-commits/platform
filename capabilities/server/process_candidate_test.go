package platformserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestProcessCandidateClosesNativeBindingsAndRecovers(t *testing.T) {
	for _, industry := range []string{"hospitality", "manufacturing"} {
		t.Run(industry, func(t *testing.T) {
			compose := func() *Tenant {
				seat := func(id, role string) Seat {
					return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
				}
				tn, err := NewTenant(industry, NewConsole(industry, seat("builder", build.Builder), seat("operator", build.User)), work.New(industry), flow.New(industry), build.New(industry))
				if err != nil {
					t.Fatal(err)
				}
				return tn
			}
			tn := compose()
			builder, _ := tn.Member("builder")
			operator, _ := tn.Member("operator")
			var journal []Entry
			tn.Record = func(e Entry) { journal = append(journal, e) }
			fail := false
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
				if fail {
					return nil, errors.New("injected append failure")
				}
				journal = append(journal, e)
				return e.Body, nil
			}
			at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
			key := 0
			submit := func(schema, typ, id string, payload any) {
				t.Helper()
				key++
				raw, _ := json.Marshal(payload)
				if _, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, at); err != nil {
					t.Fatalf("%s: %s", schema, err.Message)
				}
			}
			name := "inspection"
			title := "Inspection"
			if industry == "hospitality" {
				name, title = "request", "Guest request"
			}
			typ := build.TypeOf(name)
			object := build.Object{Name: name, Title: title, Fields: []build.Field{{Name: "note", Title: "Note", Type: "text"}}, States: []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}, {Name: "rejected", Title: "Rejected"}}, Actions: []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}, {Name: "reject", Title: "Reject", From: []string{"open"}, To: "rejected"}}}
			submit(build.ObjectType+".create", build.ObjectType, "O", map[string]any{"name": object.Name, "title": object.Title, "fields": object.Fields, "states": object.States, "actions": object.Actions})
			submit(build.SchemaPublish, build.ObjectType, "O", map[string]any{})
			// Named objects can share every field/state shape without sharing
			// their runtime Go identity or their records.
			submit(build.ObjectType+".create", build.ObjectType, "OTHER", map[string]any{"name": name + "other", "title": object.Title, "fields": object.Fields, "states": object.States, "actions": object.Actions})
			submit(build.SchemaPublish, build.ObjectType, "OTHER", map[string]any{})
			steps := []build.ProcessStep{{Name: "review", Ask: build.User, Answers: []string{"approve", "reject"}, Branches: map[string]string{"approve": "close", "reject": "reject"}}, {Name: "close", Act: "close"}, {Name: "reject", Act: "reject"}}
			submit(build.ProcessType+".create", build.ProcessType, "P", map[string]any{"name": "review", "title": title + " review", "object": typ, "when": "open", "steps": steps})
			submit(typ+".create", typ, "TEST", map[string]string{"note": "PRODUCTION"})
			before, count := snapshot(tn), len(journal)
			preview, err := tn.PreviewRelease(builder, platform.AssetFlow, "P")
			if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
				t.Fatalf("flow preview: %+v %v", preview, err)
			}
			if snapshot(tn) != before || len(journal) != count {
				t.Fatal("preview installed a flow or changed the tenant")
			}
			answer, expectedState := "approve", "done"
			if industry == "manufacturing" {
				answer, expectedState = "reject", "rejected"
			}
			request := CandidateSimulationRequest{ProcessID: "P", As: operator.ID, At: at, Steps: []SimulationStep{
				{Type: typ, ID: "TEST", Action: typ + ".create", Payload: json.RawMessage(`{"note":"FIXED SAMPLE"}`), AdvanceSeconds: 2, Expect: "accepted"},
				{Type: typ, ID: "TEST", As: builder.ID, Flow: "build.review", Step: "review", Answer: answer, Expect: "refused"},
				{Type: typ, ID: "TEST", Flow: "build.review", Step: "review", Answer: answer, AdvanceSeconds: 2, Expect: "accepted"},
				{Type: typ, ID: "TEST", Action: typ + ".create", Payload: json.RawMessage(`{"note":"DUPLICATE"}`), Expect: "refused"},
			}}
			planSteps := make([]build.TestStep, 0, len(request.Steps))
			for _, step := range request.Steps {
				payload := string(step.Payload)
				if payload == "" {
					payload = "{}"
				}
				planSteps = append(planSteps, build.TestStep{Type: step.Type, ID: step.ID, Action: step.Action, Payload: payload, Expect: step.Expect, As: step.As, AdvanceSeconds: step.AdvanceSeconds, Flow: step.Flow, Step: step.Step, Answer: step.Answer})
			}
			submit(build.TestPlanType+".create", build.TestPlanType, "PLAN", map[string]any{"title": "Workflow fixed samples", "process": "P", "as": operator.ID, "at": at, "steps": planSteps})
			for _, bad := range []map[string]any{{"object": "O"}, {"process": ""}, {"steps": []build.TestStep{{Type: typ, ID: "TEST", Action: typ + ".create", Flow: "build.review", Step: "review", Answer: answer, Payload: "{}", Expect: "accepted"}}}} {
				key++
				raw, _ := json.Marshal(bad)
				if _, problem := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: build.TestPlanType, Id: "PLAN"}, Schema: &pb.SchemaRef{Name: build.TestPlanType + ".edit", Version: 1}, Payload: raw}, at); problem == nil {
					t.Fatal("invalid workflow plan patch was saved")
				}
			}
			before, count = snapshot(tn), len(journal)
			result, problem := tn.SimulateCandidate(builder, request)
			if problem != nil || !result.Recovered || result.Passed == nil || !*result.Passed || result.CandidateID != preview.CandidateID {
				t.Fatalf("workflow simulation: %+v %v", result, problem)
			}
			if len(result.Steps[0].Flows) != 1 || result.Steps[0].Flows[0].State != "waiting" || len(result.Steps[0].Tasks) != 1 {
				t.Fatalf("native ask not exposed: %+v", result.Steps[0])
			}
			if result.Steps[0].Flows[0].Dependencies != preview.CandidateID || result.Steps[0].Flows[0].Release != "" {
				t.Fatalf("candidate run did not bind exact development dependencies: %+v", result.Steps[0].Flows[0])
			}
			last := result.Steps[2]
			if len(last.Flows) != 1 || last.Flows[0].State != "done" || len(last.Changes) != 1 || !bytes.Contains(last.Changes[0].Record, []byte(`"state":"`+expectedState+`"`)) {
				t.Fatalf("native answer branch did not act: %+v", last)
			}
			repeated, problem := tn.SimulateCandidate(builder, request)
			if problem != nil || !reflect.DeepEqual(result, repeated) || snapshot(tn) != before || len(journal) != count {
				t.Fatal("fixed workflow inputs changed on repeat or touched production")
			}
			invalidRequest := request
			invalidRequest.ObjectID = "O"
			if _, problem := tn.SimulateCandidate(builder, invalidRequest); problem == nil {
				t.Fatal("accepted two candidate roots")
			}
			invalidRequest = request
			invalidRequest.Steps = append([]SimulationStep(nil), request.Steps...)
			invalidRequest.Steps[0].AdvanceSeconds = -1
			if _, problem := tn.SimulateCandidate(builder, invalidRequest); problem == nil {
				t.Fatal("accepted a backwards test clock")
			}
			if _, problem := tn.SimulateCandidate(operator, request); problem == nil {
				t.Fatal("operator ran a private candidate test")
			}
			if _, err := tn.PreviewRelease(operator, platform.AssetFlow, "P"); err == nil {
				t.Fatal("operator reviewed a private workflow")
			}
			foreign := builder
			foreign.Tenant = "other"
			if _, err := tn.PreviewRelease(foreign, platform.AssetFlow, "P"); err == nil {
				t.Fatal("foreign builder reviewed a workflow")
			}
			fail = true
			if _, err := tn.SaveReleaseCandidate(builder, platform.AssetFlow, "P", preview.CandidateID, "save", at); err == nil || snapshot(tn) != before || len(journal) != count {
				t.Fatal("failed save exposed a candidate")
			}
			fail = false
			saved, err := tn.SaveReleaseCandidate(builder, platform.AssetFlow, "P", preview.CandidateID, "save", at)
			if err != nil || saved != preview.CandidateID {
				t.Fatal(err)
			}
			candidate, err := platform.ReadCandidate(saved, tn.releaseCandidates[saved])
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, asset := range candidate.Assets {
				if asset.Ref.Kind != platform.AssetFlow {
					continue
				}
				found = true
				if len(asset.Requires) != 3 || !bytes.Contains(asset.Body, []byte(`"approve":"close"`)) {
					t.Fatalf("missing native definition/bindings: %s", asset.Body)
				}
				var envelope platform.FlowReleaseDescriptor
				_ = json.Unmarshal(asset.Body, &envelope)
				var forged map[string]json.RawMessage
				_ = json.Unmarshal(envelope.Definition, &forged)
				var forgedSteps []build.ProcessStep
				_ = json.Unmarshal(forged["steps"], &forgedSteps)
				forgedSteps[1].Act = "reject"
				forged["steps"], _ = json.Marshal(forgedSteps)
				envelope.Definition, _ = json.Marshal(forged)
				forgedAsset := asset
				forgedAsset.Body, _ = json.Marshal(envelope)
				if _, err := build.ProcessFromReleaseAsset(forgedAsset); err == nil {
					t.Fatal("accepted a definition that did not compile to its binding envelope")
				}
				// The abstract release validator must reject an omitted compiled action
				// dependency even if a caller can recompute the outer content digest.
				asset.Requires = asset.Requires[:1]
				corrupt := append([]platform.ReleaseAsset(nil), candidate.Assets...)
				for i := range corrupt {
					if corrupt[i].Ref == asset.Ref {
						corrupt[i] = asset
					}
				}
				if _, err := platform.Candidate([]platform.AssetRef{asset.Ref}, corrupt); err == nil {
					t.Fatal("accepted an omitted native binding")
				}
			}
			if !found {
				t.Fatal("candidate omitted the workflow")
			}
			if _, err := tn.ActivateRelease(builder, saved, "early", at); err == nil {
				t.Fatal("activated an uninstalled workflow")
			}
			submit(build.SchemaProcess, build.ProcessType, "P", map[string]any{})
			if _, err := tn.ActivateRelease(builder, saved, "active", at); err != nil {
				t.Fatal(err)
			}
			// Native version ordinals are retained in records/instances; the release
			// identity describes semantic content, so unchanged republication matches.
			again, err := tn.PreviewRelease(builder, platform.AssetFlow, "P")
			if err != nil || again.CandidateID != saved {
				t.Fatalf("unchanged flow changed semantic candidate: %+v %v", again, err)
			}
			submit(build.ProcessType+".edit", build.ProcessType, "P", map[string]any{"steps": []build.ProcessStep{{Name: "reject", Act: "reject"}}})
			changed, err := tn.PreviewRelease(builder, platform.AssetFlow, "P")
			if err != nil || changed.Diagnostic != "" || changed.CandidateID == saved {
				t.Fatalf("branch/action change was not reviewed: %+v %v", changed, err)
			}
			// An invalid source mutation is blocked even with no running instance.
			submit(build.ObjectType+".edit", build.ObjectType, "O", map[string]any{"actions": []build.Action{object.Actions[1]}})
			invalid, err := tn.PreviewRelease(builder, platform.AssetObject, "O")
			if err != nil || !strings.Contains(invalid.Diagnostic, "not declared") {
				t.Fatalf("removed a published workflow dependency: %+v %v", invalid, err)
			}
			submit(build.ObjectType+".edit", build.ObjectType, "O", map[string]any{"actions": object.Actions})
			for _, invalid := range []struct {
				draft   map[string]any
				message string
			}{
				{map[string]any{"states": []build.State{{Name: "done", Title: "Done"}, {Name: "rejected", Title: "Rejected"}}, "actions": []build.Action{{Name: "close", Title: "Close", From: []string{"done"}, To: "done"}, {Name: "reject", Title: "Reject", From: []string{"done"}, To: "rejected"}}}, "no process start state"},
				{map[string]any{"access": []build.Access{{Role: build.User, Read: "own", Create: true}}}, "must read all"},
			} {
				submit(build.ObjectType+".edit", build.ObjectType, "O", invalid.draft)
				preview, err := tn.PreviewRelease(builder, platform.AssetObject, "O")
				if err != nil || !strings.Contains(preview.Diagnostic, invalid.message) {
					t.Fatalf("incompatible process source: %+v %v", preview, err)
				}
				key++
				if _, problem := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: build.ObjectType, Id: "O"}, Schema: &pb.SchemaRef{Name: build.SchemaPublish, Version: 1}, Payload: []byte(`{}`)}, at); problem == nil {
					t.Fatal("object publication bypassed process dependency validation")
				}
				submit(build.ObjectType+".edit", build.ObjectType, "O", map[string]any{"states": object.States, "actions": object.Actions, "access": []build.Access{}})
			}
			raw, _, err := tn.Snapshot(func() int64 { return 0 })
			if err != nil {
				t.Fatal(err)
			}
			restored := compose()
			if err := restored.Restore(raw); err != nil {
				t.Fatal(err)
			}
			if snapshot(restored) != snapshot(tn) || restored.ActiveRelease() != saved {
				t.Fatal("snapshot lost workflow candidate/active release")
			}
			plan := platformGetPlan(t, restored, builder, "PLAN", at)
			if len(plan.Steps) != len(planSteps) || !reflect.DeepEqual(plan.Steps, planSteps) || plan.Process != "P" || plan.Object != "" {
				t.Fatal("restoration lost the saved workflow inputs")
			}
			CheckReplay(t, tn, journal, compose)
		})
	}
}

func platformGetPlan(t *testing.T, tenant *Tenant, member platform.Member, id string, at time.Time) build.TestPlan {
	t.Helper()
	view, err := tenant.RecordOf(member, build.TestPlanType, id, at)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(view.Record)
	var plan build.TestPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}
