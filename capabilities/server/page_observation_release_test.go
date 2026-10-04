package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestFrozenFourObservationKindsProjectionReplayAndSnapshot(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("observations", NewConsole("observations", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("observations"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	key := 0
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, issue := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if issue != nil {
			t.Fatal(typ, id, verb, issue.Message)
		}
	}
	submit(build.ObjectType, "asset", "create", map[string]any{"name": "asset", "title": "Assets", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "availability", Title: "Availability", Type: "decimal"}}})
	submit(build.ObjectType, "asset", "publish", map[string]any{})
	submit(build.ObjectType, "sample", "create", map[string]any{"name": "sample", "title": "Actual samples", "fields": []build.Field{{Name: "at", Title: "Business time", Type: "datetime"}, {Name: "value", Title: "Pressure", Type: "decimal"}, {Name: "temperature", Title: "Private temperature", Type: "decimal", Read: []string{build.Builder}}, {Name: "availability", Title: "Availability", Type: "decimal"}, {Name: "asset", Title: "Asset", Type: "reference", Ref: "build.asset"}}})
	submit(build.ObjectType, "sample", "publish", map[string]any{})
	submit(build.QueryType, "history", "create", map[string]any{"name": "history", "title": "History by actual asset", "description": "Original business-time samples by original asset", "object": "build.sample", "by": "asset", "sort": []string{"-at", "id"}, "limit": 100})
	submit(build.QueryType, "history", "publish", map[string]any{})
	asset := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.asset"}
	sample := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.sample"}
	signals := []platform.PageObservationSignal{{Field: "value", Unit: "bar"}, {Field: "temperature", Unit: "C"}, {Field: "availability", Unit: "%"}}
	sections := []build.Section{
		{ID: "table", Widget: "observation", ConfigVersion: 1, CollectionVariable: "samples", Observation: &platform.PageObservation{Kind: "table", TimeField: "at", Signals: signals[:1], Metadata: &platform.PageObservationMetadata{Asset: "asset"}, AssetObject: &asset, AssetField: "asset", RowHeight: 30, RowOutput: "row", AssetOutput: "asset"}},
		{ID: "stats", Widget: "observation", ConfigVersion: 1, CollectionVariable: "samples", RecordVariable: "asset", ObservationContextVariable: "context", ObservationSignalVariable: "signal", ObservationThresholdVariable: "threshold", ObservationRowsVariable: "rows", Observation: &platform.PageObservation{Kind: "statistics", TimeField: "at", Signals: signals[:1], AssetObject: &asset, AssetField: "asset"}},
		{ID: "availability", Widget: "observation", ConfigVersion: 1, Object: asset.Name, CollectionVariable: "assets", ObservationHistoryVariable: "history", ObservationCountVariable: "count", ObservationMeanVariable: "mean", Observation: &platform.PageObservation{Kind: "availability", TimeField: "at", Signals: signals[2:], AverageField: "availability"}},
		{ID: "series", Widget: "observation", ConfigVersion: 1, CollectionVariable: "history", Observation: &platform.PageObservation{Kind: "series", TimeField: "at", Signals: signals}},
		{ID: "detail", Widget: "detail", ConfigVersion: 1, Object: asset.Name, RecordVariable: "asset", Fields: []string{"name"}},
	}
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows"}}, Variables: map[string]platform.PageVariable{}, Queries: map[string]platform.PageQuery{}}
	for _, id := range []string{"samples", "history", "context", "assets"} {
		object := sample
		sort := []string{"-at", "id"}
		if id == "assets" {
			object = asset
			sort = []string{"id"}
		}
		q := platform.PageQuery{Object: object, Limit: 100, Sort: sort}
		if id == "context" {
			q.For = &platform.PageValue{Variable: "asset"}
			q.Query = &platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "history"}, SourceVersion: "1.query-1"}
		}
		doc.Queries[id] = q
		doc.Variables[id] = platform.PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: id}}
	}
	for _, port := range []string{"row", "asset"} {
		doc.Variables[port] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table", Port: port}}
	}
	for id, initial := range map[string]string{"signal": "value", "threshold": "11.5", "rows": "10000"} {
		doc.Variables[id] = platform.PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw(initial)}
	}
	doc.Variables["count"] = platform.PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &platform.PageResourceSource{Kind: "count", Query: "assets"}}
	doc.Variables["mean"] = platform.PageVariable{Scope: "page", Type: "number", Mode: "aggregate", Source: &platform.PageResourceSource{Kind: "aggregate", Query: "assets", Measure: "avg:availability"}}
	for _, s := range sections {
		doc.Nodes[s.ID] = platform.PageLayoutNode{Kind: "widget", Section: s.ID}
		root := doc.Nodes["root"]
		root.Children = append(root.Children, s.ID)
		doc.Nodes["root"] = root
	}
	submit(build.PageType, "page", "create", map[string]any{"name": "observations", "title": "Original observations", "object": sample.Name, "sections": sections, "document": doc})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "page")
	if err != nil || preview.Diagnostic != "" {
		t.Fatal(preview, err)
	}
	saved, err := tn.SaveReleaseCandidate(builder, platform.AssetPage, "page", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	sections[0].Observation.Signals[0].Unit = "later"
	v := doc.Variables["threshold"]
	v.Initial = platform.Raw("999")
	doc.Variables["threshold"] = v
	submit(build.PageType, "page", "edit", map[string]any{"sections": sections, "document": doc})
	if _, err := tn.ActivateRelease(builder, saved, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			found := false
			for _, d := range current.Definitions(member) {
				if d.Page == nil || d.Ref.Name != "observations" {
					continue
				}
				found = true
				seen := map[string]platform.Section{}
				for _, s := range d.Page.Sections {
					seen[s.ID] = s
				}
				if seen["table"].Observation == nil || seen["table"].Observation.Signals[0].Unit != "bar" || string(d.Page.Document.Variables["threshold"].Initial) != `"11.5"` || d.Page.RecordResourceObject("asset") != asset || d.Page.RecordResourceObject("row") != sample || seen["availability"].Observation.AverageField != "availability" {
					t.Fatal("frozen original identities or state changed")
				}
				if member.ID == reader.ID {
					if _, ok := seen["series"]; ok {
						t.Fatal("private numeric signal exposed")
					}
					if _, ok := d.Page.Document.Nodes["series"]; ok {
						t.Fatal("orphan private series survived")
					}
				} else if seen["series"].Observation == nil {
					t.Fatal("builder original series disappeared")
				}
				if err := d.Page.Document.Check(d.Page.Sections); err != nil {
					t.Fatal(err)
				}
			}
			if !found {
				t.Fatal("original observation page disappeared")
			}
		}
	}
	check(tn)
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
