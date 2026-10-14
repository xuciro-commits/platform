package platform

import (
	"encoding/json"
	"reflect"
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
