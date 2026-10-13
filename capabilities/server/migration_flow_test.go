package platformserver

import (
	"bytes"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestMigrationKeepsWaitingFlowOnItsStartingObjectAndAction(t *testing.T) {
	for _, retired := range []bool{false, true} {
		name := "changed-action"
		if retired {
			name = "retired-action"
		}
		t.Run(name, func(t *testing.T) { testWaitingFlowMigration(t, retired) })
	}
}

func testWaitingFlowMigration(t *testing.T, retired bool) {
	compose := func() *Tenant {
		return builderTenant(t, "migration-flow", work.New("migration-flow"), flow.New("migration-flow"))
	}
	tn := compose()
	entries := []Entry{}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := txnAt
	fields := []map[string]any{{"name": "guest", "title": "Guest", "type": "text", "sourceType": "user-input"}}
	actions := []build.Action{{Name: "close", Title: "Close", From: []string{"open"}, To: "done"}, {Name: "reject", Title: "Reject", From: []string{"open"}, To: "rejected"}}
	states := []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}, {Name: "rejected", Title: "Rejected"}}
	decide(t, tn, "builder", build.ID, "build.object.create", build.ObjectType, "O", map[string]any{"name": "visit", "title": "Visit", "fields": fields, "states": states, "actions": actions}, at)
	decide(t, tn, "builder", build.ID, build.SchemaPublish, build.ObjectType, "O", map[string]any{}, at)
	steps := []build.ProcessStep{
		{Name: "check", Title: "Check", Kind: "ask", Ask: build.User, Answers: []string{"yes"}, Next: "rename"},
		{Name: "rename", Kind: "action", Act: "edit", Inputs: map[string]platform.Binding{"guest": {Source: "literal", Value: platform.Raw("Flow updated")}}, Next: "close"},
		{Name: "close", Kind: "action", Act: "close"},
	}
	decide(t, tn, "builder", build.ID, "build.process.create", build.ProcessType, "P", map[string]any{"name": "review", "title": "Review", "object": "build.visit", "when": "open", "steps": steps}, at)
	decide(t, tn, "builder", build.ID, build.SchemaProcess, build.ProcessType, "P", map[string]any{}, at)
	builder := memberOf(t, tn, "builder")
	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "O"}, {Kind: platform.AssetFlow, ID: "P"}}
	save := func(key string) string {
		t.Helper()
		preview, err := tn.PreviewReleaseDrafts(builder, drafts)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview: %v %+v", err, preview)
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
	decide(t, tn, "user", build.ID, "build.visit.create", "build.visit", "A", map[string]any{"guest": "Ada"}, at)
	at = at.Add(2 * time.Second)
	tn.Work(at)
	started, found := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), "build.review:A")
	if !found || started.State != "waiting" || started.Release != from {
		t.Fatalf("source did not wait: %+v", started)
	}
	tasks, _, refusal := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{})
	if refusal != nil || len(tasks) != 1 {
		t.Fatalf("source task: %+v %v", tasks, refusal)
	}
	sourceBytes := bytes.Clone(tn.releases.raw(from))
	fields[0]["readOnly"], fields[0]["aliasOf"] = true, "name"
	fields = append(fields, map[string]any{"name": "name", "title": "Name", "type": "text", "sourceType": "user-input"}, map[string]any{"name": "enabled", "title": "Enabled", "type": "boolean", "required": true, "sourceType": "user-input"})
	actions[0].To = "rejected"
	if retired {
		actions = actions[1:]
		steps[2].Act = "reject"
	}
	steps[1].Inputs = map[string]platform.Binding{"name": {Source: "literal", Value: platform.Raw("Flow updated")}}
	decide(t, tn, "builder", build.ID, "build.process.edit", build.ProcessType, "P", map[string]any{"steps": steps}, at)
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "O", map[string]any{"fields": fields, "actions": actions}, at)
	to := save("target")
	literal := platform.ExprLiteralValue("boolean", true)
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.visit"}, Steps: []platform.MigrateStep{
		{Kind: "rename", Types: []string{"build.visit"}, Batch: 1, Fields: []platform.MigrateField{{From: "guest", To: "name"}}},
		{Kind: "default", Types: []string{"build.visit"}, Batch: 1, Fields: []platform.MigrateField{{To: "enabled", Expr: &literal}}},
	}}
	if retired {
		plan.Steps = append(plan.Steps, platform.MigrateStep{Kind: "retire-action", Types: []string{"build.visit"}, Action: "build.visit.close", Batch: 1})
	}
	review, err := tn.ReviewMigrationPlan(builder, plan, at)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", build.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": review.ID, "reviewedAt": review.At}, at)
	for range plan.Steps {
		decide(t, tn, "builder", build.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, at)
	}
	if _, err := tn.ActivateRelease(builder, to, "target-activate", at); err != nil {
		t.Fatal(err)
	}
	if err := tn.procs.Check(); err != nil {
		t.Fatal(err)
	}
	// Both snapshot and full accepted-log recovery must retain a waiting v1
	// even after v2 has installed a different shape and action catalog.
	saved, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	snapshotRestored := compose()
	if err := snapshotRestored.Restore(saved); err != nil {
		t.Fatal(err)
	}
	if err := snapshotRestored.procs.Check(); err != nil {
		t.Fatal(err)
	}
	waitingRestored := compose()
	if err := waitingRestored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if err := waitingRestored.procs.Check(); err != nil {
		t.Fatal(err)
	}
	tn = waitingRestored
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	builder = memberOf(t, tn, "builder")
	decide(t, tn, "user", build.ID, "build.visit.edit", "build.visit", "A", map[string]any{"name": "Updated"}, at)
	decide(t, tn, "user", work.ID, "work.task.complete", work.TaskType, tasks[0].ID, map[string]any{"answer": "yes"}, at)
	at = at.Add(2 * time.Second)
	tn.Work(at)
	info := tn.records.types["build.visit"].info
	row := tn.records.types["build.visit"].rows["A"]
	if businessField(info, row.value, "state") != "done" || businessField(info, row.value, "name") != "Flow updated" || businessField(info, row.value, "enabled") != "true" {
		t.Fatalf("old action used new semantics or lost migrated facts: %s", jsonOf(row.value.Interface()))
	}
	ended, _ := platform.Get[platform.FlowInstance](tn.automation(flow.ID, false), started.ID)
	if ended.State != "done" || ended.Release != started.Release || ended.Dependencies != started.Dependencies {
		t.Fatalf("old instance drifted: %+v", ended)
	}
	decide(t, tn, "user", build.ID, "build.visit.create", "build.visit", "B", map[string]any{"name": "Bob", "enabled": true}, at)
	at = at.Add(2 * time.Second)
	tn.Work(at)
	newTasks, _, _ := platform.Find[platform.WorkTask](tn.automation(work.ID, false), platform.Query{})
	for _, task := range newTasks {
		if task.State == "open" {
			decide(t, tn, "user", work.ID, "work.task.complete", work.TaskType, task.ID, map[string]any{"answer": "yes"}, at)
		}
	}
	at = at.Add(2 * time.Second)
	tn.Work(at)
	if businessField(info, tn.records.types["build.visit"].rows["B"].value, "state") != "rejected" {
		t.Fatal("new instance ignored its current action")
	}
	if !bytes.Equal(sourceBytes, tn.releases.raw(from)) {
		t.Fatal("execution rewrote the starting release")
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	before, _ := tn.records.types["build.visit"].rows["A"].image()
	after, _ := recovered.records.types["build.visit"].rows["A"].image()
	if !bytes.Equal(before, after) {
		t.Fatal("recovery reran an old action")
	}
}
