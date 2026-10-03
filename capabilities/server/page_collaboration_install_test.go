package platformserver

import (
	"strings"
	"testing"

	"platformserver/apps/files"
	"platformserver/apps/relations"
	"platformserver/platform"
)

func TestCollaborationInstallChecksActualRecordOwner(t *testing.T) {
	for _, widget := range []string{"record-comments", "record-uploader", "media-preview", "pdf-viewer", "timeline"} {
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
				}
				return platform.Page{Name: "collaboration-owner", Object: object, Layout: "composed", Sections: []platform.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Object: object, Fields: []string{"text"}}, section}, Document: &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "collaboration"}}, "table": {Kind: "widget", Section: "table"}, "collaboration": {Kind: "widget", Section: "collaboration"}}, Variables: map[string]platform.PageVariable{
					"active": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}},
					"draft":  {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("")}, "file": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("")}, "page": {Scope: "page", Type: "string", Mode: "state", Initial: platform.Raw("1")},
				}}}
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
