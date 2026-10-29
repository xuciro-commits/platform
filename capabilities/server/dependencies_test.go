package platformserver

import "platformserver/studio/build"

import (
	"bytes"
	controlowner "platformserver/kernel/control"
	"slices"
	"testing"
	"time"

	"platformserver/platform"
)

func TestDependencyDiscoveryUsesTheAuthorizedOwnerDescription(t *testing.T) {
	tn := stockTenant(t)
	console := tn.app(controlowner.PlatformApp).(*controlowner.Console)
	clerk, _ := console.MemberBySubject("ana")
	operator, _ := console.MemberBySubject("op")
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	create := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.create"}
	count := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.count"}
	view, refusal := tn.DependencyView(clerk, object, "")
	if refusal != nil {
		t.Fatal(refusal)
	}
	if !slices.Contains(view.UsedBy, create) || slices.Contains(view.UsedBy, count) {
		t.Fatalf("impact did not preserve action authorization: %+v", view.UsedBy)
	}
	authorized, refusal := tn.DependencyView(clerk, platform.AssetRef{}, "")
	if refusal != nil {
		t.Fatal(refusal)
	}
	for _, descriptor := range view.Descriptors {
		for _, dependency := range descriptor.Requires {
			if !slices.ContainsFunc(authorized.Descriptors, func(d platform.Dependencies) bool { return d.Ref == dependency }) {
				t.Fatalf("discovery exposed an unreadable dependency: %s -> %s", descriptor.Ref, dependency)
			}
		}
	}
	provided, refusal := tn.ProvidedCapabilities(clerk, "action", platform.CapabilityAuthoritative)
	if refusal != nil {
		t.Fatal(refusal)
	}
	if !slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == create }) || slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == count }) {
		t.Fatalf("clerk capability discovery widened its original catalog: %+v", provided)
	}
	provided, refusal = tn.ProvidedCapabilities(operator, "action", "")
	if refusal != nil || !slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == count }) || slices.ContainsFunc(provided, func(c ProvidedCapability) bool { return c.Ref == create }) {
		t.Fatalf("operator capability discovery: %+v %v", provided, refusal)
	}
	if _, refusal := tn.DependencyView(clerk, count, ""); refusal == nil {
		t.Fatal("hidden action graph was exposed")
	}
}

func TestDependencyDiscoveryTracksTheSameDescriptorChange(t *testing.T) {
	tn := stockTenant(t)
	member, _ := tn.app(controlowner.PlatformApp).(*controlowner.Console).MemberBySubject("ana")
	create := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.create"}
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	for i := range tn.definitions {
		if tn.definitions[i].Ref == create {
			tn.definitions[i].Requires = nil
		}
	}
	view, refusal := tn.DependencyView(member, create, "")
	if refusal != nil || slices.Contains(view.Requires, object) {
		t.Fatalf("forward view retained an independent list: %+v %v", view, refusal)
	}
	view, refusal = tn.DependencyView(member, object, "")
	if refusal != nil || slices.Contains(view.UsedBy, create) {
		t.Fatalf("reverse view retained an independent list: %+v %v", view, refusal)
	}
}

func TestFocusedDependenciesKeepCrossOwnerRelationsWithoutUnrelatedDescriptors(t *testing.T) {
	const tenant = "focused-dependencies"
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("builder", "build:builder", "stock:clerk")}, build.New(tenant), newStock(tenant))
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	unrelatedType := publishObject(t, tn, "builder", "UNRELATED", map[string]any{"name": "unrelated", "title": "Unrelated", "fields": []build.Field{{Name: "note", Type: "text", Title: "Note"}}}, at)
	decide(t, tn, "builder", build.ID, build.PageType+".create", build.PageType, "STOCK", map[string]any{"name": "stockdesk", "title": "Stock desk", "object": "stock.item", "list": []string{"name", "qty"}, "detail": []string{"name", "qty"}, "actions": []string{"stock.item.create"}}, at)
	decide(t, tn, "builder", build.ID, build.SchemaRelease, build.PageType, "STOCK", map[string]any{}, at)
	member := memberOf(t, tn, "builder")
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	page := platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: "stockdesk"}
	unrelated := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: unrelatedType}
	full, refusal := tn.DependencyView(member, platform.AssetRef{}, "")
	if refusal != nil || !slices.ContainsFunc(full.Descriptors, func(d platform.Dependencies) bool { return d.Ref == unrelated }) {
		t.Fatalf("unfocused discovery lost an authorized unrelated asset: %+v %v", full, refusal)
	}
	for _, focus := range []platform.AssetRef{object, page} {
		view, refusal := tn.DependencyView(member, focus, focus.App)
		if refusal != nil {
			t.Fatal(refusal)
		}
		if focus == object && !slices.Contains(view.UsedBy, page) || focus == page && !slices.Contains(view.Requires, object) {
			t.Fatalf("focused discovery lost its cross-owner relation: %+v", view)
		}
		refs := append([]platform.AssetRef{focus}, append(slices.Clone(view.Requires), view.UsedBy...)...)
		if len(view.Descriptors) >= len(full.Descriptors) || len(view.Descriptors) != len(refs) {
			t.Fatalf("focused response retained unrelated descriptors: %+v", view)
		}
		for _, descriptor := range view.Descriptors {
			if !slices.Contains(refs, descriptor.Ref) || descriptor.Ref == unrelated {
				t.Fatalf("focused response retained an unrelated asset: %s", descriptor.Ref)
			}
		}
	}
}

func TestDependencyDiscoveryIncludesOriginalDatasetConfigurations(t *testing.T) {
	const id = "dataset-dependencies"
	tn := composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder"), seatOf("integrator", "build:integrator"), seatOf("user", "build:user")}, build.New(id))
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, dataset := range []struct{ id, name string }{{"RAW", "rawrows"}, {"CLEAN", "cleanrows"}} {
		decide(t, tn, "integrator", build.ID, build.DatasetType+".create", build.DatasetType, dataset.id, map[string]any{"name": dataset.name, "title": dataset.name}, at)
	}
	for range 2 {
		decide(t, tn, "integrator", build.ID, build.SchemaDatasetLoad, build.DatasetType, "RAW", map[string]any{"rows": []map[string]any{{"id": "ROW", "note": "runtime-only-dataset-content"}}}, at)
	}
	for _, pipeline := range []struct{ id, name string }{{"PIPELINE", "cleaning"}, {"DRAFT", "unpublishedcleaning"}} {
		decide(t, tn, "integrator", build.ID, build.PipelineType+".create", build.PipelineType, pipeline.id, map[string]any{"name": pipeline.name, "title": pipeline.name, "input": "RAW", "outputDataset": "CLEAN"}, at)
	}
	decide(t, tn, "integrator", build.ID, build.PipelineType+".publish", build.PipelineType, "PIPELINE", map[string]any{}, at)
	tn.RunPipelines(at)
	beforeRun, _ := platform.Get[build.Dataset](tn.automation(build.ID, false), "CLEAN")
	if beforeRun.Version != 0 {
		t.Fatal("publishing a pipeline executed it before an explicit request")
	}
	decide(t, tn, "integrator", build.ID, build.PipelineType+".run", build.PipelineType, "PIPELINE", map[string]any{}, at)
	tn.RunPipelines(at)
	c := tn.automation(build.ID, false)
	raw, _ := platform.Get[build.Dataset](c, "RAW")
	clean, _ := platform.Get[build.Dataset](c, "CLEAN")
	if raw.Version != 2 || clean.Version != 1 || raw.Published != "" || clean.Published != "" || raw.DefinitionVersion != 0 || clean.DefinitionVersion != 0 {
		t.Fatalf("expected original loaded datasets without candidate publication: raw=%+v clean=%+v", raw, clean)
	}
	version, _ := platform.Get[build.DatasetVersion](c, "CLEAN@1")
	if !bytes.Contains(version.Data, []byte("runtime-only-dataset-content")) {
		t.Fatal("the original pipeline did not write its dataset rows")
	}
	ownerAssets, err := tn.app(build.ID).(*build.Build).ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	pipeline := platform.AssetRef{App: build.ID, Kind: platform.AssetPipeline, Name: "cleaning"}
	draft := platform.AssetRef{App: build.ID, Kind: platform.AssetPipeline, Name: "unpublishedcleaning"}
	for _, who := range []string{"builder", "integrator"} {
		member := memberOf(t, tn, who)
		definitions, err := tn.definitionsFor(member)
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(definitions, func(def platform.Definition) bool { return def.Ref == draft }) || bytes.Contains(platform.Raw(definitions), []byte("runtime-only-dataset-content")) {
			t.Fatalf("%s discovery exposed unpublished pipeline configuration or dataset rows", who)
		}
		for _, dataset := range []struct{ id, name string }{{"RAW", "rawrows"}, {"CLEAN", "cleanrows"}} {
			ref := platform.AssetRef{App: build.ID, Kind: platform.AssetDataset, Name: dataset.name}
			definition := slices.IndexFunc(definitions, func(def platform.Definition) bool { return def.Ref == ref })
			asset := slices.IndexFunc(ownerAssets, func(asset platform.ReleaseAsset) bool { return asset.Ref == ref })
			if definition < 0 || asset < 0 {
				t.Fatalf("%s cannot discover the original dataset %s", who, ref)
			}
			def, original := definitions[definition], ownerAssets[asset]
			if def.Version != original.SourceVersion || def.ContractVersion != original.ContractVersion || !slices.Equal(def.Requires, original.Requires) || !bytes.Equal(platform.Raw(def.Provides), platform.Raw(original.Provides)) || def.OwnerRecord != build.DatasetType+"/"+dataset.id || !tn.Readable(member, def.OwnerRecord, at) {
				t.Fatalf("%s dataset projection differs from its original owner description: %+v", who, def)
			}
			view, refusal := tn.DependencyView(member, ref, "")
			if refusal != nil || !slices.Contains(view.UsedBy, pipeline) {
				t.Fatalf("%s cannot navigate dataset %s to its published pipeline: %+v %v", who, ref, view.UsedBy, refusal)
			}
		}
		view, refusal := tn.DependencyView(member, pipeline, "")
		if refusal != nil || len(view.Requires) != 2 {
			t.Fatalf("%s pipeline lost its original input/output dependencies: %+v %v", who, view.Requires, refusal)
		}
	}
	user := memberOf(t, tn, "user")
	for _, ref := range []platform.AssetRef{{App: build.ID, Kind: platform.AssetDataset, Name: "rawrows"}, pipeline, draft} {
		if _, refusal := tn.DependencyView(user, ref, ""); refusal == nil {
			t.Fatalf("ordinary member discovered integration metadata %s", ref)
		}
	}
	after, _ := platform.Get[build.Dataset](c, "RAW")
	if !bytes.Equal(platform.Raw(after), platform.Raw(raw)) {
		t.Fatal("reading dataset dependencies changed the original configuration or runtime versions")
	}
}
