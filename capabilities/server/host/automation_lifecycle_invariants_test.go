package host

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	automateowner "platformserver/services/automate"
	"platformserver/services/automate/runtime"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK1K2K4AutomationNotificationsAndOwner(t *testing.T) {
	seat := platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: build.Builder}}}
	live := newInvariantTenantWithApps(t, "automation-lifecycle", []platform.App{automation.New("automation-lifecycle")}, seat)
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alarmitem", "Alarm item"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	trigger := platform.Expr{Kind: "boolean", Op: "=", Args: []platform.Expr{platform.ExprReference("json", "subject.note"), platform.ExprLiteralValue("json", "match")}}
	live.accept(t, "build.process.create", build.ProcessType, "PROCESS", map[string]any{
		"name": "notifyitem", "title": "Notify matching item", "kind": "automation", "object": "build.alarmitem", "trigger": trigger,
		"steps": []build.ProcessStep{{Name: "notify", Kind: "notify", Notify: &automateowner.NotificationEffect{Title: "Matched", Body: "Review the item", Role: build.Builder}}},
	})
	live.accept(t, build.SchemaProcess, build.ProcessType, "PROCESS", map[string]any{})
	live.accept(t, "build.alarmitem.create", "build.alarmitem", "ITEM", map[string]any{"note": "other"})
	live.Work(invariantClock)
	if got := live.records.Type(platform.FlowInstanceType).Rows().Len(); got != 0 {
		t.Fatalf("nonmatching record started %d runs", got)
	}
	live.accept(t, "build.alarmitem.edit", "build.alarmitem", "ITEM", map[string]any{"note": "match"})
	live.Work(invariantClock.Add(time.Second))
	first, ok := platform.Get[platform.FlowInstance](live.automation(platform.FlowApp, false), "build.notifyitem:ITEM")
	if !ok || first.State != "done" || first.OnBehalf != "builder" || first.Subject != "build.alarmitem/ITEM" {
		t.Fatalf("matching trigger: %+v, found=%v", first, ok)
	}
	if notices := live.notices.ForMember("builder"); len(notices) != 1 || notices[0].Ref != "build.alarmitem/ITEM" {
		t.Fatalf("notification: %+v", notices)
	}
	request := live.submission(build.SchemaProcessRun, build.ProcessType, "PROCESS", map[string]any{"key": "explicit", "subject": "ITEM"})
	admin, _ := live.Member("admin")
	request.PrincipalId = admin.ID
	firstResult, refusal := live.Submit(admin, request, invariantClock.Add(2*time.Second))
	if refusal != nil {
		t.Fatal(refusal)
	}
	secondResult, refusal := live.Submit(admin, request, invariantClock.Add(2*time.Second))
	if refusal != nil || firstResult.GetChangeId() != secondResult.GetChangeId() {
		t.Fatalf("manual run idempotency: %v", refusal)
	}
	live.Work(invariantClock.Add(3 * time.Second))
	manual, ok := platform.Get[platform.FlowInstance](live.automation(platform.FlowApp, false), "build.notifyitem:manual:explicit")
	if !ok || manual.State != "done" || manual.OnBehalf != "builder" || manual.Subject != "build.alarmitem/ITEM" {
		t.Fatalf("run now did not retain publisher and subject: %+v found=%v", manual, ok)
	}
	if notices := live.notices.ForMember("builder"); len(notices) != 2 {
		t.Fatalf("manual run produced %d notices; want 2 total", len(notices))
	}
	before, refusal := live.RecordOf(t.Context(), live.member(t), build.ProcessType, "PROCESS", invariantClock)
	if refusal != nil {
		t.Fatal(refusal)
	}
	published := before.Record.(build.Process).Published
	revoke := live.submission(controlowner.SchemaRevoke, controlowner.MemberType, "builder", map[string]any{"app": build.ID, "role": build.Builder})
	revoke.PrincipalId, revoke.Authority = admin.ID, "platform"
	if _, refusal := live.Submit(admin, revoke, invariantClock.Add(4*time.Second)); refusal != nil {
		t.Fatal(refusal)
	}
	live.Work(invariantClock.Add(5 * time.Second))
	paused, ok := platform.Get[build.Process](live.automation(build.ID, false), "PROCESS")
	if !ok || !paused.Paused || paused.PauseReason == "" || paused.Published != published {
		t.Fatalf("owner revocation did not pause without changing publication: %+v", paused)
	}
	pausedRun := live.submission(build.SchemaProcessRun, build.ProcessType, "PROCESS", map[string]any{"key": "paused", "subject": "ITEM"})
	pausedRun.PrincipalId = admin.ID
	if _, refusal := live.Submit(admin, pausedRun, invariantClock.Add(6*time.Second)); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("paused automation accepted run now: %v", refusal)
	}
	resume := live.submission(build.SchemaProcessResume, build.ProcessType, "PROCESS", map[string]any{})
	resume.PrincipalId = admin.ID
	if _, refusal := live.Submit(admin, resume, invariantClock.Add(6*time.Second)); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("resume without restored owner permission: %v", refusal)
	}
	grant := live.submission(controlowner.SchemaGrant, controlowner.MemberType, "builder", map[string]any{"app": build.ID, "role": build.Builder})
	grant.PrincipalId, grant.Authority = admin.ID, "platform"
	if _, refusal := live.Submit(admin, grant, invariantClock.Add(7*time.Second)); refusal != nil {
		t.Fatal(refusal)
	}
	resume.IdempotencyKey += "-restored"
	if _, refusal := live.Submit(admin, resume, invariantClock.Add(8*time.Second)); refusal != nil {
		t.Fatal(refusal)
	}
	resumed, _ := platform.Get[build.Process](live.automation(build.ID, false), "PROCESS")
	if resumed.Paused || resumed.PauseReason != "" || resumed.Published != published {
		t.Fatal("resume changed publication or retained pause reason")
	}
	live.accept(t, "build.process.create", build.ProcessType, "SCHEDULE", map[string]any{"name": "schedulenotify", "title": "Scheduled notification", "kind": "automation", "every": "1h", "inputSchema": platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}, "steps": []build.ProcessStep{{Name: "notify", Kind: "notify", Notify: &automateowner.NotificationEffect{Title: "Scheduled", Body: "Scheduled notice", Role: build.Builder}}}})
	live.accept(t, build.SchemaProcess, build.ProcessType, "SCHEDULE", map[string]any{})
	live.accept(t, build.SchemaProcessRun, build.ProcessType, "SCHEDULE", map[string]any{"key": "schedule"})
	live.Work(invariantClock.Add(9 * time.Second))
	scheduled, ok := platform.Get[platform.FlowInstance](live.automation(platform.FlowApp, false), "build.schedulenotify:manual:schedule")
	if !ok || scheduled.State != "done" || scheduled.OnBehalf != "builder" {
		t.Fatalf("scheduled run now: %+v found=%v", scheduled, ok)
	}
	want := live.recordImage(t)
	for range 2 {
		restored := newInvariantTenantReplayWithApps(t, live.ID, []platform.App{automation.New(live.ID)}, seat)
		if err := restored.recoverEntries(live.entries); err != nil {
			t.Fatal(err)
		}
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("automation replay differs in %v", differing)
		}
		if !bytes.Equal(platform.Raw(live.notices.ForMember("builder")), platform.Raw(restored.notices.ForMember("builder"))) {
			t.Fatal("replay duplicated or changed notifications")
		}
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(published), &fields)
	if _, ok := fields["pauseReason"]; ok {
		t.Fatal("publication contains runtime pause reason")
	}
}
