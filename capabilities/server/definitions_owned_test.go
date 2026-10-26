package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func TestOwnedAssetDiscoveryUsesMetadataReadAuthority(t *testing.T) {
	tn := composeTenant(t, "owned-discovery", []Seat{
		seatOf("builder", construction.ID+":"+construction.Builder),
		seatOf("integrator", construction.ID+":"+construction.Integrator),
		seatOf("user", construction.ID+":"+construction.User),
		seatOf("outsider"),
	}, build.New("owned-discovery"))
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	decide(t, tn, "builder", construction.ID, construction.DatasetType+".create", construction.DatasetType, "private-dataset-id", map[string]any{"name": "input", "title": "Input rows"}, at)
	ref := platform.AssetRef{App: construction.ID, Kind: platform.AssetDataset, Name: "input"}
	builder := memberOf(t, tn, "builder")
	preview, err := tn.PreviewReleaseDrafts(builder, []construction.JointDraftRef{{Kind: platform.AssetDataset, ID: "private-dataset-id"}})
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("dataset candidate: %+v %v", preview, err)
	}
	if _, err := tn.SaveReleaseCandidates(builder, preview.Drafts, preview.CandidateID, "dataset-save", at); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(builder, preview.CandidateID, "dataset-activate", at); err != nil {
		t.Fatal(err)
	}
	before := snapshot(tn)
	find := func(m platform.Member) *platform.Definition {
		defs := tn.Definitions(m)
		for i := range defs {
			if defs[i].Ref == ref {
				return &defs[i]
			}
		}
		return nil
	}
	for _, id := range []string{"builder", "integrator"} {
		def := find(memberOf(t, tn, id))
		if def == nil {
			t.Fatalf("%s cannot discover metadata it can read", id)
		}
		raw, err := json.Marshal(def)
		if err != nil || strings.Contains(string(raw), "private-dataset-id") || strings.Contains(string(raw), "OwnerRecord") {
			t.Fatalf("internal metadata identity escaped discovery: %s %v", raw, err)
		}
	}
	for _, id := range []string{"user", "outsider"} {
		if find(memberOf(t, tn, id)) != nil {
			t.Fatalf("%s discovered private integration metadata", id)
		}
	}
	if snapshot(tn) != before {
		t.Fatal("member discovery mutated the live registry, journal state or records")
	}
	foreign := memberOf(t, tn, "builder")
	foreign.Tenant = "another-tenant"
	if find(foreign) != nil {
		t.Fatal("foreign-tenant member discovered integration metadata")
	}
	missing := *find(builder)
	missing.OwnerRecord = construction.DatasetType + "/missing"
	if slices.ContainsFunc(tn.definitionsFrom(memberOf(t, tn, "builder"), []platform.Definition{missing}), func(def platform.Definition) bool { return def.Ref == ref }) {
		t.Fatal("asset without readable metadata record was offered")
	}
	// A damaged accepted image is an explicit discovery error, rather than an
	// invented legacy configuration or a stale cached definition.
	row := tn.records.types[construction.DatasetType].rows.Get("private-dataset-id")
	row.Value.FieldByName("Published").SetString("{damaged")
	row.Original = nil
	tn.setDefinitions(tn.definitions) // direct damage moves the cache generation like any install would
	if _, err := tn.definitionsFor(builder); err == nil || len(tn.Definitions(builder)) != 0 {
		t.Fatal("damaged accepted metadata was silently omitted or served stale")
	}
}

func TestOwnedAssetDiscoveryPreservesRowScopeAndFiltersDependencies(t *testing.T) {
	tn := stockTenant(t)
	at := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	decide(t, tn, "ana", "stock", "stock.item.create", "stock.item", "own", map[string]any{"name": "Own metadata", "kind": "part"}, at)
	decide(t, tn, "bo", "stock", "stock.item.create", "stock.item", "other", map[string]any{"name": "Other metadata", "kind": "part"}, at)
	visible := platform.AssetRef{App: "stock", Kind: platform.AssetSource, Name: "own"}
	hidden := platform.AssetRef{App: "stock", Kind: platform.AssetSource, Name: "other"}
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	lineAction := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.count"}
	original := []platform.AssetRef{object, hidden, lineAction}
	tn.definitions = append(tn.definitions,
		platform.Definition{Ref: visible, OwnerRecord: "stock.item/own", Requires: original},
		platform.Definition{Ref: hidden, OwnerRecord: "stock.item/other"})
	found := false
	for _, def := range tn.definitionsFrom(memberOf(t, tn, "ana"), tn.definitions) {
		if def.Ref == hidden {
			t.Fatal("metadata type visibility bypassed actual row scope")
		}
		if def.Ref == visible {
			if !slices.Equal(def.Requires, []platform.AssetRef{object}) || !slices.Equal(original, []platform.AssetRef{object, hidden, lineAction}) {
				t.Fatal("dependency projection leaked private refs or mutated registered metadata")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("member's own readable metadata was not discoverable")
	}
}

func TestOwnedAssetDiscoveryPreservesNativeMetadataDefinitions(t *testing.T) {
	for _, tenant := range []*Tenant{stockTenant(t), builderTenant(t, "native-build-metadata")} {
		app := "stock"
		if tenant.app(construction.ID) != nil {
			app = construction.ID
		}
		ref := platform.AssetRef{App: app, Kind: platform.AssetObservation, Name: "temperature"}
		native := platform.Definition{Ref: ref, Source: "code", Version: "1", ContractVersion: 1, Observation: &platform.ObservationDefinition{Name: "temperature", Title: "Native temperature"}}
		definitions, err := tenant.ownedAssetDefinitions([]platform.Definition{native})
		if err != nil || len(definitions) != 1 || definitions[0].Ref != ref || definitions[0].Source != "code" || definitions[0].Observation != native.Observation {
			t.Fatalf("controlled metadata projection removed a native declaration: %+v %v", definitions, err)
		}
	}
}
