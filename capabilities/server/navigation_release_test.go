package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestFrozenNavigationPagesActivateAndReplayAsOneClosure(t *testing.T) {
	const tenant = "frozen-navigation"
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}), build.New(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	member, _ := tn.Member("builder")
	var entries []Entry
	key := 0
	at := time.Now().UTC()
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, problem := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if problem != nil {
			t.Fatal(problem.Message)
		}
	}
	submit(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	submit(build.ObjectType, "object", "publish", map[string]any{})
	makeDoc := func(target string) *platform.PageDocument {
		d := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"button"}}, "button": {Kind: "widget", Section: "button"}}, Variables: map[string]platform.PageVariable{"flag": {Scope: "page", Type: "boolean", Mode: "state", Initial: json.RawMessage(`false`)}}, Interface: &platform.PageInterface{Version: 1}, Events: []platform.PageEventBinding{{Source: "button", Event: "click", Effects: []platform.PageEffect{{Kind: "set", Target: "flag", Value: json.RawMessage(`true`)}}}}}
		if target != "" {
			d.Events[0] = platform.PageEventBinding{Source: "button", Event: "click", Effects: []platform.PageEffect{{Kind: "navigate", Navigate: &platform.PageNavigation{Page: platform.AssetRef{App: build.ID, Kind: platform.AssetPage, Name: target}, InterfaceVersion: 1}}}}
		}
		return d
	}
	for _, name := range []string{"first", "second"} {
		submit(build.PageType, name, "create", map[string]any{"name": name, "title": name, "object": "build.note", "sections": []build.Section{{ID: "button", Widget: "button", ConfigVersion: 1}}, "document": makeDoc("")})
		submit(build.PageType, name, "publish", map[string]any{})
	}
	for _, pair := range [][2]string{{"first", "second"}, {"second", "first"}} {
		submit(build.PageType, pair[0], "edit", map[string]any{"document": makeDoc(pair[1])})
		submit(build.PageType, pair[0], "publish", map[string]any{})
	}
	preview, err := tn.PreviewRelease(member, platform.AssetPage, "first")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err := tn.SaveReleaseCandidate(member, platform.AssetPage, "first", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	later := makeDoc("first")
	later.Interface.Version = 2
	submit(build.PageType, "second", "edit", map[string]any{"document": later})
	if _, err := tn.ActivateRelease(member, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, tn, entries, compose)
	saved, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(saved); err != nil {
		t.Fatal("reciprocal pages failed original snapshot restore", err)
	}
	for _, broken := range []string{"target", "interface"} {
		var state tenantState
		if err := json.Unmarshal(saved, &state); err != nil {
			t.Fatal(err)
		}
		changed := false
		for i, row := range state.Records[build.PageType] {
			var page build.Page
			if json.Unmarshal(row.Value, &page) != nil || page.Name != "first" {
				continue
			}
			var published build.Page
			if err := json.Unmarshal([]byte(page.Published), &published); err != nil {
				t.Fatal(err)
			}
			nav := published.Document.Events[0].Effects[0].Navigate
			if broken == "target" {
				nav.Page.Name = "missing"
			} else {
				nav.InterfaceVersion = 2
			}
			page.Published = string(platform.Raw(published))
			row.Value = platform.Raw(page)
			state.Records[build.PageType][i] = row
			changed = true
		}
		if !changed {
			t.Fatal("published navigation tampering fixture missing")
		}
		if err := compose().Restore(platform.Raw(state)); err == nil || !strings.Contains(err.Error(), "navigation") {
			t.Fatalf("invalid original %s was not rejected by final restore navigation check: %v", broken, err)
		}
	}
	replayed := compose()
	if err := replayed.Replay(entries); err != nil {
		t.Fatal(err)
	}
	for _, current := range []*Tenant{tn, replayed} {
		for _, d := range current.definitions {
			if d.Ref.Kind == platform.AssetPage && (d.Ref.Name == "first" || d.Ref.Name == "second") {
				if d.Page.Document.Interface.Version != 1 || d.Page.Document.Events[0].Effects[0].Navigate == nil {
					t.Fatal("activation or replay lost frozen navigation")
				}
			}
		}
	}
}
