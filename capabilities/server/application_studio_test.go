package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

func TestApplicationStudioGroupingPreservesDefinitionsAndRecovery(t *testing.T) {
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return builderTenant(t, "application-studio") }
	tn := compose()
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "case", "title": "Case", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, at)
	object := platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: "build.case"}
	for _, name := range []string{"desk", "other"} {
		decide(t, tn, "builder", construction.ID, "build.app.create", construction.AppType, name, map[string]any{"name": name, "title": name, "resources": []platform.AssetRef{object}}, at)
		decide(t, tn, "builder", construction.ID, "build.app.publish", construction.AppType, name, map[string]any{}, at)
	}
	member := memberOf(t, tn, "builder")
	leafBytes := func() []byte {
		var definitions []platform.Definition
		for _, definition := range tn.Definitions(member) {
			if definition.Ref.Kind != platform.AssetApp {
				definitions = append(definitions, definition)
			}
		}
		return platform.Raw(definitions)
	}
	before := leafBytes()
	groups := []platform.AppConcept{{ID: "work", Title: "Customer work", Assets: []platform.AssetRef{object}}, {ID: "master", Title: "Master data", Assets: []platform.AssetRef{}}}
	decide(t, tn, "builder", construction.ID, "build.app.edit", construction.AppType, "desk", map[string]any{"concepts": groups}, at)
	groups[0].Title = "Service cases"
	groups[0].Assets = []platform.AssetRef{}
	groups[1].Assets = []platform.AssetRef{object}
	decide(t, tn, "builder", construction.ID, "build.app.edit", construction.AppType, "desk", map[string]any{"concepts": groups}, at)
	decide(t, tn, "builder", construction.ID, "build.app.publish", construction.AppType, "desk", map[string]any{}, at)
	if !bytes.Equal(before, leafBytes()) {
		t.Fatal("moving or renaming a business group changed asset definition bytes")
	}
	for _, definition := range tn.Definitions(member) {
		if definition.Ref == (platform.AssetRef{App: construction.ID, Kind: platform.AssetApp, Name: "desk"}) {
			if !reflect.DeepEqual(definition.Application.Concepts, groups) || !reflect.DeepEqual(definition.Requires, []platform.AssetRef{object}) {
				t.Fatalf("application metadata or semantic membership changed: %+v", definition)
			}
		}
	}
	if got := refuse(t, tn, "user", construction.ID, "build.app.edit", construction.AppType, "desk", map[string]any{"concepts": groups}, at); got == "ok" {
		t.Fatal("grouping granted a business user builder permissions")
	}
	for _, name := range []string{"desk", "other"} {
		preview, err := tn.PreviewRelease(member, platform.AssetApp, name)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview %s: %+v %v", name, preview, err)
		}
		if _, err := tn.SaveReleaseCandidate(member, platform.AssetApp, name, preview.CandidateID, "save-"+name, at); err != nil {
			t.Fatal(err)
		}
	}
	page, err := tn.SavedReleases(member, 0, 1, "desk")
	if err != nil || page.Total != 1 || len(page.Candidates) != 1 || page.Candidates[0].Title != "desk" {
		t.Fatalf("application candidate filter: %+v %v", page, err)
	}
	page, err = tn.SavedReleases(member, 1, 1, "desk")
	if err != nil || page.Total != 1 || len(page.Candidates) != 0 {
		t.Fatalf("filter must precede pagination: %+v %v", page, err)
	}
	CheckReplay(t, tn, entries, compose)
}

func TestApplicationStudioNativeExtensionPublicationAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		return composeTenant(t, "studio-extension", []Seat{seatOf("builder", "build:builder", "core:steward"), seatOf("user", "build:user")}, build.New("studio-extension"), core.New("studio-extension"))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	publishObject(t, tn, "builder", "O", map[string]any{"name": "case", "title": "Case", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, txnAt)
	base := platform.AssetRef{App: core.ID, Kind: platform.AssetApp, Name: core.ID}
	replacements := []platform.ApplicationPageReplacement{{View: "materials", Page: "case"}}
	decide(t, tn, "builder", construction.ID, "build.app.create", construction.AppType, "EXT", map[string]any{"name": "materialdesk", "title": "Material desk", "pages": []string{"case"}, "extends": base, "entry": "core", "replacements": replacements}, txnAt)
	member := memberOf(t, tn, "builder")
	preview, err := tn.PreviewRelease(member, platform.AssetApp, "EXT")
	if err != nil || preview.Diagnostic != "" || !slices.Contains(preview.Included, base) {
		t.Fatalf("preview lost the original application dependency: %+v %v", preview, err)
	}
	id, err := tn.SaveReleaseCandidate(member, platform.AssetApp, "EXT", preview.CandidateID, "save-extension", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", construction.ID, "build.app.create", construction.AppType, "OTHER", map[string]any{"name": "otherdesk", "title": "Other desk", "pages": []string{"case"}, "extends": base, "entry": "core", "replacements": replacements}, txnAt)
	otherPreview, err := tn.PreviewRelease(member, platform.AssetApp, "OTHER")
	if err != nil || otherPreview.Diagnostic != "" {
		t.Fatalf("independent extension draft before activation: %+v %v", otherPreview, err)
	}
	otherID, err := tn.SaveReleaseCandidate(member, platform.AssetApp, "OTHER", otherPreview.CandidateID, "save-other", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", construction.ID, "build.app.edit", construction.AppType, "EXT", map[string]any{"title": "Later draft"}, txnAt)
	if _, err := tn.ActivateRelease(member, id, "activate-extension", txnAt); err != nil {
		t.Fatal(err)
	}
	check := func(live *Tenant) {
		t.Helper()
		definitions := live.Definitions(memberOf(t, live, "builder"))
		i := slices.IndexFunc(definitions, func(d platform.Definition) bool {
			return d.Ref == (platform.AssetRef{App: construction.ID, Kind: platform.AssetApp, Name: "materialdesk"})
		})
		if i < 0 || definitions[i].Application.Extends == nil || *definitions[i].Application.Extends != base || definitions[i].Application.Entry != "core" || !reflect.DeepEqual(definitions[i].Application.Replacements, replacements) || !slices.Contains(definitions[i].Requires, base) {
			t.Fatalf("published extension lost its metadata or original owner: %+v", definitions)
		}
		var draft build.Application
		if raw, err := live.records.types[construction.AppType].rows.Get("EXT").image(); err != nil || json.Unmarshal(raw, &draft) != nil || draft.Title != "Later draft" {
			t.Fatal("activation replaced the mutable application draft")
		}
	}
	check(tn)
	userDefinitions := tn.Definitions(memberOf(t, tn, "user"))
	if slices.ContainsFunc(userDefinitions, func(definition platform.Definition) bool { return definition.Ref == base }) {
		t.Fatal("extension granted access to the original application's structure")
	}
	if !slices.ContainsFunc(userDefinitions, func(definition platform.Definition) bool {
		return definition.Ref.Name == "materialdesk" && definition.Application != nil && definition.Application.Extends != nil && *definition.Application.Extends == base
	}) {
		t.Fatal("the extension identity was lost when the original application is unavailable")
	}
	CheckReplay(t, tn, entries, compose)
	if _, err := tn.ActivateRelease(member, otherID, "activate-other", txnAt); err == nil || tn.ActiveRelease() != id {
		t.Fatal("activating an earlier candidate replaced an already claimed original view")
	}
	if got := refuse(t, tn, "builder", construction.ID, "build.app.publish", construction.AppType, "OTHER", map[string]any{}, txnAt); got == "ok" || !strings.Contains(got, "already replaced") {
		t.Fatalf("duplicate original view was published: %s", got)
	}
	if preview, err := tn.PreviewRelease(member, platform.AssetApp, "OTHER"); err == nil && preview.Diagnostic == "" {
		t.Fatal("conflicting extension produced a usable release candidate")
	}
	if got := refuse(t, tn, "builder", construction.ID, "build.app.edit", construction.AppType, "OTHER", map[string]any{"replacements": []platform.ApplicationPageReplacement{{View: "materials", Page: "foreign"}}}, txnAt); got == "ok" {
		t.Fatal("replacement accepted a page outside its own application")
	}
	if got := refuse(t, tn, "user", construction.ID, "build.app.edit", construction.AppType, "EXT", map[string]any{"title": "Unauthorized"}, txnAt); got == "ok" {
		t.Fatal("an application extension granted editing permissions")
	}
}

type studioReadOwner struct{ *notes }

func (a studioReadOwner) Read(platform.Caller, string) (any, *kernel.Error) { return a.id, nil }

func TestApplicationStudioScopedReadsKeepOriginalOwnerAuthorization(t *testing.T) {
	tn := composeTenant(t, "studio-reads", []Seat{seatOf("both", "a:writer", "b:writer"), seatOf("onlya", "a:writer")}, studioReadOwner{newNotes("studio-reads", "a")}, studioReadOwner{newNotes("studio-reads", "b")})
	handler := NewHost(Tokens(map[string]string{"both": "both", "onlya": "onlya"}), tn).Handler()
	for _, tc := range []struct {
		app, member, answer string
		status              int
	}{{"a", "both", `"a"`, http.StatusOK}, {"b", "both", `"b"`, http.StatusOK}, {"b", "onlya", "", http.StatusForbidden}, {"missing", "both", "", http.StatusNotFound}} {
		request := httptest.NewRequest(http.MethodGet, "/v1/apps/"+tc.app+"/reads/"+tc.app+"-notes", nil)
		request.Header.Set("Authorization", "Bearer "+tc.member)
		reply := httptest.NewRecorder()
		handler.ServeHTTP(reply, request)
		if reply.Code != tc.status || tc.answer != "" && strings.TrimSpace(reply.Body.String()) != tc.answer {
			t.Fatalf("%s as %s: %d %s", tc.app, tc.member, reply.Code, reply.Body.String())
		}
	}
	if _, err := tn.ReadFrom(memberOf(t, tn, "both"), "a", "b-notes"); err == nil {
		t.Fatal("a scoped read selected a different app's read")
	}
}

type studioEmptyOwner struct{ *notes }

func (a studioEmptyOwner) Manifest() platform.Manifest {
	return platform.Manifest{ID: a.id, Title: "Empty native application", Version: "1", Actions: platform.NewCatalog(), Roles: []string{"reader"}}
}

func TestApplicationStudioRetainsEmptyNativeApplicationIdentity(t *testing.T) {
	owner := studioEmptyOwner{&notes{id: "empty", ledger: platform.NewLedger("empty-studio", "empty", platform.NewCatalog()), texts: map[string]string{}}}
	tn := composeTenant(t, "empty-studio", []Seat{seatOf("reader", "empty:reader"), seatOf("other", "platform:admin")}, owner)
	ref := platform.AssetRef{App: "empty", Kind: platform.AssetApp, Name: "empty"}
	definitions := tn.Definitions(memberOf(t, tn, "reader"))
	i := slices.IndexFunc(definitions, func(definition platform.Definition) bool { return definition.Ref == ref })
	if i < 0 || definitions[i].Application == nil || len(definitions[i].Application.Pages) != 0 || len(definitions[i].Application.Resources) != 0 {
		t.Fatal("an empty native application lost its identity")
	}
	if slices.ContainsFunc(tn.Definitions(memberOf(t, tn, "other")), func(definition platform.Definition) bool { return definition.Ref == ref }) {
		t.Fatal("empty application discovery granted another member structure access")
	}
	if candidate, err := tn.ReleaseCandidate([]platform.AssetRef{ref}); err != nil || len(candidate.Assets) != 1 || candidate.Assets[0].Ref != ref {
		t.Fatalf("empty application cannot be an original release prerequisite: %+v %v", candidate, err)
	}
}
