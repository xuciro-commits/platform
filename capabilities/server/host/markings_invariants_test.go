package host

import (
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK1K4K5CompartmentClearancesAndDeclassification(t *testing.T) {
	seats := []platform.Seat{
		platform.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", "build": "builder"}}},
		platform.Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{"build": "builder"}}},
		platform.Seat{Subjects: []string{"partial"}, Member: platform.Member{ID: "partial", Roles: map[string]string{"build": "builder"}}},
	}
	live := newInvariantTenant(t, "compartment-invariants", seats...)
	admin, _ := live.Member("admin")
	save := func(schema, typ, id string, payload any) {
		t.Helper()
		sub := live.submission(schema, typ, id, payload)
		sub.Authority, sub.PrincipalId = "platform", admin.ID
		if _, refusal := live.Submit(admin, sub, invariantClock); refusal != nil {
			t.Fatalf("%s: %v", schema, refusal)
		}
	}
	save(platform.SchemaTeamSave, platform.TeamType, "finance-team", map[string]any{"name": "Finance team", "members": []string{"reader", "builder"}, "grants": []platform.Grant{}})
	finance := func(declassifiers []string) {
		save(platform.SchemaMarkingSave, platform.MarkingType, "finance", map[string]any{"title": "Finance", "teams": []string{"finance-team"}, "declassifiers": declassifiers})
	}
	finance([]string{"builder"})
	save(platform.SchemaMarkingSave, platform.MarkingType, "pii", map[string]any{"title": "Personal data", "members": []string{"reader", "builder", "partial"}, "declassifiers": []string{"builder"}})
	save(platform.SchemaMarkingSave, platform.MarkingType, "confidential", map[string]any{"title": "Confidential", "members": []string{"reader", "builder", "partial"}, "declassifiers": []string{"builder"}})
	live.accept(t, "build.dataset.create", build.DatasetType, "INPUT", map[string]any{"name": "classified", "title": "Classified", "marking": "confidential", "compartments": []string{"pii", "finance"}, "keep": 10})
	live.accept(t, build.SchemaDatasetLoad, build.DatasetType, "INPUT", map[string]any{"rows": []map[string]any{{"id": "A", "note": "protected"}}})
	for _, id := range []string{"reader", "partial", "admin"} {
		member, _ := live.Member(id)
		page, refusal := live.Records(t.Context(), member, build.DatasetVersionType, platform.Query{}, invariantClock)
		if refusal != nil {
			t.Fatal(refusal)
		}
		want := 0
		if id == "reader" {
			want = 1
		}
		if len(page.Records) != want {
			t.Fatalf("%s sees %d classified versions, want %d", id, len(page.Records), want)
		}
	}
	if _, refusal := live.Submit(live.member(t), live.submission("build.dataset.edit", build.DatasetType, "INPUT", map[string]any{"compartments": []string{}}), invariantClock); refusal == nil {
		t.Fatal("editing removed mandatory markings")
	}
	live.accept(t, "build.dataset.create", build.DatasetType, "OUTPUT", map[string]any{"name": "declassified", "title": "Declassified", "keep": 10})
	live.accept(t, "build.pipeline.create", build.PipelineType, "PIPE", map[string]any{"name": "releasepublic", "title": "Release public data", "input": "INPUT", "outputDataset": "OUTPUT", "steps": []platform.TransformStep{{Kind: "declassify", Markings: []string{"confidential", "pii", "finance"}}}})
	drafts := []build.JointDraftRef{{Kind: platform.AssetDataset, ID: "INPUT"}, {Kind: platform.AssetDataset, ID: "OUTPUT"}, {Kind: platform.AssetPipeline, ID: "PIPE"}}
	preview, err := live.PreviewReleaseDrafts(admin, drafts, "", invariantClock)
	if err != nil || !strings.Contains(preview.Diagnostic, "declassification") {
		t.Fatalf("joint publication bypassed declassification permission: %+v %v", preview, err)
	}
	live.accept(t, "build.pipeline.publish", build.PipelineType, "PIPE", map[string]any{})
	live.accept(t, "build.pipeline.run", build.PipelineType, "PIPE", map[string]any{})
	finance(nil)
	live.RunPipelines(invariantClock.Add(time.Second))
	output, _ := platform.Get[build.Dataset](live.automation(build.ID, false), "OUTPUT")
	pipeline, _ := platform.Get[build.Pipeline](live.automation(build.ID, false), "PIPE")
	if output.Version != 0 || pipeline.Last == nil || pipeline.Last.Error == "" {
		t.Fatalf("unpermitted declassification wrote output: %+v %+v", output, pipeline.Last)
	}
	finance([]string{"builder"})
	live.accept(t, "build.pipeline.run", build.PipelineType, "PIPE", map[string]any{})
	live.RunPipelines(invariantClock.Add(2 * time.Second))
	output, _ = platform.Get[build.Dataset](live.automation(build.ID, false), "OUTPUT")
	pipeline, _ = platform.Get[build.Pipeline](live.automation(build.ID, false), "PIPE")
	if output.Version != 1 || len(output.Compartments) != 0 || pipeline.Last.Error != "" || !slices.Contains(pipeline.Last.Declassified, "finance") {
		t.Fatalf("authorized declassification: %+v %+v", output, pipeline.Last)
	}
	forged := live.submission(build.SchemaPipelineRan, build.PipelineType, "PIPE", map[string]any{"run": *pipeline.Last})
	if _, refusal := live.Submit(live.member(t), forged, invariantClock); refusal == nil {
		t.Fatal("a caller forged a pipeline execution result")
	}
	partial, _ := live.Member("partial")
	visible, refusal := live.Records(t.Context(), partial, build.DatasetVersionType, platform.Query{}, invariantClock)
	if refusal != nil || len(visible.Records) != 1 || visible.Records[0].(build.DatasetVersion).Dataset != "OUTPUT" {
		t.Fatalf("declassified output not readable: %+v %v", visible, refusal)
	}
	for _, replay := range []int{1, 2} {
		restored := live.restore(t, live.entries, seats...)
		reader, _ := restored.Member("reader")
		if !slices.Contains(reader.Markings, "finance") || !slices.Contains(reader.Markings, "pii") {
			t.Fatalf("replay %d lost team or direct clearances", replay)
		}
		if differing := differingTypes(live.recordImage(t), restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("replay %d differs in %v", replay, differing)
		}
	}
}
