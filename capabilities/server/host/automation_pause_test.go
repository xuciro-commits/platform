package host

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/automate/runtime"
	"platformserver/services/resources/authoring"
)

func TestInvariantAutomationPauseGatesNewTriggersNotRunning(t *testing.T) {
	t.Run("scheduled automation and release", func(t *testing.T) {
		live := newInvariantTenantWithApps(t, "invariant-automation-pause", []platform.App{automation.New("invariant-automation-pause")})
		live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
		live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
		live.accept(t, "build.process.create", build.ProcessType, "PROCESS", map[string]any{
			"name": "scheduled", "title": "Scheduled", "kind": "automation", "every": "1m",
			"steps": []build.ProcessStep{{Name: "create", Kind: "action", Act: "build.alpha.create",
				Target: &platform.Binding{Source: "input", Path: []string{"period"}},
				Inputs: map[string]platform.Binding{"note": {Source: "literal", Value: json.RawMessage(`"scheduled"`)}},
			}},
		})
		live.accept(t, build.SchemaProcess, build.ProcessType, "PROCESS", map[string]any{})
		checkInstances := func(now time.Time, want int) {
			t.Helper()
			live.Work(now)
			if got := live.records.Type(platform.FlowInstanceType).Rows().Len(); got != want {
				t.Fatalf("scheduled instances at %s: got %d; want %d", now, got, want)
			}
			key := now.UTC().Truncate(time.Minute).Format(time.RFC3339)
			instance, ok := platform.Get[platform.FlowInstance](live.automation(platform.FlowApp, false), "build.scheduled:"+key)
			if !ok || instance.State != "done" {
				t.Fatalf("scheduled instance %s: %+v found=%v; want done", key, instance, ok)
			}
			view, refusal := live.RecordOf(live.member(t), "build.alpha", key, now)
			if refusal != nil || view.Record == nil {
				t.Fatalf("scheduled effect %s was not accepted: record=%v refusal=%v", key, view.Record, refusal)
			}
		}
		checkInstances(invariantClock, 1)
		live.accept(t, build.SchemaProcessPause, build.ProcessType, "PROCESS", map[string]any{})
		live.accept(t, build.SchemaProcess, build.ProcessType, "PROCESS", map[string]any{})
		view, refusal := live.RecordOf(live.member(t), build.ProcessType, "PROCESS", invariantClock)
		if refusal != nil {
			t.Fatal(refusal)
		}
		process := view.Record.(build.Process)
		if !process.Paused || process.Version != 2 {
			t.Fatalf("publishing the paused process changed its runtime state: %+v", process)
		}
		for _, publication := range process.Versions {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(publication), &fields); err != nil {
				t.Fatal(err)
			}
			if _, present := fields["paused"]; present {
				t.Fatal("a retained process publication contains paused")
			}
		}
		ref := platform.AssetRef{App: build.ID, Kind: platform.AssetFlow, Name: "build.scheduled"}
		candidate, err := live.ReleaseCandidate([]platform.AssetRef{ref})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, asset := range candidate.Assets {
			if asset.Ref != ref {
				continue
			}
			found = true
			var body platform.FlowReleaseDescriptor
			if err := json.Unmarshal(asset.Body, &body); err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(body.Definition, &fields); err != nil {
				t.Fatal(err)
			}
			if _, present := fields["paused"]; present {
				t.Fatal("the exported process asset contains paused")
			}
		}
		if !found {
			t.Fatal("the release candidate omits the paused process")
		}
		releaseOwner, ok := live.app(build.ID).(platform.AssetLifecycleOwner)
		if !ok {
			t.Fatal("the construction owner cannot prepare releases")
		}
		plans, err := releaseOwner.PrepareReleaseAssets(candidate.Assets, platform.AssetReleaseContext{Member: live.member(t), At: invariantClock})
		if err != nil {
			t.Fatal(err)
		}
		for _, plan := range plans {
			if plan.Ref != ref {
				continue
			}
			if plan.Record != build.ProcessType+"/PROCESS" {
				t.Fatalf("the process installation changed its accepted record identity: %s", plan.Record)
			}
		}
		plan, err := releaseOwner.DecodeAssetInstallation(build.SchemaProcess, platform.Raw(process))
		if err != nil {
			t.Fatal(err)
		}
		if plan.Record != build.ProcessType+"/PROCESS" || plan.Ref != ref {
			t.Fatalf("the process installation changed its accepted identity: %+v", plan)
		}
		pausedAt := invariantClock.Add(time.Minute)
		live.Work(pausedAt)
		if got := live.records.Type(platform.FlowInstanceType).Rows().Len(); got != 1 {
			t.Fatalf("the paused schedule created a new instance: got %d; want 1", got)
		}
		key := pausedAt.Format(time.RFC3339)
		if _, refusal := live.RecordOf(live.member(t), "build.alpha", key, pausedAt); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			t.Fatalf("the paused schedule produced its effect: %v", refusal)
		}
		live.accept(t, build.SchemaProcessResume, build.ProcessType, "PROCESS", map[string]any{})
		checkInstances(invariantClock.Add(2*time.Minute), 2)
	})
	t.Run("manual start and existing wait", func(t *testing.T) {
		live := newInvariantTenantWithApps(t, "invariant-flow-pause", []platform.App{automation.New("invariant-flow-pause")})
		live.accept(t, "build.process.create", build.ProcessType, "PROCESS", map[string]any{
			"name": "waiting", "title": "Waiting", "kind": "flow", "manual": true,
			"steps": []build.ProcessStep{{Name: "wait", Kind: "wait", UntilSeconds: 120, Next: "end"}, {Name: "end", Kind: "end"}},
		})
		live.accept(t, build.SchemaProcess, build.ProcessType, "PROCESS", map[string]any{})
		live.accept(t, build.SchemaProcessRun, build.ProcessType, "PROCESS", map[string]any{"key": "before"})
		live.Work(invariantClock)
		caller := live.automation(platform.FlowApp, false)
		before, ok := platform.Get[platform.FlowInstance](caller, "build.waiting:before")
		if !ok || before.State != "waiting" || len(before.Tokens) != 1 || before.Tokens[0].Waits != "wait" {
			t.Fatalf("the manual run did not enter its wait: %+v found=%v", before, ok)
		}
		live.accept(t, build.SchemaProcessPause, build.ProcessType, "PROCESS", map[string]any{})
		request := live.submission(build.SchemaProcessRun, build.ProcessType, "PROCESS", map[string]any{"key": "after"})
		if record, refusal := live.Submit(live.member(t), request, invariantClock); record != nil || refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatalf("the paused manual start: record=%v refusal=%v; want CONFLICT", record, refusal)
		}
		if got := live.records.Type(platform.FlowInstanceType).Rows().Len(); got != 1 {
			t.Fatalf("the refused manual start created an instance: got %d; want 1", got)
		}
		live.Work(invariantClock.Add(121 * time.Second))
		completed, ok := platform.Get[platform.FlowInstance](caller, before.ID)
		if !ok || completed.State != "done" || completed.Revision <= before.Revision {
			t.Fatalf("pausing prevented the existing wait from completing: %+v found=%v", completed, ok)
		}
		view, refusal := live.RecordOf(live.member(t), build.ProcessType, "PROCESS", invariantClock)
		if refusal != nil || !view.Record.(build.Process).Paused {
			t.Fatalf("the process was not paused while its existing run completed: record=%v refusal=%v", view.Record, refusal)
		}
		live.accept(t, build.SchemaProcessResume, build.ProcessType, "PROCESS", map[string]any{})
		live.accept(t, build.SchemaProcessRun, build.ProcessType, "PROCESS", map[string]any{"key": "after"})
		resumed, ok := platform.Get[platform.FlowInstance](caller, "build.waiting:after")
		if !ok || resumed.State != "waiting" || live.records.Type(platform.FlowInstanceType).Rows().Len() != 2 {
			t.Fatalf("resuming did not allow a new manual run: %+v found=%v", resumed, ok)
		}
	})
}
