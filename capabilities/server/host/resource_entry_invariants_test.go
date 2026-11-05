package host

import (
	"testing"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func TestInvariantResourceEntryRoutesAndReleaseKinds(t *testing.T) {
	metadata, err := build.ResourceMetadata()
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[string]build.ResourceMetadataEntry{}
	for _, entry := range metadata {
		byKind[entry.Kind] = entry
	}
	for kind, want := range map[string]struct {
		typ, view, route, projectView, releaseKind, assetKind string
	}{
		"page":           {typ: "build.page", view: "module", route: "page", releaseKind: "page", assetKind: "page"},
		"query":          {typ: "build.query", view: "object-set", route: "default", releaseKind: "query", assetKind: "query"},
		"test-plan":      {typ: "build.testplan", view: "test-plan", route: "release-plan", projectView: "project", releaseKind: "test-plan", assetKind: "test-plan"},
		"evaluation-set": {typ: "build.testplan", view: "evaluation-set", route: "default", releaseKind: "evaluation-set", assetKind: "evaluation-set"},
	} {
		got, ok := byKind[kind]
		if !ok || got.Type != want.typ || got.View != want.view || string(got.Route) != want.route || got.ProjectView != want.projectView || got.ReleaseKind != want.releaseKind || got.AssetKind != want.assetKind {
			t.Fatalf("%s route: %+v; want %+v", kind, got, want)
		}
	}

	live := newInvariantTenant(t, "invariant-resource-entries")
	member := live.member(t)
	objectType := "build.entryobject"
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("entryobject", "Entry object"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, "build.function.create", build.FunctionType, "FUNCTION1", map[string]any{
		"name": "entryanswer", "title": "Entry answer", "description": "A pinned function for the entry acceptance",
		"object": objectType, "fields": []string{"note"}, "instructions": "Answer about the note.",
		"output": []platform.Field{{Name: "answer", Type: "string", Description: "Answer"}},
		"roles":  []string{"builder"}, "maxInputBytes": 1024, "maxOutputBytes": 1024, "maxTokens": 100,
	})
	live.accept(t, "build.function.publish", build.FunctionType, "FUNCTION1", map[string]any{})
	live.accept(t, "build.testplan.create", build.TestPlanType, "PLAN1", map[string]any{
		"variant": "test-plan", "name": "entryplan", "title": "Entry test plan",
		"roots": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: objectType}},
		"at":    invariantClock,
		"steps": []map[string]any{{"type": objectType, "id": "SET1", "action": objectType + ".create", "payload": "{}", "expect": "accepted"}},
	})
	live.accept(t, "build.testplan.create", build.TestPlanType, "EVAL1", map[string]any{
		"variant": "evaluation-set", "name": "entryevaluation", "title": "Entry evaluation set",
		"function": "FUNCTION1", "functionVersion": 1, "at": invariantClock,
		"steps": []map[string]any{{"type": "build.function-call", "id": "evaluation", "action": "build.function-call.start", "payload": "{}", "expect": "accepted"}},
		"evaluation": []map[string]any{{"minQuality": 1, "maxCostUsd": 1, "maxLatencyMillis": 1000,
			"cases": []map[string]any{{"name": "case", "input": map[string]any{"note": "keep"}, "expected": map[string]any{"answer": "ok"}}}}},
	})

	directory := live.resourceDirectory(member)
	for draft, want := range map[string]struct {
		refKind platform.AssetKind
		variant string
	}{
		"PLAN1": {refKind: platform.AssetTestPlan, variant: "test-plan"},
		"EVAL1": {refKind: platform.AssetEvaluation, variant: "evaluation-set"},
	} {
		found := false
		for _, entry := range directory.Resources {
			if entry.Draft == draft {
				found = true
				if entry.Type != build.TestPlanType || entry.Ref.Kind != want.refKind || entry.Variant != want.variant {
					t.Fatalf("directory entry %s: type=%s ref=%s variant=%s; want %s/%s", draft, entry.Type, entry.Ref.Kind, entry.Variant, build.TestPlanType, want.refKind)
				}
			}
		}
		if !found {
			t.Fatalf("directory has no test plan entry %s", draft)
		}
	}
	for draft, want := range map[string]platform.AssetKind{
		"PLAN1": platform.AssetTestPlan,
		"EVAL1": platform.AssetEvaluation,
	} {
		preview, err := live.PreviewReleaseDrafts(member, []build.JointDraftRef{{Kind: want, ID: draft}}, "", invariantClock)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview %s as %s: %+v: %v", draft, want, preview, err)
		}
		ref := platform.AssetRef{App: build.ID, Kind: want, Name: map[string]string{"PLAN1": "entryplan", "EVAL1": "entryevaluation"}[draft]}
		found := false
		for _, included := range preview.Included {
			if included == ref {
				found = true
			}
		}
		if !found {
			t.Fatalf("preview %s as %s omitted %s", draft, want, ref)
		}
	}
}
