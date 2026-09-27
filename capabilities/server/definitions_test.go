package platformserver

import (
	"strings"
	"testing"

	"platformserver/platform"
)

type definitionStock struct {
	*stock
	manifest platform.Manifest
}

func (s definitionStock) Manifest() platform.Manifest { return s.manifest }

func TestDefinitions(t *testing.T) {
	tn := stockTenant(t)
	console := tn.app(PlatformApp).(*Console)
	clerk, _ := console.Member("ana")
	line, _ := console.Member("op")
	find := func(defs []platform.Definition, ref platform.AssetRef) *platform.Definition {
		for i := range defs {
			if defs[i].Ref == ref {
				return &defs[i]
			}
		}
		return nil
	}
	object := platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"}
	create := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.create"}
	count := platform.AssetRef{App: "stock", Kind: platform.AssetAction, Name: "stock.item.count"}
	if got := find(tn.Definitions(clerk), object); got == nil || got.Entity == nil || got.Version != "1" || got.Source != "code" {
		t.Fatalf("clerk's installed object: %+v", got)
	}
	if got := find(tn.Definitions(clerk), create); got == nil || len(got.Requires) != 1 || got.Requires[0] != object {
		t.Fatalf("create does not bind the registered object: %+v", got)
	}
	if got := find(tn.Definitions(clerk), count); got != nil {
		t.Fatalf("clerk discovered a line-only action: %+v", got)
	}
	if got := find(tn.Definitions(line), count); got == nil || got.Action == nil {
		t.Fatalf("line cannot discover its action: %+v", got)
	}
	if got := find(tn.Definitions(line), create); got != nil {
		t.Fatalf("line discovered a clerk-only action: %+v", got)
	}
	if got := find(tn.Definitions(platform.Member{ID: "outsider", Roles: map[string]string{}}), object); got != nil {
		t.Fatalf("outsider discovered stock: %+v", got)
	}
}

func TestDefinitionValidation(t *testing.T) {
	for _, bad := range []platform.AssetRef{{}, {App: "stock", Kind: "page", Name: "x"}, {App: "stock", Kind: platform.AssetObject}, {App: "stock/x", Kind: platform.AssetObject, Name: "item"}} {
		if err := bad.Check(); err == nil {
			t.Fatalf("invalid reference accepted: %+v", bad)
		}
	}
	for _, tc := range []struct {
		name, target, want string
	}{
		{"missing target", "missing.item", "targets missing data class missing.item"},
		{"duplicate asset", "stock.item", "is declared twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tn := stockTenant(t)
			for i, app := range tn.apps {
				if app.Manifest().ID != "stock" {
					continue
				}
				manifest := app.Manifest()
				actions := manifest.Actions.All()
				schema := "stock.item.invalid"
				if tc.name == "duplicate asset" {
					schema = "stock.item.count"
				}
				actions = append(actions, platform.Action{Schema: schema, Target: tc.target})
				manifest.Actions = platform.NewCatalog(actions...)
				tn.apps[i] = definitionStock{stock: app.(*stock), manifest: manifest}
				break
			}
			tn.definitions = nil
			if err := tn.registerDefinitions(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
	t.Run("incompatible record reference", func(t *testing.T) {
		tn := stockTenant(t)
		field := &tn.records.types["stock.item"].info.Fields[0]
		field.Type, field.Ref = "integer", "stock.bin"
		tn.definitions = nil
		if err := tn.registerDefinitions(); err == nil || !strings.Contains(err.Error(), "incompatible type") {
			t.Fatalf("incompatible reference accepted: %v", err)
		}
	})
}
