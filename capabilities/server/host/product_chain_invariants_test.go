package host

import (
	"encoding/json"
	"slices"
	"testing"

	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

func TestInvariantProductVersionFreezesSavedCandidate(t *testing.T) {
	live := newInvariantTenant(t, "invariant-product-chain")
	object := invariantObject("alpha", "Alpha")
	live.accept(t, build.ObjectType+".create", build.ObjectType, "OBJ1", object)
	live.accept(t, build.AppType+".create", build.AppType, "APP1", map[string]any{
		"name": "alphaapp", "title": "Alpha application",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"}},
	})
	product := map[string]any{
		"name": "alpha", "title": "Alpha product", "description": "Delivers the Alpha application.",
		"project": "APP1", "roots": []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "alphaapp"}},
	}
	live.accept(t, build.ProductType+".create", build.ProductType, "alpha", product)

	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "OBJ1"}, {Kind: platform.AssetApp, ID: "APP1"}}
	preview, err := live.PreviewReleaseDrafts(live.member(t), drafts, "", invariantClock)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("candidate preview: %+v: %v", preview, err)
	}
	candidate, err := live.SaveReleaseCandidates(live.member(t), drafts, preview.CandidateID, "product-chain-candidate", invariantClock)
	if err != nil {
		t.Fatalf("save candidate: %v", err)
	}
	live.accept(t, build.SchemaProductVersion, build.ProductVersionType, "alpha@1.0.0", map[string]any{
		"product": "alpha", "version": "1.0.0", "candidate": candidate,
	})

	view, refusal := live.RecordOf(t.Context(), live.member(t), build.ProductVersionType, "alpha@1.0.0", invariantClock)
	if refusal != nil {
		t.Fatalf("read product version: %v", refusal)
	}
	version, ok := view.Record.(build.ProductVersion)
	if !ok || version.Product != "alpha" || version.Version != "1.0.0" || version.Candidate != candidate {
		t.Fatalf("product version: %#v", view.Record)
	}
	if version.Descriptor.Blueprint == nil || !slices.ContainsFunc(version.Descriptor.Blueprint.Assets, func(asset platform.ReleaseAsset) bool {
		return asset.Ref == (platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.alpha"})
	}) {
		t.Fatalf("product descriptor lacks the candidate closure: %+v", version.Descriptor)
	}
	before, err := json.Marshal(version)
	if err != nil {
		t.Fatal(err)
	}

	product["title"] = "Alpha product, revised"
	live.accept(t, build.ProductType+".edit", build.ProductType, "alpha", product)
	again, refusal := live.RecordOf(t.Context(), live.member(t), build.ProductVersionType, "alpha@1.0.0", invariantClock)
	if refusal != nil {
		t.Fatalf("read product version after product edit: %v", refusal)
	}
	after, _ := json.Marshal(again.Record)
	if string(before) != string(after) {
		t.Fatal("editing a product changed an already frozen version")
	}
	if _, refusal := live.Submit(live.member(t), live.submission(build.SchemaProductVersion, build.ProductVersionType, "alpha@1.0.0", map[string]any{
		"product": "alpha", "version": "1.0.0", "candidate": candidate,
	}), invariantClock); refusal == nil {
		t.Fatal("a published product version was replaced")
	}
	restored := live.restore(t, live.entries)
	replayed, refusal := restored.RecordOf(t.Context(), restored.member(t), build.ProductVersionType, "alpha@1.0.0", invariantClock)
	if refusal != nil {
		t.Fatalf("read replayed product version: %v", refusal)
	}
	replayedBytes, _ := json.Marshal(replayed.Record)
	if string(before) != string(replayedBytes) {
		t.Fatal("replay changed the frozen product version")
	}

	catalog := live.App("platform").(interface{ PackageViews() []platform.PackageView }).PackageViews()
	var found *platform.PackageView
	for i := range catalog {
		if catalog[i].Descriptor.ID == "alpha" && catalog[i].Descriptor.Version == "1.0.0" {
			found = &catalog[i]
			break
		}
	}
	if found == nil || found.Source != "product" || found.Product != "alpha" || found.Candidate != candidate {
		t.Fatalf("Marketplace did not consume the product version: %+v", found)
	}
}
