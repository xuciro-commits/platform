package platformserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"platformserver/apps/core"
	"platformserver/apps/enterprise"
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
	for i := range tn.definitions {
		if tn.definitions[i].Ref == object {
			tn.definitions[i].Requires = nil // no dependency must still be a JSON array
		}
	}
	if got := find(tn.Definitions(clerk), object); got == nil || got.Requires == nil {
		t.Fatalf("empty dependencies should be an array: %+v", got)
	} else if raw, err := json.Marshal(got); err != nil || !strings.Contains(string(raw), `"requires":[]`) {
		t.Fatalf("empty dependencies JSON: %s, %v", raw, err)
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
	for _, bad := range []platform.AssetRef{{}, {App: "stock", Kind: "widget", Name: "x"}, {App: "stock", Kind: platform.AssetObject}, {App: "stock/x", Kind: platform.AssetObject, Name: "item"}} {
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
	t.Run("directory reference", func(t *testing.T) {
		tn := stockTenant(t)
		field := &tn.records.types["stock.item"].info.Fields[0]
		field.Type, field.Ref = "reference", "enterprise.element"
		for i, app := range tn.apps {
			if app.Manifest().ID != "stock" {
				continue
			}
			manifest := app.Manifest()
			actions := manifest.Actions.All()
			actions = append(actions, platform.Action{Schema: "stock.item.assign", Target: "stock.item", Payload: []platform.Field{{Name: "unit", Type: "string", Ref: "enterprise.element"}}})
			manifest.Actions = platform.NewCatalog(actions...)
			tn.apps[i] = definitionStock{stock: app.(*stock), manifest: manifest}
		}
		tn.definitions = nil
		if err := tn.registerDefinitions(); err != nil {
			t.Fatalf("directory reference rejected as a record dependency: %v", err)
		}
		if tn.readableLocked(platform.Member{}, "enterprise.element/missing", time.Now()) {
			t.Fatal("a missing directory must not resolve references")
		}
	})
	t.Run("directory reference submission", func(t *testing.T) {
		at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
		tn := composeTenant(t, "directory-records", []Seat{seatOf("admin", "core:steward", "enterprise:admin")}, core.New("directory-records"), enterprise.New("directory-records", platform.OrgSeed{}))
		decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, "plant", map[string]any{"name": "Plant", "stereotype": enterprise.Location}, at)
		decide(t, tn, "admin", core.ID, "core.site.create", core.SiteType, "warehouse", map[string]any{"name": "Warehouse", "code": "W1", "kind": "warehouse", "place": "plant"}, at)
		decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementClose, enterprise.ElementType, "plant", map[string]any{"until": "2026-10-09"}, at)
		if el, ok := tn.directory.Element("plant", "2026-10-09"); ok {
			t.Fatalf("closed element still active: %+v", el)
		}
		if got := refuse(t, tn, "admin", core.ID, "core.site.create", core.SiteType, "late", map[string]any{"name": "Late", "code": "W2", "kind": "warehouse", "place": "plant"}, at.Add(24*time.Hour)); got == "ok" {
			t.Fatal("an ended model element was accepted")
		}
	})
}

func TestPageSelectionBindings(t *testing.T) {
	tn, err := NewTenant("page-selections", NewConsole("page-selections"), newTestCRMApp("page-selections"))
	if err != nil {
		t.Fatal(err)
	}
	account := platform.AssetRef{App: "crm", Kind: platform.AssetObject, Name: "crm.account"}
	opportunity := platform.AssetRef{App: "crm", Kind: platform.AssetObject, Name: "crm.opportunity"}
	parent, _ := tn.entity(account.Name)
	page := platform.Page{Name: "comparison", Object: account, Layout: "composed",
		Selections: []platform.SelectionVariable{{Name: "left", Object: account}, {Name: "right", Object: account}, {Name: "line", Object: opportunity}},
		Sections: []platform.Section{
			{Widget: "table", Selection: "left", Fields: []string{"name"}},
			{Widget: "table", Selection: "right", Fields: []string{"name"}},
			{Widget: "detail", Selection: "left", Fields: []string{"name"}},
			{Widget: "table", Selection: "line", ParentSelection: "left", Object: opportunity, Relation: "opportunities", Fields: []string{"title"}},
		}}
	if err := tn.checkSections(page, parent); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, want string
		change     func(*platform.Page)
	}{
		{"unknown variable", "does not hold", func(p *platform.Page) { p.Sections[2].Selection = "missing" }},
		{"wrong object", "does not hold", func(p *platform.Page) { p.Sections[2].Selection = "line" }},
		{"duplicate declaration", "unique lowercase", func(p *platform.Page) { p.Selections[1].Name = "left" }},
		{"wrong owner", "known object", func(p *platform.Page) { p.Selections[0].Object.App = "other" }},
		{"no producer", "supplies selection", func(p *platform.Page) { p.Sections[0].Selection = "right" }},
		{"wrong parent type", "parent selection", func(p *platform.Page) { p.Sections[3].ParentSelection = "line" }},
		{"ambiguous parent", "conflicting parent", func(p *platform.Page) {
			other := p.Sections[3]
			other.ParentSelection = "right"
			p.Sections = append(p.Sections, other)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := page
			p.Sections = append([]platform.Section(nil), page.Sections...)
			p.Selections = append([]platform.SelectionVariable(nil), page.Selections...)
			tc.change(&p)
			if err := tn.checkSections(p, parent); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestRelatedFormBinding(t *testing.T) {
	crm := newTestCRMApp("related-form")
	// This fixture's normal route is .open; the form requires .create.
	crm.ledger.Catalog.Add(platform.Action{Schema: "crm.opportunity.create", Target: "crm.opportunity", Title: "Create opportunity",
		Description: "Create an opportunity for an account.", Roles: []string{"sales"},
		Payload: []platform.Field{{Name: "account", Type: "string"}, {Name: "title", Type: "string"}}})
	tn, err := NewTenant("related-form", NewConsole("related-form"), crm)
	if err != nil {
		t.Fatal(err)
	}
	parent, _ := tn.entity("crm.account")
	form := platform.Section{Widget: "form", Object: platform.AssetRef{App: "crm", Kind: platform.AssetObject, Name: "crm.opportunity"},
		Relation: "opportunities", Fields: []string{"title"}}
	check := func(section platform.Section, selectParent bool) error {
		p := platform.Page{Name: "account-work", Object: platform.AssetRef{App: "crm", Kind: platform.AssetObject, Name: parent.Type}, Sections: []platform.Section{section}}
		if selectParent {
			p.Sections = append([]platform.Section{{Widget: "table", Fields: []string{"name"}}}, p.Sections...)
		}
		return tn.checkSections(p, parent)
	}
	if err := check(form, true); err != nil {
		t.Fatalf("the declared parent supplies the required account: %v", err)
	}
	for _, tc := range []struct {
		name, relation string
		fields         []string
		selectParent   bool
		want           string
	}{
		{"independent form needs the parent input", "", []string{"title"}, true, "needs account"},
		{"binding does not supply another required field", "opportunities", []string{"account"}, true, "needs title"},
		{"unknown relation", "unknown", []string{"title"}, true, "declares no relation"},
		{"no parent selection", "opportunities", []string{"title"}, false, "selects a parent record"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			section := form
			section.Relation, section.Fields = tc.relation, tc.fields
			if err := check(section, tc.selectParent); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPageDefinitions(t *testing.T) {
	newPage := func() platform.Page {
		return platform.Page{Name: "items", Title: "Items", Layout: "list-detail",
			Object:     platform.AssetRef{App: "stock", Kind: platform.AssetObject, Name: "stock.item"},
			ListFields: []string{"name", "qty"}, DetailFields: []string{"name", "qty", "bin"},
			Actions: []platform.AssetRef{{App: "stock", Kind: platform.AssetAction, Name: "stock.item.create"},
				{App: "stock", Kind: platform.AssetAction, Name: "stock.item.count"}}}
	}
	build := func(page platform.Page) *Tenant {
		tn := stockTenant(t)
		for i, app := range tn.apps {
			if app.Manifest().ID == "stock" {
				manifest := app.Manifest()
				manifest.Pages = []platform.Page{page}
				tn.apps[i] = definitionStock{stock: app.(*stock), manifest: manifest}
				break
			}
		}
		tn.definitions = nil
		return tn
	}
	tn := build(newPage())
	if err := tn.registerDefinitions(); err != nil {
		t.Fatal(err)
	}
	console := tn.app(PlatformApp).(*Console)
	clerk, _ := console.Member("ana")
	line, _ := console.Member("op")
	pageFor := func(m platform.Member) *platform.Page {
		for _, d := range tn.Definitions(m) {
			if d.Ref == (platform.AssetRef{App: "stock", Kind: platform.AssetPage, Name: "items"}) {
				return d.Page
			}
		}
		return nil
	}
	if got := pageFor(clerk); got == nil || len(got.Actions) != 1 || got.Actions[0].Name != "stock.item.create" {
		t.Fatalf("clerk page grants: %+v", got)
	}
	if got := pageFor(line); got == nil || len(got.Actions) != 1 || got.Actions[0].Name != "stock.item.count" {
		t.Fatalf("line page grants: %+v", got)
	}
	if got := pageFor(platform.Member{Roles: map[string]string{}}); got != nil {
		t.Fatalf("outsider page: %+v", got)
	}
	t.Run("code pages use the same selection contract", func(t *testing.T) {
		p := newPage()
		p.Layout, p.ListFields, p.DetailFields = "composed", nil, nil
		p.Selections = []platform.SelectionVariable{{Name: "chosen", Object: p.Object}}
		p.Sections = []platform.Section{{Widget: "table", Selection: "chosen", Fields: []string{"name"}}, {Widget: "detail", Selection: "chosen", Fields: []string{"name"}}}
		if err := build(p).registerDefinitions(); err != nil {
			t.Fatal(err)
		}
		p.Sections[1].Selection = "missing"
		if err := build(p).registerDefinitions(); err == nil || !strings.Contains(err.Error(), "does not hold") {
			t.Fatalf("code page bypassed binding validation: %v", err)
		}
	})
	for _, tc := range []struct {
		name, want string
		change     func(*platform.Page)
	}{
		{"missing field", "missing field", func(p *platform.Page) { p.ListFields = []string{"missing"} }},
		{"wrong action target", "targets stock.bin", func(p *platform.Page) {
			p.Actions = []platform.AssetRef{{App: "stock", Kind: platform.AssetAction, Name: "stock.bin.create"}}
		}},
		{"missing action", "missing action", func(p *platform.Page) {
			p.Actions = []platform.AssetRef{{App: "stock", Kind: platform.AssetAction, Name: "stock.unknown"}}
		}},
		{"duplicate field", "repeats field", func(p *platform.Page) { p.DetailFields = []string{"name", "name"} }},
		{"unknown layout", "unsupported layout", func(p *platform.Page) { p.Layout = "script" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPage()
			tc.change(&p)
			bad := build(p)
			if err := bad.registerDefinitions(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}
