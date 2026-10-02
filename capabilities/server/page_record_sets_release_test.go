package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
	"testing"
	"time"
)

func TestFrozenRecordSetPortsAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("record-sets", NewConsole("record-sets", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New("record-sets"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	var entries []Entry
	key := 0
	at := time.Now().UTC()
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	submit := func(typ, id, verb string, payload any) {
		t.Helper()
		key++
		_, err := tn.Submit(builder, &pb.Submission{TenantId: tn.ID, PrincipalId: builder.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	submit(build.ObjectType, "O", "create", map[string]any{"name": "note", "title": "Notes", "states": []build.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}}, "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}, {Name: "amount", Title: "Amount", Type: "decimal"}, {Name: "sensitive", Title: "Sensitive", Type: "decimal", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "O", "publish", map[string]any{})
	submit(build.ObjectType, "private-object", "create", map[string]any{"name": "private", "title": "Private", "states": []build.State{{Name: "open", Title: "Private open"}, {Name: "done", Title: "Private done"}}, "access": []build.Access{{Role: build.User, Read: "none"}}, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "parent", Title: "Parent", Type: "reference", Ref: "build.note", Inverse: "privateitems"}}})
	submit(build.ObjectType, "private-object", "publish", map[string]any{})
	submit(build.ObjectType, "child-object", "create", map[string]any{"name": "child", "title": "Children", "fields": []build.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.note", Inverse: "children"}, {Name: "hidden", Title: "Hidden parent", Type: "reference", Ref: "build.note", Inverse: "hiddenchildren", Read: []string{build.Builder}}}})
	submit(build.ObjectType, "child-object", "publish", map[string]any{})
	doc := platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]platform.PageVariable{"picked": {Scope: "page", Type: "record-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "records", Section: "table"}}}}
	doc.Variables["shown"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}
	doc.Events = []platform.PageEventBinding{{Source: "table", Event: "select", Target: "shown", Value: platform.Raw(true)}}
	sections := []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name", "secret"}, SelectionSetVariable: "picked"}}
	search := false
	sections[0].ShowSearch = &search
	sections[0].TableColumns = []platform.PageTableColumn{{Field: "name", Title: "Frozen name", Width: 170, Formatter: "text"}, {Field: "secret", Title: "Private column", Width: 110}}
	doc.Nodes["detail"] = platform.PageLayoutNode{Kind: "widget", Section: "detail"}
	root := doc.Nodes[doc.Root]
	root.Children = append(root.Children, "detail")
	doc.Nodes[doc.Root] = root
	sections = append(sections, build.Section{ID: "detail", Widget: "detail", ConfigVersion: 1, Fields: []string{"name", "secret"}, DetailPresentation: &platform.PageDetailPresentation{Columns: 2, HideNull: true}})
	doc.Variables["active"] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}
	doc.Nodes["view"] = platform.PageLayoutNode{Kind: "widget", Section: "view"}
	root = doc.Nodes[doc.Root]
	root.Children = append(root.Children, "view")
	doc.Nodes[doc.Root] = root
	sections = append(sections, build.Section{ID: "view", Widget: "record-view", ConfigVersion: 1, RecordVariable: "active", Fields: []string{"name", "secret"}, RecordView: &platform.PageRecordView{Tabs: []string{"overview", "properties", "links", "history"}}})
	doc.Nodes["commands"] = platform.PageLayoutNode{Kind: "widget", Section: "commands"}
	root = doc.Nodes[doc.Root]
	root.Children = append(root.Children, "commands")
	doc.Nodes[doc.Root] = root
	sections = append(sections, build.Section{ID: "commands", Widget: "button-group", ConfigVersion: 1, Buttons: []platform.PageButton{{ID: "show", Title: "Show", Variant: "primary", Icon: "arrow"}, {ID: "hide", Title: "Hide", Variant: "danger", Icon: "trash"}}})
	doc.Events = append(doc.Events, platform.PageEventBinding{Source: "commands", Control: "show", Event: "click", Target: "shown", Value: platform.Raw(true)}, platform.PageEventBinding{Source: "commands", Control: "hide", Event: "click", Target: "shown", Value: platform.Raw(false)})
	doc.Variables["privateOpen"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}
	doc.Nodes["privateRoot"] = platform.PageLayoutNode{Kind: "rows", Children: []string{"privateBody"}}
	doc.Nodes["privateBody"] = platform.PageLayoutNode{Kind: "widget", Section: "privateBody"}
	doc.Overlays = map[string]platform.PageOverlay{"private": {Root: "privateRoot", Kind: "drawer", Title: "Private panel", OpenVariable: "privateOpen"}}
	sections[3].Buttons = append(sections[3].Buttons, platform.PageButton{ID: "private", Title: "Private command", Icon: "trash"})
	doc.Events = append(doc.Events, platform.PageEventBinding{Source: "commands", Control: "private", Event: "click", Target: "privateOpen", Value: platform.Raw(true)})
	sections = append(sections, build.Section{ID: "privateBody", Widget: "detail", ConfigVersion: 1, Object: "build.private", Fields: []string{"note"}})
	doc.Nodes["links"] = platform.PageLayoutNode{Kind: "widget", Section: "links"}
	root = doc.Nodes[doc.Root]
	root.Children = append(root.Children, "links")
	doc.Nodes[doc.Root] = root
	sections = append(sections, build.Section{ID: "links", Widget: "record-links", ConfigVersion: 1, RecordVariable: "active", RecordLinks: []platform.PageRecordLink{{Object: platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.child"}, Field: "parent", Title: "Public children"}, {Object: platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.child"}, Field: "hidden", Title: "Hidden children"}, {Object: platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.private"}, Field: "parent", Title: "Private children"}}})
	for _, id := range []string{"status", "privateStatus"} {
		doc.Nodes[id] = platform.PageLayoutNode{Kind: "widget", Section: id}
		root = doc.Nodes[doc.Root]
		root.Children = append(root.Children, id)
		doc.Nodes[doc.Root] = root
		object := ""
		if id == "privateStatus" {
			object = "build.private"
		}
		sections = append(sections, build.Section{ID: id, Widget: "status-tracker", ConfigVersion: 1, Object: object, StatusTracker: &platform.PageStatusTracker{Field: "state", Stages: []string{"done", "open"}}})
	}
	doc.Queries = map[string]platform.PageQuery{"metricq": {Object: platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.note"}, Limit: 1, Sort: []string{"id"}}}
	doc.Variables["metricWindow"] = platform.PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "metricq"}}
	for _, field := range []string{"amount", "sensitive"} {
		id := "metric_" + field
		doc.Nodes[id] = platform.PageLayoutNode{Kind: "widget", Section: id}
		root = doc.Nodes[doc.Root]
		root.Children = append(root.Children, id)
		doc.Nodes[doc.Root] = root
		sections = append(sections, build.Section{ID: id, Widget: "metric", ConfigVersion: 1, CollectionVariable: "metricWindow", Measure: "avg:" + field, MetricPresentation: &platform.PageMetricPresentation{Suffix: "%", Formatter: "number", Variant: "card", Tone: "success"}})
	}
	doc.Variables["collectionCount"] = platform.PageVariable{Scope: "page", Type: "decimal", Mode: "aggregate", Source: &platform.PageResourceSource{Kind: "count", Query: "metricq"}}
	for _, id := range []string{"heading", "collectionTitle"} {
		doc.Nodes[id] = platform.PageLayoutNode{Kind: "widget", Section: id}
		root = doc.Nodes[doc.Root]
		root.Children = append(root.Children, id)
		doc.Nodes[doc.Root] = root
	}
	sections = append(sections, build.Section{ID: "heading", Widget: "heading", ConfigVersion: 1, HeadingLevel: "h2", Text: "Frozen literal heading"}, build.Section{ID: "collectionTitle", Widget: "collection-title", ConfigVersion: 1, Title: "Frozen collection", CollectionVariable: "metricWindow", CountVariable: "collectionCount"})
	for _, id := range []string{"cards", "hiddenCards"} {
		doc.Nodes[id] = platform.PageLayoutNode{Kind: "widget", Section: id}
		root = doc.Nodes[doc.Root]
		root.Children = append(root.Children, id)
		doc.Nodes[doc.Root] = root
		label := "name"
		if id == "hiddenCards" {
			label = "secret"
		}
		sections = append(sections, build.Section{ID: id, Widget: "record-list", ConfigVersion: 1, CollectionVariable: "metricWindow", CardLabel: label, Fields: []string{"name", "secret"}, RecordList: &platform.PageRecordList{Layout: "grid"}})
	}
	doc.Variables["cardRecord"] = platform.PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "cards"}}
	for _, spec := range []struct{ id, group, measure string }{{"chart", "name", "avg:amount"}, {"hiddenChartGroup", "secret", "avg:amount"}, {"hiddenChartMeasure", "name", "avg:sensitive"}} {
		doc.Nodes[spec.id] = platform.PageLayoutNode{Kind: "widget", Section: spec.id}
		root = doc.Nodes[doc.Root]
		root.Children = append(root.Children, spec.id)
		doc.Nodes[doc.Root] = root
		sections = append(sections, build.Section{ID: spec.id, Widget: "chart", ConfigVersion: 1, CollectionVariable: "metricWindow", Mark: "bar", Group: spec.group, Measure: spec.measure})
	}
	for _, spec := range []struct{ id, x, y string }{{"recordChart", "name", "amount"}, {"hiddenRecordX", "secret", "amount"}, {"hiddenRecordY", "name", "sensitive"}} {
		doc.Nodes[spec.id] = platform.PageLayoutNode{Kind: "widget", Section: spec.id}
		root = doc.Nodes[doc.Root]
		root.Children = append(root.Children, spec.id)
		doc.Nodes[doc.Root] = root
		sections = append(sections, build.Section{ID: spec.id, Widget: "record-chart", ConfigVersion: 1, CollectionVariable: "metricWindow", RecordChart: &platform.PageRecordChart{Mark: "line", XField: spec.x, YField: spec.y}})
	}
	for _, spec := range []struct{ id, group string }{{"pie", "name"}, {"hiddenPie", "secret"}} {
		doc.Nodes[spec.id] = platform.PageLayoutNode{Kind: "widget", Section: spec.id}
		root = doc.Nodes[doc.Root]
		root.Children = append(root.Children, spec.id)
		doc.Nodes[doc.Root] = root
		sections = append(sections, build.Section{ID: spec.id, Widget: "chart", ConfigVersion: 1, CollectionVariable: "metricWindow", Mark: "arc", Group: spec.group, Measure: "count", ChartVariant: "donut"})
	}
	submit(build.PageType, "P", "create", map[string]any{"name": "notes", "title": "Notes", "object": "build.note", "document": doc, "sections": sections})
	preview, err := tn.PreviewRelease(builder, platform.AssetPage, "P")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetPage, "P", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	sections[0].SelectionSetVariable = ""
	sections[0].TableColumns = nil
	sections[0].ShowSearch = nil
	sections[1].DetailPresentation = nil
	sections[2].RecordView = &platform.PageRecordView{Tabs: []string{"properties"}}
	sections[3].Buttons[0].Title = "Later draft title"
	sections[5].RecordLinks[0].Title = "Later links"
	sections[12].RecordList = &platform.PageRecordList{Layout: "list"}
	sections[12].CardLabel = "id"
	sections[14].Group = "state"
	sections[14].Measure = "count"
	sections[17].RecordChart = &platform.PageRecordChart{Mark: "bar", XField: "id", YField: "amount"}
	sections[20].ChartVariant = "pie"
	sections[10].Text = "Later heading"
	sections[11].Title = "Later collection"
	sections[8].MetricPresentation = &platform.PageMetricPresentation{Prefix: "Later", Formatter: "short", Variant: "tag", Tone: "danger"}
	sections[6].StatusTracker = &platform.PageStatusTracker{Field: "state", Stages: []string{"open"}}
	doc.Events = nil
	submit(build.PageType, "P", "edit", map[string]any{"sections": sections, "document": doc})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, member := range []platform.Member{builder, reader} {
			seen := false
			for _, d := range current.Definitions(member) {
				if d.Page != nil && d.Ref.Name == "notes" {
					seen = true
					pie, hiddenPie := false, false
					for _, s := range d.Page.Sections {
						if s.ID == "pie" {
							pie = s.ChartVariant == "donut" && s.Mark == "arc" && s.Group == "name" && s.Measure == "count"
						}
						if s.ID == "hiddenPie" {
							hiddenPie = true
						}
					}
					if !pie || hiddenPie != (member.ID == builder.ID) {
						t.Fatal("frozen pie presentation or member projection changed")
					}
					recordChart, hiddenRecordX, hiddenRecordY := false, false, false
					for _, s := range d.Page.Sections {
						switch s.ID {
						case "recordChart":
							recordChart = s.RecordChart != nil && s.RecordChart.Mark == "line" && s.RecordChart.XField == "name" && s.RecordChart.YField == "amount" && s.CollectionVariable == "metricWindow"
						case "hiddenRecordX":
							hiddenRecordX = true
						case "hiddenRecordY":
							hiddenRecordY = true
						}
					}
					if !recordChart || hiddenRecordX != (member.ID == builder.ID) || hiddenRecordY != (member.ID == builder.ID) || d.Page.Document.Queries["metricq"].Sort[0] != "id" {
						t.Fatal("frozen record chart axes, ordering or member projection changed")
					}
					chart, hiddenChartGroup, hiddenChartMeasure := false, false, false
					for _, s := range d.Page.Sections {
						switch s.ID {
						case "chart":
							chart = s.Mark == "bar" && s.Group == "name" && s.Measure == "avg:amount" && s.CollectionVariable == "metricWindow"
						case "hiddenChartGroup":
							hiddenChartGroup = true
						case "hiddenChartMeasure":
							hiddenChartMeasure = true
						}
					}
					if !chart || hiddenChartGroup != (member.ID == builder.ID) || hiddenChartMeasure != (member.ID == builder.ID) {
						t.Fatal("frozen chart group, measure or member projection changed")
					}
					wantGroups := 3
					if member.ID == reader.ID {
						wantGroups = 1
					}
					cards, hiddenCards := false, false
					for _, s := range d.Page.Sections {
						if s.ID == "cards" {
							cards = true
							if s.RecordList.Layout != "grid" || s.CardLabel != "name" || member.ID == reader.ID && len(s.Fields) != 1 {
								t.Fatal("frozen cards changed or hidden summary escaped")
							}
						}
						if s.ID == "hiddenCards" {
							hiddenCards = true
						}
					}
					if !cards || hiddenCards != (member.ID == builder.ID) {
						t.Fatal("card member projection changed")
					}
					if d.Page.Document.Variables["cardRecord"].Source.Section != "cards" {
						t.Fatal("card record producer changed")
					}
					head, title := false, false
					for _, s := range d.Page.Sections {
						if s.ID == "heading" {
							head = s.Text == "Frozen literal heading" && s.HeadingLevel == "h2"
						}
						if s.ID == "collectionTitle" {
							title = s.Title == "Frozen collection" && s.CountVariable == "collectionCount"
						}
					}
					if !head || !title {
						t.Fatal("frozen titles changed")
					}
					metric, hiddenMetric := false, false
					for _, section := range d.Page.Sections {
						if section.ID == "metric_amount" {
							metric = true
							if section.MetricPresentation.Suffix != "%" || section.MetricPresentation.Prefix != "" || section.Measure != "avg:amount" {
								t.Fatal("frozen metric changed")
							}
						}
						if section.ID == "metric_sensitive" {
							hiddenMetric = true
						}
					}
					if !metric || hiddenMetric != (member.ID == builder.ID) {
						t.Fatal("metric member field projection changed")
					}
					var links platform.Section
					status, privateStatus := false, false
					for _, section := range d.Page.Sections {
						if section.ID == "links" {
							links = section
						}
						if section.ID == "status" {
							status = true
							if len(section.StatusTracker.Stages) != 2 || section.StatusTracker.Stages[0] != "done" {
								t.Fatal("frozen status stages changed")
							}
						}
						if section.ID == "privateStatus" {
							privateStatus = true
						}
					}
					if !status || privateStatus != (member.ID == builder.ID) {
						t.Fatal("status tracker member projection changed")
					}
					if len(links.RecordLinks) != wantGroups || links.RecordLinks[0].Title != "Public children" {
						t.Fatal("frozen links changed or hidden groups escaped")
					}
					wantControls := 3
					wantEvents := 4
					if member.ID == reader.ID {
						wantControls = 2
						wantEvents = 3
					}
					if len(d.Page.Sections[3].Buttons) != wantControls || d.Page.Sections[3].Buttons[0].Title != "Show" || d.Page.Document.Events[1].Control != "show" || d.Page.Document.Events[2].Control != "hide" {
						t.Fatal("frozen button group changed")
					}
					if len(d.Page.Sections[2].RecordView.Tabs) != 4 || d.Page.Sections[2].RecordVariable != "active" {
						t.Fatal("frozen record view changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[2].Fields) != 1 {
						t.Fatal("record view fields were not masked")
					}
					if d.Page.Sections[1].DetailPresentation == nil || !d.Page.Sections[1].DetailPresentation.HideNull || d.Page.Sections[1].DetailPresentation.Columns != 2 {
						t.Fatal("frozen detail presentation changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[1].Fields) != 1 {
						t.Fatal("detail fields were not masked")
					}
					if d.Page.Sections[0].ShowSearch == nil || *d.Page.Sections[0].ShowSearch || d.Page.Sections[0].TableColumns[0].Title != "Frozen name" {
						t.Fatal("frozen table presentation changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[0].TableColumns) != 1 {
						t.Fatal("hidden column presentation escaped member projection")
					}
					if len(d.Page.Document.Events) != wantEvents || d.Page.Document.Events[0].Event != "select" || d.Page.Document.Events[0].Target != "shown" {
						t.Fatal("frozen selection event changed")
					}
					if d.Page.Sections[0].SelectionSetVariable != "picked" || d.Page.Document.Variables["picked"].Source.Kind != "records" {
						t.Fatal("frozen record-set port changed")
					}
					if member.ID == reader.ID && len(d.Page.Sections[0].Fields) != 1 {
						t.Fatal("member fields were not masked")
					}
				}
			}
			if !seen {
				t.Fatal("record-set page missing")
			}
		}
	}
	check(tn)
	CheckReplay(t, tn, entries, compose)
	image, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err = restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	check(restored)
}
