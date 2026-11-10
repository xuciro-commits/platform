package host

import (
	"encoding/json"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
	"testing"
	"time"
)

func TestInvariantK1K2ScheduleMigrationHasOneTimer(t *testing.T) {
	live := newInvariantTenant(t, "schedule-invariants")
	for _, id := range []string{"INPUT", "OUTPUT"} {
		live.accept(t, "build.dataset.create", dataowner.DatasetType, id, map[string]any{"name": id, "title": id})
	}
	live.accept(t, dataowner.SchemaDatasetLoad, dataowner.DatasetType, "INPUT", map[string]any{"rows": []map[string]any{{"id": "A"}}})
	live.accept(t, "build.pipeline.create", dataowner.PipelineType, "PIPE", map[string]any{"name": "scheduled", "title": "Scheduled", "input": "INPUT", "outputDataset": "OUTPUT"})
	live.accept(t, "build.pipeline.publish", dataowner.PipelineType, "PIPE", map[string]any{})
	live.accept(t, "build.pipeline.run", dataowner.PipelineType, "PIPE", map[string]any{})
	legacy, _ := platform.Get[dataowner.Pipeline](live.automation(build.ID, false), "PIPE")
	var asset platform.ReleaseAsset
	if err := json.Unmarshal([]byte(legacy.Published), &asset); err != nil {
		t.Fatal(err)
	}
	var definition dataowner.PipelineDefinition
	if err := json.Unmarshal(asset.Body, &definition); err != nil {
		t.Fatal(err)
	}
	definition.Every = "1m"
	asset.Body = platform.Raw(definition)
	asset.SourceVersion = "1.pipeline-2"
	legacy.Every = "1m"
	legacy.DefinitionVersion = 2
	legacy.Published = string(platform.Raw(asset))
	legacy.Versions = append(legacy.Versions, legacy.Published)
	original, err := live.ReleaseCandidate([]platform.AssetRef{asset.Ref})
	if err != nil {
		t.Fatal(err)
	}
	for i, existing := range original.Assets {
		if existing.Ref == asset.Ref {
			original.Assets[i] = asset
		}
	}
	candidate, err := platform.Candidate([]platform.AssetRef{asset.Ref}, original.Assets)
	if err != nil {
		t.Fatal(err)
	}
	before, err := live.installationPredecessor(dataowner.PipelineType + "/PIPE")
	if err != nil {
		t.Fatal(err)
	}
	saved := acceptedformat.Release{Version: 4, Kind: "release-result", Tenant: live.ID, App: build.ID, Member: "builder", Key: "legacy-schedule-fixture", At: invariantClock, CandidateID: candidate.ID, Bytes: candidate.Bytes, Active: true, Installations: []platform.ReleaseInstallation{{ReleasePublication: platform.ReleasePublication{Schema: dataowner.PipelineType + ".publish", Image: platform.Raw(legacy)}, Ref: &asset.Ref, Record: dataowner.PipelineType + "/PIPE", Before: before}}}
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(live.ID, "builder", saved.Key, candidate.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	live.mu.Lock()
	_, err = live.commitReleaseLocked(live.member(t), saved)
	live.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	live.RunPipelines(invariantClock)
	schedule, ok := platform.Get[platform.Schedule](live.automation(build.ID, false), "pipeline-PIPE")
	if !ok || schedule.Runner != "builder" || schedule.LastRun == nil {
		t.Fatalf("legacy schedule not accepted: %+v", schedule)
	}
	output, _ := platform.Get[dataowner.Dataset](live.automation(build.ID, false), "OUTPUT")
	if output.Version != 1 {
		p, _ := platform.Get[dataowner.Pipeline](live.automation(build.ID, false), "PIPE")
		t.Fatalf("first scheduled build wrote %d versions; last=%+v", output.Version, p.Last)
	}
	invariantPublishData(t, live, live.member(t), []build.JointDraftRef{{Kind: platform.AssetSchedule, ID: schedule.ID}}, "scheduled-resource-release")
	live.accept(t, platform.ScheduleType+".edit", platform.ScheduleType, schedule.ID, map[string]any{"enabled": false})
	live.RunPipelines(invariantClock.Add(2 * time.Minute))
	output, _ = platform.Get[dataowner.Dataset](live.automation(build.ID, false), "OUTPUT")
	if output.Version != 1 {
		t.Fatal("legacy Pipeline.Every still executed after its schedule was disabled")
	}
	live.accept(t, platform.ScheduleType+".edit", platform.ScheduleType, schedule.ID, map[string]any{"enabled": true})
	live.RunPipelines(invariantClock.Add(3 * time.Minute))
	output, _ = platform.Get[dataowner.Dataset](live.automation(build.ID, false), "OUTPUT")
	if output.Version != 2 {
		t.Fatalf("enabled schedule versions=%d, want 2", output.Version)
	}
	live.RunPipelines(invariantClock.Add(3 * time.Minute))
	output, _ = platform.Get[dataowner.Dataset](live.automation(build.ID, false), "OUTPUT")
	if output.Version != 2 {
		t.Fatal("schedule repeated within one due period")
	}
	want := live.recordImage(t)
	for replay := 0; replay < 2; replay++ {
		restored := live.restore(t, live.entries)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("schedule replay differs in %v", differing)
		}
	}
}
