package platformserver

import (
	"strings"
	"testing"

	"platformserver/apps/files"
	"platformserver/apps/relations"
	"platformserver/platform"
)

func TestCollaborationInstallChecksActualRecordOwner(t *testing.T) {
	for _, widget := range []string{"record-comments", "record-uploader", "media-preview", "pdf-viewer", "timeline", "breadcrumb"} {
		t.Run(widget, func(t *testing.T) {
			owner := relations.New("collaboration-owner")
			tenant, err := NewTenant("collaboration-owner", NewConsole("collaboration-owner"), owner, files.New("collaboration-owner"))
			if err != nil {
				t.Fatal(err)
			}
			makePage := func(app string) platform.Page {
				object := platform.AssetRef{App: app, Kind: platform.AssetObject, Name: relations.CommentType}
				section := platform.Section{ID: "collaboration", Widget: widget, ConfigVersion: 1, Object: object, RecordVariable: "active"}
				switch widget {
				case "record-comments":
					section.CommentDraftVariable = "draft"
				case "record-uploader", "media-preview":
					section.FileVariable = "file"
				case "pdf-viewer":
					section.FileVariable = "file"
					section.PdfPageVariable = "page"
				case "timeline":
					section.HistoryLimit = 18
				case "breadcrumb":
					section.Breadcrumb = &platform.PageBreadcrumb{HomeLabel: "Home", PageLabel: "Comments", LabelField: "text"}
				}
				page := platform.Page{Name: "collaboration-owner", Object: object, Layout: "composed", Sections: []platform.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Object: object, Fields: []string{"text"}}, section}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "collaboration"}}, "table": {Kind: "widget", Section: "table"}, "collaboration": {Kind: "widget", Section: "collaboration"}}, Variables: map[string]platform.PageVariable{
					"active": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}},
					"draft":  {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("")}, "file": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("")}, "page": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("1")},
				}}}
				if widget == "breadcrumb" {
					page.Document.Events = []platform.PageEventBinding{{Source: "collaboration", Event: "click", Control: "home", Effects: []platform.PageEffect{{Kind: "navigate", Navigate: &platform.PageNavigation{Page: platform.AssetRef{App: relations.ID, Kind: platform.AssetPage, Name: page.Name}}}}}}
				}
				return page
			}
			// Both refs agree and the record type exists, but its real app is relations.
			bad := makePage("platform")
			if err := bad.CheckRecordPorts(); err != nil {
				t.Fatal("fixture must reach actual-owner validation", err)
			}
			if err := tenant.InstallPage(owner, bad); err == nil || !strings.Contains(err.Error(), "actual entity owner") {
				t.Fatalf("matching false owner accepted: %v", err)
			}
			if err := tenant.InstallPage(owner, makePage(relations.ID)); err != nil {
				t.Fatal("actual aliased service owner rejected", err)
			}
		})
	}
}

func TestAvatarInstallRetainsOriginalOwnerAndQueryBudget(t *testing.T) {
	owner := relations.New("avatar-owner")
	tenant, err := NewTenant("avatar-owner", NewConsole("avatar-owner"), owner)
	if err != nil {
		t.Fatal(err)
	}
	view := hostView{t: tenant, app: owner}
	query := platform.NamedQuery{Name: "all-comments", Title: "Original comment records", Object: relations.CommentType, Sort: []string{"id"}, Limit: 6}
	if err := view.InstallQuery(platform.Caller{}, query, 1); err != nil {
		t.Fatal(err)
	}
	object := platform.AssetRef{App: relations.ID, Kind: platform.AssetObject, Name: relations.CommentType}
	page := platform.Page{Name: "avatars", Object: object, Layout: "composed", Sections: []platform.Section{{ID: "avatars", Widget: "avatar-stack", ConfigVersion: 1, CollectionVariable: "all", Avatar: &platform.PageAvatarStack{LabelField: "text"}}}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"avatars"}}, "avatars": {Kind: "widget", Section: "avatars"}}, Variables: map[string]platform.PageVariable{"all": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "all"}}}, Queries: map[string]platform.PageQuery{"all": {Object: object, Query: &platform.AssetBinding{Ref: platform.AssetRef{App: relations.ID, Kind: platform.AssetQuery, Name: query.Name}, SourceVersion: owner.Manifest().Version + ".query-1"}, Limit: 6, Sort: []string{"id"}}}}}
	if err := tenant.InstallPage(owner, page); err != nil {
		t.Fatal("actual original aliased owner rejected", err)
	}
	query.Limit = 5
	if err := view.InstallQuery(platform.Caller{}, query, 2); err != nil {
		t.Fatal(err)
	}
	if err := tenant.InstallPage(owner, page); err != nil {
		t.Fatal("latest named query changed original retained avatar", err)
	}
	q := page.Document.Queries["all"]
	q.Query.SourceVersion = owner.Manifest().Version + ".query-2"
	page.Document.Queries["all"] = q
	if err := tenant.InstallPage(owner, page); err == nil || !strings.Contains(err.Error(), "avatar requires") {
		t.Fatalf("five-item original query silently truncated fixed avatar window: %v", err)
	}
	q.Query.SourceVersion = owner.Manifest().Version + ".query-1"
	q.Object.App = "platform"
	page.Document.Queries["all"] = q
	page.Object.App = "platform"
	if err := tenant.InstallPage(owner, page); err == nil {
		t.Fatal("matching false original owner accepted")
	}
}
