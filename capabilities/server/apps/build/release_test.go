package build

import (
	"bytes"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestPublishedBuilderReleaseUsesCompleteOwnerDefinitions(t *testing.T) {
	object := Object{
		Name: "visit", Title: "Visit", Fields: []Field{{Name: "guest", Title: "Guest", Type: "text", Required: true, Search: true}},
		Access: []Access{{Role: User, Read: "own"}},
	}
	object.ID = "O1"
	object.Published = published(object)
	page := Page{Name: "visits", Title: "Visits", Object: "build.visit", List: []string{"guest"}, Detail: []string{"guest"},
		Actions: []string{"build.visit.create"}}
	page.ID = "P1"
	page.Published = published(page)
	app := Application{Name: "desk", Title: "Desk", Pages: []string{"visits"}}
	app.ID = "A1"
	app.Published = published(app)

	root := platform.AssetRef{App: ID, Kind: platform.AssetApp, Name: "desk"}
	release := func(o Object) platform.ReleaseCandidate {
		t.Helper()
		assets, err := releaseAssets([]Object{o}, []Page{page}, []Application{app}, nil, "1")
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := platform.Candidate([]platform.AssetRef{root}, assets)
		if err != nil {
			t.Fatal(err)
		}
		if len(candidate.Assets) != 4 {
			t.Fatalf("expected object, action, page and application, got %d assets", len(candidate.Assets))
		}
		if _, err := platform.ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
			t.Fatal(err)
		}
		return candidate
	}
	first := release(object)
	if !bytes.Contains(first.Bytes, []byte(`"access"`)) || !bytes.Contains(first.Bytes, []byte(`"own"`)) {
		t.Fatal("owner's complete access rule was omitted from the revision")
	}
	object.Title = "Unsaved draft"
	if candidate := release(object); candidate.ID != first.ID {
		t.Fatal("editing a draft changed the installed release")
	}
	object.Access = nil
	object.Published = published(object)
	if candidate := release(object); candidate.ID == first.ID {
		t.Fatal("changing published access retained the old content identity")
	}
}

func TestPublishedBuilderReleaseRejectsUnclosedReferences(t *testing.T) {
	object := Object{Name: "visit", Title: "Visit", Fields: []Field{
		{Name: "account", Title: "Account", Type: "reference", Ref: "crm.account"},
	}}
	object.ID = "O1"
	object.Published = published(object)
	assets, err := releaseAssets([]Object{object}, nil, nil, nil, "1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = platform.Candidate([]platform.AssetRef{{App: ID, Kind: platform.AssetObject, Name: "build.visit"}}, assets)
	if err == nil || !strings.Contains(err.Error(), "missing asset crm/object/crm.account") {
		t.Fatalf("expected a closed object reference, got %v", err)
	}
}

func TestPublishedBuilderPageReplacesTheGeneratedPage(t *testing.T) {
	object := Object{Name: "visit", Title: "Visit", Fields: []Field{{Name: "guest", Title: "Guest", Type: "text"}}}
	object.ID = "O1"
	object.Published = published(object)
	page := Page{Name: "visit", Title: "Reception visits", Object: "build.visit",
		List: []string{"guest"}, Detail: []string{"guest"}}
	page.ID = "P1"
	page.Published = published(page)
	assets, err := releaseAssets([]Object{object}, []Page{page}, nil, nil, "1")
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.Candidate([]platform.AssetRef{{App: ID, Kind: platform.AssetPage, Name: "visit"}}, assets)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(candidate.Bytes, []byte("Reception visits")) {
		t.Fatal("explicit published page did not replace the generated page of the same name")
	}
}
