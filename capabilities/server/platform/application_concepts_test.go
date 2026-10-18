package platform

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestApplicationConceptsOnlyOrganizeNavigation(t *testing.T) {
	object := AssetRef{App: "build", Kind: AssetObject, Name: "build.case"}
	page := AssetRef{App: "build", Kind: AssetPage, Name: "cases"}
	app := Application{Name: "desk", Title: "Desk", Pages: []string{page.Name}, Resources: []AssetRef{object}}
	before := app.Dependencies("build")
	historical, _ := json.Marshal(app)
	if strings.Contains(string(historical), "concepts") {
		t.Fatal("absent metadata changed historical application bytes")
	}
	app.Concepts = []AppConcept{{ID: "records", Title: "Cases", Assets: []AssetRef{object, page}}}
	if err := app.CheckResources(); err != nil {
		t.Fatal(err)
	}
	app.Concepts[0].Title = "Customer work"
	app.Concepts = append(app.Concepts, AppConcept{ID: "screens", Title: "Service desk", Assets: []AssetRef{page}})
	app.Concepts[0].Assets = []AssetRef{object}
	if err := app.CheckResources(); err != nil || !reflect.DeepEqual(before, app.Dependencies("build")) {
		t.Fatalf("regrouping changed application dependencies: %v", err)
	}
	// A reference retained after removing membership is navigation metadata,
	// never a dependency or an implicit grant to the previously grouped asset.
	app.Resources = nil
	if err := app.CheckConcepts(); err != nil || len(app.Dependencies("build")) != 1 {
		t.Fatalf("retained navigation reference changed ownership: %v", err)
	}
	for name, concepts := range map[string][]AppConcept{
		"empty ID":          {{Title: "Cases"}},
		"blank title":       {{ID: "cases", Title: " "}},
		"duplicate ID":      {{ID: "cases", Title: "Cases"}, {ID: "cases", Title: "Other"}},
		"duplicate asset":   {{ID: "cases", Title: "Cases", Assets: []AssetRef{object, object}}},
		"application root":  {{ID: "cases", Title: "Cases", Assets: []AssetRef{{App: "build", Kind: AssetApp, Name: "desk"}}}},
		"invalid reference": {{ID: "cases", Title: "Cases", Assets: []AssetRef{{Kind: AssetObject, Name: "case"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			app.Concepts = concepts
			if app.CheckConcepts() == nil {
				t.Fatal("invalid concept metadata accepted")
			}
		})
	}
}

func TestApplicationExtensionKeepsOwnPagesAndOriginalDependency(t *testing.T) {
	base := AssetRef{App: "mes", Kind: AssetApp, Name: "mes"}
	application := Application{Name: "mesworkspace", Title: "Manufacturing", Pages: []string{"orders"}, Extends: &base,
		Entry: "mes", Replacements: []ApplicationPageReplacement{{View: "orders", Page: "orders"}}}
	if err := application.CheckResources(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(application.Dependencies("build"), base) || !slices.Contains(application.Dependencies("build"), AssetRef{App: "build", Kind: AssetPage, Name: "orders"}) {
		t.Fatal("an extension omitted its original application or owned page")
	}
	for name, change := range map[string]func(*Application){
		"no base":            func(a *Application) { a.Extends = nil },
		"not an application": func(a *Application) { a.Extends = &AssetRef{App: "mes", Kind: AssetPage, Name: "orders"} },
		"missing entry":      func(a *Application) { a.Entry = "" },
		"blank entry":        func(a *Application) { a.Entry = " " },
		"foreign page":       func(a *Application) { a.Replacements = []ApplicationPageReplacement{{View: "orders", Page: "other"}} },
		"duplicate view":     func(a *Application) { a.Replacements = append(slices.Clone(a.Replacements), a.Replacements[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := application
			change(&invalid)
			if err := invalid.CheckExtension(); err == nil {
				t.Fatal("invalid extension accepted")
			}
		})
	}
	page, err := PageReleaseAsset("build", "1", Page{Name: "orders", Title: "Orders"})
	if err != nil {
		t.Fatal(err)
	}
	native, err := ApplicationReleaseAsset("mes", "1", Application{Name: "mes", Title: "Manufacturing"})
	if err != nil {
		t.Fatal(err)
	}
	extension, err := ApplicationReleaseAsset("build", "1", application)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := Candidate([]AssetRef{extension.Ref}, []ReleaseAsset{page, native, extension})
	if err != nil {
		t.Fatal(err)
	}
	retained, err := ReadCandidate(candidate.ID, candidate.Bytes)
	if err != nil || !reflect.DeepEqual(retained.Bytes, candidate.Bytes) || len(retained.Assets) != 3 {
		t.Fatalf("extension closure was not retained: %v", err)
	}
	application.Name = "otherworkspace"
	other, err := ApplicationReleaseAsset("build", "1", application)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Candidate([]AssetRef{extension.Ref, other.Ref}, []ReleaseAsset{page, native, extension, other}); err == nil || !strings.Contains(err.Error(), "replaced by both") {
		t.Fatalf("two extensions replaced the same original view: %v", err)
	}
	application.Entry, application.Replacements = "", nil
	if err := application.CheckExtension(); err != nil {
		t.Fatalf("an extension without replacements cannot use its default entry: %v", err)
	}
}
