package platformserver

import (
	"bytes"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

func TestMigrationMapsRetainedFlowWrites(t *testing.T) {
	for _, reference := range []bool{false, true} {
		name := "retype"
		if reference {
			name = "rebind"
		}
		t.Run(name, func(t *testing.T) {
			compose := func() *Tenant {
				return builderTenant(t, "mapped-flow-"+name, work.New("mapped-flow-"+name), flow.New("mapped-flow-"+name))
			}
			tn := compose()
			entries := []Entry{}
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
			at := txnAt
			field := map[string]any{"name": "value", "title": "Value", "type": "text", "sourceType": "user-input"}
			original, nextValue := "12", "25"
			if reference {
				for _, target := range []string{"oldtarget", "newtarget"} {
					decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, target, map[string]any{"name": target, "title": target, "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
					decide(t, tn, "builder", build.ID, build.SchemaPublish, build.ObjectType, target, map[string]any{}, at)
					for _, id := range []string{"a", "b", "unmapped"} {
						decide(t, tn, "user", build.ID, "build."+target+".create", "build."+target, id, map[string]any{"name": id}, at)
					}
				}
				field["type"], field["ref"] = "reference", "build.oldtarget"
				original, nextValue = "a", "b"
			}
			states := []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}
			actions := []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}}
			decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "O", map[string]any{"name": "sample", "title": "Sample", "fields": []any{field}, "states": states, "actions": actions}, at)
			decide(t, tn, "builder", build.ID, build.SchemaPublish, build.ObjectType, "O", map[string]any{}, at)
			steps := []build.ProcessStep{{Name: "ask", Title: "Confirm", Kind: "ask", Ask: build.User, Answers: []string{"yes"}, Next: "write"}, {Name: "write", Kind: "action", Act: "edit", Inputs: map[string]platform.Binding{"value": {Source: "literal", Value: platform.Raw(nextValue)}}, Next: "close"}, {Name: "close", Kind: "action", Act: "close"}}
			decide(t, tn, "builder", build.ID, "build.process.create", build.ProcessType, "P", map[string]any{"name": "review", "title": "Review", "object": "build.sample", "when": "open", "steps": steps}, at)
			decide(t, tn, "builder", build.ID, build.SchemaProcess, build.ProcessType, "P", map[string]any{}, at)
			builder := memberOf(t, tn, "builder")
			save := func(key string) string {
				t.Helper()
				drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetFlow, ID: "P"}}
				preview, err := tn.PreviewReleaseDrafts(builder, drafts)
				if err != nil || preview.Diagnostic != "" {
					t.Fatalf("preview %+v %v", preview, err)
				}
				id, err := tn.SaveReleaseCandidates(builder, drafts, preview.CandidateID, key, at)
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			from := save("source")
			if _, err := tn.ActivateRelease(builder, from, "source-activate", at); err != nil {
				t.Fatal(err)
			}
			decide(t, tn, "user", build.ID, "build.sample.create", "build.sample", "A", map[string]any{"value": original}, at)
			at = at.Add(2 * time.Second)
			tn.Work(at)
			tasks, _, _ := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{})
			if len(tasks) != 1 {
				t.Fatal("source did not wait")
			}
			step := platform.MigrateStep{Kind: "retype", Types: []string{"build.sample"}, Batch: 1, OnFailure: "reject"}
			if reference {
				field["ref"] = "build.newtarget"
				step.Kind, step.Mapping = "rebind", map[string]string{"a": "a", "b": "b"}
				step.Fields = []platform.MigrateField{{From: "value", To: "value"}}
			} else {
				field["type"] = "decimal"
				parse := platform.Expr{Kind: "decimal", Op: "parse-decimal", Args: []platform.Expr{platform.ExprReference("text", "value")}}
				step.Fields = []platform.MigrateField{{From: "value", To: "value", Expr: &parse}}
				steps[1].Inputs = map[string]platform.Binding{"value": {Source: "literal", Value: platform.Raw(99)}}
			}
			decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "O", map[string]any{"fields": []any{field}}, at)
			decide(t, tn, "builder", build.ID, "build.process.edit", build.ProcessType, "P", map[string]any{"steps": steps}, at)
			to := save("target")
			plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.sample"}, Steps: []platform.MigrateStep{step}}
			review, err := tn.ReviewMigrationPlan(builder, plan, at)
			if err != nil {
				t.Fatal(err)
			}
			decide(t, tn, "builder", build.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, at)
			decide(t, tn, "builder", build.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, at)
			if _, err := tn.ActivateRelease(builder, to, "target-activate", at); err != nil {
				t.Fatal(err)
			}
			draft := tn.newStagedDecision()
			parent := platform.NewCaller(draft, memberOf(t, tn, "user"), flow.ID, false, false)
			starting, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), "build.review:A")
			execution, bindErr := (hostView{t: tn, decision: draft}).FlowExecution(parent, build.ID, "review", 1, host.FlowBinding{Dependencies: starting.Dependencies, Release: starting.Release})
			if bindErr != nil {
				t.Fatal(bindErr)
			}
			caller := execution.Caller(parent, memberOf(t, tn, "user"), build.ID)
			beforeRejected, _ := acceptedRowOf("build.sample", "A", draft.records.types["build.sample"].rows.get("A"))
			_, invalid := platform.Attempt(caller, func() (*pb.ChangeRecord, *kernel.Error) {
				return execution.Submit(caller, submission(tn, caller.Member, build.ID, "build.sample.edit", "build.sample", "A", map[string]any{"value": "unmapped"}), at)
			})
			if invalid == nil {
				t.Fatal("invalid retained conversion accepted")
			}
			afterRejected, _ := acceptedRowOf("build.sample", "A", draft.records.types["build.sample"].rows.get("A"))
			if !bytes.Equal(jsonOf(beforeRejected), jsonOf(afterRejected)) {
				t.Fatal("invalid conversion exposed a partial target or source image")
			}
			if _, refusal := execution.Submit(caller, submission(tn, caller.Member, build.ID, "build.sample.edit", "build.sample", "A", map[string]any{"value": nextValue}), at); refusal != nil {
				t.Fatalf("retained write refusal: %#v; draft failure: %#v", refusal, draft.failure)
			}
			decide(t, tn, "user", work.ID, "work.task.complete", work.TaskType, tasks[0].ID, map[string]any{"answer": "yes"}, at)
			at = at.Add(2 * time.Second)
			tn.Work(at)
			et := tn.records.types["build.sample"]
			want := "25"
			if reference {
				want = "b"
			}
			if businessField(et.info, et.rows.get("A").value, "state") != "done" || businessField(et.info, et.rows.get("A").value, "value") != want {
				instance, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), "build.review:A")
				t.Fatalf("old conversion did not finish: %s; flow: %s", jsonOf(et.rows.get("A").value.Interface()), jsonOf(instance))
			}
			source := projectionFor(et.rows.get("A").projection, from)
			if source == nil || !bytes.Contains(source.Value, []byte(`"value":"`+nextValue+`"`)) {
				t.Fatalf("old source write was lost: %+v", source)
			}
			recovered := compose()
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			before, _ := acceptedRowOf(et.info.Type, "A", et.rows.get("A"))
			after, _ := acceptedRowOf(et.info.Type, "A", recovered.records.types[et.info.Type].rows.get("A"))
			if !bytes.Equal(jsonOf(before), jsonOf(after)) {
				t.Fatal("recovery reran the conversion")
			}
		})
	}
}
