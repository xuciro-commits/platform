package platformserver

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

// ADR-0058: interface reads retain concrete identity and original permissions;
// frozen query versions never acquire later implementers behind the reader.
func TestInterfaceQueriesKeepIdentityPermissionsAndFrozenImplementers(t *testing.T) {
	const tenant = "interface-queries"
	compose := func() *Tenant {
		return composeTenant(t, tenant, []Seat{seatOf("builder", "build:builder", "core:steward"), seatOf("reader", "build:user"), seatOf("outsider")},
			enterprise.New(tenant, platform.OrgSeed{}), core.New(tenant), build.New(tenant))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"alpha", "beta", "private"} {
		fields := []build.Field{{Name: "code", Title: "Code", Type: "text", Search: true}, {Name: "name", Title: "Name", Type: "text", Search: true}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}
		if name == "private" {
			fields[0].Read = []string{build.Builder}
		}
		publishObject(t, tn, "builder", name, map[string]any{"name": name, "title": name, "fields": fields, "implements": []string{"core.coded"}}, at)
		decide(t, tn, "builder", build.ID, "build."+name+".create", "build."+name, "SAME", map[string]any{"code": "DUP", "name": name, "secret": "private-data"}, at)
	}
	reader := memberOf(t, tn, "reader")
	window := platform.Query{Domain: json.RawMessage(`[["code","=","DUP"]]`), Sort: []string{"name"}, Limit: 1}
	first, err := tn.InterfaceRecords(reader, "core.coded", window, at)
	if err != nil || first.Total != 2 || len(first.Records) != 1 || first.Records[0].Type != "build.alpha" || first.Records[0].ID != "SAME" {
		t.Fatalf("first window: %+v %v", first, err)
	}
	window.Offset = 1
	second, err := tn.InterfaceRecords(reader, "core.coded", window, at)
	if err != nil || second.Total != 2 || len(second.Records) != 1 || second.Records[0].Type != "build.beta" || second.Records[0].ID != "SAME" {
		t.Fatalf("second identity collapsed: %+v %v", second, err)
	}
	if _, leaked := second.Records[0].Record["secret"]; leaked {
		t.Fatal("interface result exposed a subtype field")
	}
	if _, fake := tn.entity("core.coded"); fake {
		t.Fatal("interface became a fabricated object")
	}
	window.Offset = 0
	if page, err := tn.InterfaceRecords(memberOf(t, tn, "outsider"), "core.coded", window, at); err != nil || page.Total != 0 {
		t.Fatalf("unreadable types leaked counts: %+v %v", page, err)
	}
	window.Domain = json.RawMessage(`[["secret","=","private-data"]]`)
	if _, err := tn.InterfaceRecords(reader, "core.coded", window, at); err == nil {
		t.Fatal("interface condition read a subtype or hidden field")
	}

	decide(t, tn, "builder", build.ID, "build.query.create", build.QueryType, "QUERY", map[string]any{"name": "coded", "title": "Coded records", "description": "Interface read", "interface": "core.coded", "domain": [][]any{{"code", "=", "DUP"}}, "sort": []string{"name"}, "limit": 20}, at)
	if refusal := refuse(t, tn, "reader", build.ID, "build.query.publish", build.QueryType, "QUERY", map[string]any{}, at); refusal == "ok" {
		t.Fatal("reader published a query")
	}
	builder := memberOf(t, tn, "builder")
	preview, problem := tn.PreviewRelease(builder, platform.AssetQuery, "QUERY")
	if problem != nil || preview.Diagnostic != "" {
		t.Fatalf("query preview: %+v %v", preview, problem)
	}
	if _, err := tn.SaveReleaseCandidate(builder, platform.AssetQuery, "QUERY", preview.CandidateID, "save-interface-query", at); err != nil {
		t.Fatal(err)
	}
	publishObject(t, tn, "builder", "later", map[string]any{"name": "later", "title": "Later", "fields": []build.Field{{Name: "code", Title: "Code", Type: "text", Search: true}, {Name: "name", Title: "Name", Type: "text", Search: true}}, "implements": []string{"core.coded"}}, at)
	decide(t, tn, "builder", build.ID, "build.later.create", "build.later", "SAME", map[string]any{"code": "DUP", "name": "later"}, at)
	if _, err := tn.ActivateRelease(builder, preview.CandidateID, "activate-interface-query", at); err != nil {
		t.Fatal(err)
	}
	readVersion := func(version string, want int) {
		t.Helper()
		page, err := tn.RunQueryWindow(reader, build.ID, "coded", "", version, platform.Query{}, at)
		if err != nil || page.Total != want {
			t.Fatalf("query %s: %+v %v", version, page, err)
		}
		for _, raw := range page.Records {
			row := raw.(InterfaceRecord)
			if row.Type == "build.private" || row.ID != "SAME" {
				t.Fatalf("wrong target: %+v", row)
			}
		}
	}
	readVersion("1.query-1", 2)
	for _, definition := range tn.Definitions(reader) {
		if definition.Ref.Kind == platform.AssetQuery && definition.Ref.Name == "coded" {
			if slices.Contains(definition.Query.Implementations, "build.private") || slices.Contains(definition.Query.Implementations, "build.later") {
				t.Fatal("query metadata leaked private or later implementations")
			}
		}
	}
	decide(t, tn, "builder", build.ID, "build.query.publish", build.QueryType, "QUERY", map[string]any{}, at)
	readVersion("1.query-1", 2)
	readVersion("1.query-2", 3)
	CheckReplay(t, tn, entries, compose)
	t.Run("joint unpublished implementation", func(t *testing.T) {
		fresh := compose()
		decide(t, fresh, "builder", build.ID, "build.object.create", build.ObjectType, "NEW", map[string]any{"name": "newcoded", "title": "New coded object", "implements": []string{"core.coded"}, "fields": []build.Field{{Name: "code", Title: "Code", Type: "text"}, {Name: "name", Title: "Name", Type: "text"}}}, at)
		decide(t, fresh, "builder", build.ID, "build.query.create", build.QueryType, "NEWQUERY", map[string]any{"name": "newquery", "title": "New query", "description": "Joint interface read", "interface": "core.coded", "domain": [][]any{{"code", "=", "NEW"}}, "limit": 20}, at)
		builder := memberOf(t, fresh, "builder")
		drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "NEW"}, {Kind: platform.AssetQuery, ID: "NEWQUERY"}}
		preview, err := fresh.PreviewReleaseDrafts(builder, drafts)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("joint interface preview: %+v %v", preview, err)
		}
		if _, err := fresh.SaveReleaseCandidates(builder, drafts, preview.CandidateID, "joint-query-save", at); err != nil {
			t.Fatal(err)
		}
		if _, err := fresh.ActivateRelease(builder, preview.CandidateID, "joint-query-active", at); err != nil {
			t.Fatal(err)
		}
		decide(t, fresh, "reader", build.ID, "build.newcoded.create", "build.newcoded", "NEW", map[string]any{"code": "NEW", "name": "Joint row"}, at)
		page, refusal := fresh.RunQuery(memberOf(t, fresh, "reader"), build.ID, "newquery", "", at)
		if refusal != nil || page.Total != 1 || page.Records[0].(InterfaceRecord).Type != "build.newcoded" {
			t.Fatalf("joint interface target: %+v %v", page, refusal)
		}
	})
}
