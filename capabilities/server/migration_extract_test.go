package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"platformserver/internal/records"
	"testing"
	"time"

	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestMigrationExtractsDeclaredObjectsAndResumes(t *testing.T) {
	for _, mode := range []string{"activate", "cancel", "conflict", "quarantine"} {
		cancel := mode == "cancel"
		t.Run(mode, func(t *testing.T) {
			for _, backend := range journalsUnderTest(t) {
				t.Run(backend.name, func(t *testing.T) {
					id := fmt.Sprintf("extract-%d", time.Now().UnixNano())
					defer backend.cleanup(t, id)
					journal := backend.open(t, id)
					defer func() { journal.Close() }()
					if _, err := journal.Entries(context.Background(), id, 0); err != nil {
						t.Fatal(err)
					}
					compose := func() *Tenant { return builderTenant(t, id, work.New(id), flow.New(id)) }
					tn := compose()
					fail := false
					attach := func() {
						tn.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
							if fail {
								return nil, errors.New("injected extraction append failure")
							}
							return journal.AppendAccepted(context.Background(), id, e, key, hash)
						}
					}
					attach()
					fields := []construction.Field{{Name: "value", Title: "Value", Type: "text", SourceType: "user-input"}}
					actions := []construction.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done", Conditions: []construction.Condition{{Field: "value", Operator: "=", Value: "Updated", Message: "The retained write must be readable"}}}}
					publishObject(t, tn, "builder", "S", map[string]any{"name": "sample", "title": "Sample", "fields": fields, "states": []construction.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, "actions": actions}, txnAt)
					steps := []construction.ProcessStep{{Name: "ask", Title: "Confirm", Kind: "ask", Ask: construction.User, Answers: []string{"yes"}, Next: "edit"}, {Name: "edit", Kind: "action", Act: "edit", Inputs: map[string]platform.Binding{"value": {Source: "literal", Value: platform.Raw("Updated")}}, Next: "close"}, {Name: "close", Kind: "action", Act: "close"}}
					decide(t, tn, "builder", construction.ID, "build.process.create", construction.ProcessType, "P", map[string]any{"name": "review", "title": "Review", "object": "build.sample", "when": "open", "steps": steps}, txnAt)
					decide(t, tn, "builder", construction.ID, construction.SchemaProcess, construction.ProcessType, "P", map[string]any{}, txnAt)

					save := func(key string, drafts []construction.JointDraftRef) string {
						t.Helper()
						member := memberOf(t, tn, "builder")
						preview, err := tn.PreviewReleaseDrafts(member, drafts)
						if err != nil || preview.Diagnostic != "" {
							t.Fatalf("preview %+v %v", preview, err)
						}
						candidate, err := tn.SaveReleaseCandidates(member, drafts, preview.CandidateID, key, txnAt)
						if err != nil {
							t.Fatal(err)
						}
						return candidate
					}
					from := save("source", []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "S"}, {Kind: platform.AssetFlow, ID: "P"}})
					if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), from, "source-activate", txnAt); err != nil {
						t.Fatal(err)
					}
					for _, rowID := range []string{"A", "B", "C"} {
						decide(t, tn, "user", construction.ID, "build.sample.create", "build.sample", rowID, map[string]any{"value": "Name " + rowID}, txnAt)
					}
					tn.Work(txnAt.Add(2 * time.Second))
					decide(t, tn, "builder", construction.ID, "build.object.create", construction.ObjectType, "T", map[string]any{"name": "detail", "title": "Detail", "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text", Required: true}}}, txnAt)
					fields[0].ReadOnly = true
					fields = append(fields, construction.Field{Name: "mark", Title: "Mark", Type: "text"})
					actions[0].Conditions[0].Field = "mark"
					steps[1].Inputs = map[string]platform.Binding{"mark": {Source: "literal", Value: platform.Raw("Updated")}}
					decide(t, tn, "builder", construction.ID, "build.process.edit", construction.ProcessType, "P", map[string]any{"steps": steps}, txnAt)

					decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "S", map[string]any{"fields": fields, "actions": actions}, txnAt)
					to := save("target", []construction.JointDraftRef{{Kind: platform.AssetObject, ID: "S"}, {Kind: platform.AssetObject, ID: "T"}, {Kind: platform.AssetFlow, ID: "P"}})
					identity, value := platform.ExprReference("reference", "id"), platform.ExprReference("text", "value")
					plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.sample"}, Steps: []platform.MigrateStep{{Kind: "extract", Types: []string{"build.sample"}, Target: "build.detail", Identity: &identity, Batch: 2, Fields: []platform.MigrateField{{From: "value", To: "name", Expr: &value}}}}}
					if mode == "conflict" || mode == "quarantine" {
						fixed := platform.ExprLiteralValue("text", "same")
						plan.Steps[0].Identity = &fixed
					}
					if mode == "quarantine" {
						plan.Steps[0].OnFailure = "quarantine"
					}
					review, err := tn.ReviewMigrationPlan(memberOf(t, tn, "builder"), plan, txnAt)
					if err != nil {
						t.Fatal(err)
					}
					start := submission(tn, memberOf(t, tn, "builder"), construction.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At})
					before := snapshot(tn)
					fail = true
					if _, err := tn.Submit(memberOf(t, tn, "builder"), start, txnAt); err == nil || snapshot(tn) != before || tn.records.types["build.detail"] != nil {
						t.Fatal("failed start leaked a target declaration or progress")
					}
					fail = false
					if _, err := tn.Submit(memberOf(t, tn, "builder"), start, txnAt); err != nil {
						t.Fatalf("start: %#v; fault: %+v", err, tn.fault.Load())
					}
					if target := tn.records.types["build.detail"]; target == nil || target.rows.Len() != 0 {
						t.Fatal("start did not accept the sealed new object declaration")
					}
					advance := submission(tn, memberOf(t, tn, "builder"), construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{})
					before = snapshot(tn)
					fail = true
					if _, err := tn.Submit(memberOf(t, tn, "builder"), advance, txnAt); err == nil || snapshot(tn) != before {
						t.Fatal("failed extraction append leaked records or cursor")
					}
					fail = false
					first, refusal := tn.Submit(memberOf(t, tn, "builder"), advance, txnAt)
					if refusal != nil {
						t.Fatal(refusal)
					}
					if mode == "conflict" || mode == "quarantine" {
						if mode == "quarantine" {
							decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
						}
						run := migrationRun(t, tn, "M")
						expected := 0
						if mode == "quarantine" {
							expected = 1
						}
						if run.State != "blocked" || len(run.Failures) == 0 || tn.records.types["build.detail"].rows.Len() != expected {
							t.Fatalf("identity collision was not recorded without overwriting: %+v", run)
						}
						if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), to, "blocked-activate", txnAt); err == nil {
							t.Fatal("unresolved extraction identities activated")
						}
						entries, err := journal.Entries(context.Background(), id, 0)
						if err != nil {
							t.Fatal(err)
						}
						recovered := compose()
						if err := recovered.Replay(entries); err != nil {
							t.Fatal(err)
						}
						if migrationRun(t, recovered, "M").State != "blocked" || recovered.records.types["build.detail"].rows.Len() != expected {
							t.Fatal("recovery lost extraction quarantine or repeated rejected rows")
						}
						return
					}
					if run := migrationRun(t, tn, "M"); run.Processed != 2 || run.Migrated != 2 || len(run.Extractions) != 2 || run.State != "running" {
						t.Fatalf("batch: %+v", run)
					}
					if got := refuse(t, tn, "user", construction.ID, "build.detail.edit", "build.detail", "A", map[string]any{"name": "forbidden"}, txnAt); got == "ok" {
						t.Fatal("target writes were not frozen")
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
					again, refusal := tn.Submit(memberOf(t, tn, "builder"), advance, txnAt.Add(time.Hour))
					if refusal != nil || first.GetChangeId() != again.GetChangeId() || journal.Position(id) != position {
						t.Fatal("recovery repeated an extraction")
					}
					if cancel {
						for i := 0; i < 3; i++ {
							decide(t, tn, "builder", construction.ID, "build.migration.cancel", platform.MigrationRunType, "M", map[string]any{}, txnAt)
						}
						if migrationRun(t, tn, "M").State != "cancelled" {
							t.Fatal("cancellation did not finish its bounded compensation")
						}
						for _, rowID := range []string{"A", "B"} {
							if !records.Header(tn.records.types["build.detail"].rows.Get(rowID).Value).Archived {
								t.Fatal("cancellation left an extracted record active")
							}
						}
						decide(t, tn, "user", construction.ID, "build.sample.edit", "build.sample", "A", map[string]any{"value": "Source reopened"}, txnAt)
					} else {
						decide(t, tn, "builder", construction.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
						if _, err := tn.ActivateRelease(memberOf(t, tn, "builder"), to, "target-activate", txnAt); err != nil {
							t.Fatal(err)
						}
						for _, rowID := range []string{"A", "B", "C"} {
							et := tn.records.types["build.detail"]
							if businessField(et.info, et.rows.Get(rowID).Value, "name") != "Name "+rowID {
								t.Fatal("extraction lost its declared source value")
							}
						}
					}
					saved, _, err := tn.Snapshot(func() int64 { return 0 })
					if err != nil {
						t.Fatal(err)
					}
					tn = compose()
					if err := tn.Restore(saved); err != nil {
						t.Fatal(err)
					}
					attach()
					tasks, _, _ := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{})
					for _, task := range tasks {
						if task.State == "open" {
							decide(t, tn, "user", work.ID, "work.task.complete", work.TaskType, task.ID, map[string]any{"answer": "yes"}, txnAt.Add(3*time.Second))
						}
					}
					tn.Work(txnAt.Add(5 * time.Second))
					for _, rowID := range []string{"A", "B", "C"} {
						instance, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), "build.review:"+rowID)
						if instance.State != "done" || instance.Release != from {
							t.Fatalf("old flow lost its extraction binding: %s; projection: %s; source: %s", jsonOf(instance), jsonOf(tn.records.types["build.sample"].rows.Get(rowID).Projection), jsonOf(tn.records.types["build.sample"].rows.Get(rowID).Value.Interface()))
						}
						if !cancel {
							et := tn.records.types["build.detail"]
							if businessField(et.info, et.rows.Get(rowID).Value, "name") != "Updated" {
								t.Fatal("retained extraction did not write its target")
							}
						}
					}
					entries, err = journal.Entries(context.Background(), id, 0)
					if err != nil {
						t.Fatal(err)
					}
					recovered := compose()
					if err := recovered.Replay(entries); err != nil {
						t.Fatal(err)
					}
					for rowID, row := range tn.records.types["build.detail"].rows.All() {
						before, _ := records.FullImage("build.detail", rowID, row)
						after, _ := records.FullImage("build.detail", rowID, recovered.records.types["build.detail"].rows.Get(rowID))
						if !bytes.Equal(jsonOf(before), jsonOf(after)) {
							t.Fatal("recovery reran extraction or compensation")
						}
					}
				})
			}
		})
	}
}
