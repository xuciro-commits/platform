package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"context"
	"fmt"
	"platformserver/internal/records"
	"testing"
	"time"

	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

// Each promised change crosses an actual journal close/reopen between batches
// and then continues the three flows bound to the unchanged source release.
func TestMigrationSixChangesResumeWaitingFlows(t *testing.T) {
	for _, kind := range []string{"default", "rename", "split", "merge", "rebind", "retire-action", "retire"} {
		t.Run(kind, func(t *testing.T) {
			for _, backend := range journalsUnderTest(t) {
				t.Run(backend.name, func(t *testing.T) {
					id := fmt.Sprintf("migration-inflight-%s-%d", kind, time.Now().UnixNano())
					defer backend.cleanup(t, id)
					journal := backend.open(t, id)
					defer func() { journal.Close() }()
					if _, err := journal.Entries(context.Background(), id, 0); err != nil {
						t.Fatal(err)
					}
					compose := func() *Tenant {
						return builderTenant(t, id, work.New(id), flow.New(id))
					}
					tn := compose()
					attach := func() {
						tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
							return journal.AppendAccepted(context.Background(), id, e, key, hash)
						}
					}
					attach()
					at := txnAt
					field := map[string]any{"name": "value", "title": "Value", "type": "text", "sourceType": "user-input"}
					original, updated := "12", "25"
					if kind == "rebind" {
						for _, target := range []string{"oldtarget", "newtarget"} {
							publishObject(t, tn, "builder", target, map[string]any{"name": target, "title": target, "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}}}, at)
							for _, value := range []string{"a", "b"} {
								decide(t, tn, "user", construction.ID, "build."+target+".create", "build."+target, value, map[string]any{"name": value}, at)
							}
						}
						field["type"], field["ref"] = "reference", "build.oldtarget"
						original, updated = "a", "b"
					}
					fields := []any{field, construction.Field{Name: "mark", Title: "Mark", Type: "text"}}
					if kind == "merge" {
						fields = append(fields, construction.Field{Name: "other", Title: "Other", Type: "text"})
					}
					states := []construction.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}, {Name: "rejected", Title: "Rejected"}}
					actions := []construction.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}, {Name: "reject", Title: "Reject", From: []string{"open"}, To: "rejected"}}
					if kind == "retire-action" {
						states = append(states, construction.State{Name: "pending", Title: "Pending"})
						actions = append(actions, construction.Action{Name: "hold", Title: "Hold", From: []string{"open"}, To: "done", Approval: &construction.ActionApproval{Pending: "pending", Rejected: "open", Levels: []construction.ApproverLevel{{Title: "Builder", Role: construction.Builder}}}})
					}
					inputs := map[string]platform.Binding{"value": {Source: "literal", Value: platform.Raw(updated)}}
					if kind == "retire" {
						inputs = map[string]platform.Binding{"mark": {Source: "literal", Value: platform.Raw(updated)}}
						actions[0].Conditions = []construction.Condition{{Field: "value", Operator: "=", Value: original, Message: "The original retained field must still be readable"}}
					}
					publishObject(t, tn, "builder", "O", map[string]any{"name": "sample", "title": "Sample", "fields": fields, "states": states, "actions": actions}, at)
					steps := []construction.ProcessStep{
						{Name: "ask", Title: "Confirm", Kind: "ask", Ask: construction.User, Answers: []string{"yes"}, Next: "write"},
						{Name: "write", Kind: "action", Act: "edit", Inputs: inputs, Next: "close"},
						{Name: "close", Kind: "action", Act: "close"},
					}
					decide(t, tn, "builder", construction.ID, "build.process.create", construction.ProcessType, "P", map[string]any{"name": "review", "title": "Review", "object": "build.sample", "when": "open", "steps": steps}, at)
					decide(t, tn, "builder", construction.ID, construction.SchemaProcess, construction.ProcessType, "P", map[string]any{}, at)
					save := func(key string) string {
						t.Helper()
						builder := memberOf(t, tn, "builder")
						drafts := []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetFlow, ID: "P"}}
						preview, err := tn.PreviewReleaseDrafts(builder, drafts)
						if err != nil || preview.Diagnostic != "" {
							t.Fatalf("preview: %+v %v", preview, err)
						}
						candidate, err := tn.SaveReleaseCandidates(builder, drafts, preview.CandidateID, key, at)
						if err != nil {
							t.Fatal(err)
						}
						return candidate
					}
					from := save("source")
					if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), from, "source-activate", at); err != nil {
						t.Fatal(err)
					}
					sourceBytes := bytes.Clone(tn.releases.raw(from))
					for _, rowID := range []string{"A", "B", "C"} {
						values := map[string]any{"value": original}
						if kind == "merge" {
							values["other"] = "8"
						}
						decide(t, tn, "user", construction.ID, "build.sample.create", "build.sample", rowID, values, at)
					}
					at = at.Add(2 * time.Second)
					tn.Work(at)
					history := map[string][]records.RecordChange{}
					for _, rowID := range []string{"A", "B", "C"} {
						instance, found := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), "build.review:"+rowID)
						if !found || instance.State != "waiting" || instance.Release != from {
							t.Fatalf("source flow did not wait: %+v", instance)
						}
						if kind == "retire-action" {
							decide(t, tn, "user", construction.ID, "build.sample.hold", "build.sample", rowID, map[string]any{}, at)
						}
						history[rowID] = records.CopyHistory(tn.records.types["build.sample"].rows.Get(rowID).RecordHistory())
					}
					step := platform.MigrateStep{Kind: kind, Types: []string{"build.sample"}, Batch: 2}
					want := map[string]string{"value": updated}
					switch kind {
					case "default":
						fields = append(fields, construction.Field{Name: "enabled", Title: "Enabled", Type: "boolean", Required: true})
						expr := platform.ExprLiteralValue("boolean", true)
						step.Fields = []platform.MigrateField{{To: "enabled", Expr: &expr}}
						want["enabled"] = "true"
					case "rename":
						field["readOnly"], field["aliasOf"] = true, "renamed"
						fields = append(fields, construction.Field{Name: "renamed", Title: "Renamed", Type: "text"})
						step.Fields = []platform.MigrateField{{From: "value", To: "renamed"}}
						steps[1].Inputs = map[string]platform.Binding{"renamed": {Source: "literal", Value: platform.Raw("new")}}
						want["renamed"] = updated
					case "split":
						field["readOnly"] = true
						fields = append(fields, construction.Field{Name: "left", Title: "Left", Type: "text"}, construction.Field{Name: "right", Title: "Right", Type: "text"})
						expr := platform.ExprReference("text", "value")
						step.Fields = []platform.MigrateField{{From: "value", To: "left", Expr: &expr}, {From: "value", To: "right", Expr: &expr}}
						steps[1].Inputs = map[string]platform.Binding{"left": {Source: "literal", Value: platform.Raw("new")}}
						want = map[string]string{"value": original, "left": updated, "right": updated}
					case "merge":
						field["readOnly"] = true
						fields[2] = construction.Field{Name: "other", Title: "Other", Type: "text", ReadOnly: true}
						fields = append(fields, construction.Field{Name: "joined", Title: "Joined", Type: "text"})
						expr := platform.Expr{Kind: "text", Op: "concat", Args: []platform.Expr{platform.ExprReference("text", "value"), platform.ExprReference("text", "other")}}
						step.Fields = []platform.MigrateField{{From: "value", To: "joined", Expr: &expr}, {From: "other", To: "joined", Expr: &expr}}
						steps[1].Inputs = map[string]platform.Binding{"joined": {Source: "literal", Value: platform.Raw("new")}}
						want = map[string]string{"value": original, "other": "8", "joined": updated + "8"}
					case "rebind":
						field["ref"] = "build.newtarget"
						step.Mapping = map[string]string{"a": "a", "b": "b"}
						step.Fields = []platform.MigrateField{{From: "value", To: "value"}}
					case "retire-action":
						actions = actions[:2]
						actions[0].To = "rejected"
						step.Action = "build.sample.hold"
					case "retire":
						field["readOnly"] = true
						step.Fields = []platform.MigrateField{{From: "value", To: "value"}}
						want = map[string]string{"value": original, "mark": updated}
					}
					decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "O", map[string]any{"fields": fields, "actions": actions}, at)
					decide(t, tn, "builder", construction.ID, "build.process.edit", construction.ProcessType, "P", map[string]any{"steps": steps}, at)
					to := save("target")
					plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.sample"}, Steps: []platform.MigrateStep{step}}
					review, err := tn.ReviewMigrationPlan(memberOf(t, tn, "builder"), plan, at)
					if err != nil {
						t.Fatal(err)
					}
					decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, at)
					request := submission(tn, memberOf(t, tn, "builder"), construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{})
					first, refusal := tn.Submit(memberOf(t, tn, "builder"), request, at)
					if refusal != nil {
						t.Fatal(refusal)
					}
					if migrationRun(t, tn, "M").State != "running" {
						t.Fatal("three source rows were not split into two batches")
					}
					position := journal.Position(id)
					journal.Close()
					journal = backend.reopen(t, id)
					entries, err := journal.Entries(context.Background(), id, 0)
					if err != nil {
						t.Fatal(err)
					}
					tn = compose()
					if err := tn.Replay(entries); err != nil {
						t.Fatal(err)
					}
					attach()
					again, refusal := tn.Submit(memberOf(t, tn, "builder"), request, at.Add(time.Hour))
					if refusal != nil || first.GetChangeId() != again.GetChangeId() || journal.Position(id) != position {
						t.Fatal("recovery repeated an accepted batch")
					}
					decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, at)
					if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), to, "target-activate", at); err != nil {
						t.Fatal(err)
					}
					if kind == "split" {
						// The oldest waiting flows now cross a second completed plan.
						fields[2] = construction.Field{Name: "left", Title: "Left", Type: "text", ReadOnly: true, AliasOf: "summary"}
						fields = append(fields, construction.Field{Name: "summary", Title: "Summary", Type: "text"})
						steps[1].Inputs = map[string]platform.Binding{"summary": {Source: "literal", Value: platform.Raw("new")}}
						decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "O", map[string]any{"fields": fields}, at)
						decide(t, tn, "builder", construction.ID, "build.process.edit", construction.ProcessType, "P", map[string]any{"steps": steps}, at)
						next := save("second-target")
						plan := platform.MigrationPlan{From: to, To: next, Scope: platform.MigrationScope{Type: "build.sample"}, Steps: []platform.MigrateStep{{Kind: "rename", Types: []string{"build.sample"}, Batch: 2, Fields: []platform.MigrateField{{From: "left", To: "summary"}}}}}
						review, err := tn.ReviewMigrationPlan(memberOf(t, tn, "builder"), plan, at)
						if err != nil {
							t.Fatal(err)
						}
						decide(t, tn, "builder", construction.ID, "build.migration.start", platform.MigrationRunType, "M2", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, at)
						decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M2", map[string]any{}, at)
						saved, _, err := tn.Snapshot(func() int64 { return 0 })
						if err != nil {
							t.Fatal(err)
						}
						tn = compose()
						if err := tn.Restore(saved); err != nil {
							t.Fatal(err)
						}
						attach()
						decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M2", map[string]any{}, at)
						if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), next, "second-target-activate", at); err != nil {
							t.Fatal(err)
						}
						want["summary"] = updated
					}
					if kind == "retire" || kind == "split" || kind == "merge" {
						if got := refuse(t, tn, "user", construction.ID, "build.sample.edit", "build.sample", "A", map[string]any{"value": "forbidden"}, at); got == "ok" {
							t.Fatal("the current version wrote a retired field")
						}
					}
					tasks, _, _ := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{})
					for _, task := range tasks {
						if task.State == "open" {
							decide(t, tn, "user", work.ID, "work.task.complete", work.TaskType, task.ID, map[string]any{"answer": "yes"}, at)
						}
					}
					at = at.Add(2 * time.Second)
					tn.Work(at)
					for _, rowID := range []string{"A", "B", "C"} {
						et := tn.records.types["build.sample"]
						row := et.rows.Get(rowID)
						instance, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), "build.review:"+rowID)
						if instance.State != "done" || instance.Release != from || businessField(et.info, row.Value, "state") != "done" {
							t.Fatalf("retained flow did not finish: %s; record: %s", jsonOf(instance), jsonOf(row.Value.Interface()))
						}
						fullHistory := row.RecordHistory()
						if len(fullHistory) < len(history[rowID]) || !bytes.Equal(jsonOf(history[rowID]), jsonOf(fullHistory[:len(history[rowID])])) {
							t.Fatal("migration or retained execution rewrote historical decisions")
						}
						view, refusal := tn.RecordOf(memberOf(t, tn, "user"), "build.sample", rowID, at)
						if refusal != nil || len(view.History) < len(history[rowID]) {
							t.Fatal("the member could no longer read the retained history")
						}
						for field, expected := range want {
							if got := businessField(et.info, row.Value, field); got != expected {
								t.Fatalf("%s/%s = %s, want %s", rowID, field, got, expected)
							}
						}
					}
					if !bytes.Equal(sourceBytes, tn.releases.raw(from)) {
						t.Fatal("migration changed sealed source bytes")
					}
					entries, err = journal.Entries(context.Background(), id, 0)
					if err != nil {
						t.Fatal(err)
					}
					recovered := compose()
					if err := recovered.Replay(entries); err != nil {
						t.Fatal(err)
					}
					for _, rowID := range []string{"A", "B", "C"} {
						before, _ := records.FullImage("build.sample", rowID, tn.records.types["build.sample"].rows.Get(rowID))
						after, _ := records.FullImage("build.sample", rowID, recovered.records.types["build.sample"].rows.Get(rowID))
						if !bytes.Equal(jsonOf(before), jsonOf(after)) {
							t.Fatal("recovery changed saved values or history")
						}
					}
				})
			}
		})
	}
}
