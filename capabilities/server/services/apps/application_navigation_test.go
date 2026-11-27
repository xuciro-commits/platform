package apps_test

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/apps"
	"platformserver/services/apps/pages"
	"platformserver/services/resources/publication"
	"testing"
)

func TestTwoLevelNavigationAndNotePageProjection(t *testing.T) {
	appDef := platform.Application{
		Name:  "opsguide",
		Title: "Ops Guide",
		Pages: []string{"orders", "audit"},
		Groups: []platform.AppGroup{
			{
				ID:    "daily",
				Title: "Daily",
				Pages: []string{"orders"},
				Groups: []platform.AppGroup{
					{
						ID:        "guides",
						Title:     "Guides",
						Collapsed: true,
						Entries:   platform.Raw([]protocol.AppMenuEntry{{Kind: "note", ID: "sop", Title: "SOP", Body: "Follow standard checklist."}}),
					},
					{
						ID:    "restricted",
						Title: "Restricted",
						Roles: []string{"manager"},
						Pages: []string{"audit"},
					},
				},
			},
		},
	}
	if err := pages.CheckApplicationGroups(appDef); err != nil {
		t.Fatalf("valid two-level groups rejected: %v", err)
	}

	tooDeep := platform.Application{
		Name:  "deep",
		Title: "Deep",
		Groups: []platform.AppGroup{
			{
				ID:    "l1",
				Title: "L1",
				Groups: []platform.AppGroup{
					{
						ID:    "l2",
						Title: "L2",
						Groups: []platform.AppGroup{
							{ID: "l3", Title: "L3", Entries: platform.Raw([]protocol.AppMenuEntry{{Kind: "note", ID: "n", Title: "N", Body: "x"}})},
						},
					},
				},
			},
		},
	}
	if err := pages.CheckApplicationGroups(tooDeep); err == nil {
		t.Fatalf("expected depth-3 group to be rejected")
	}

	reg := platform.Definition{
		Ref:         platform.AssetRef{App: "opsguide", Kind: platform.AssetApp, Name: "opsguide"},
		Source:      "package",
		Application: &appDef,
	}
	ordersRef := platform.AssetRef{App: "opsguide", Kind: platform.AssetPage, Name: "orders"}
	auditRef := platform.AssetRef{App: "opsguide", Kind: platform.AssetPage, Name: "audit"}
	pageDefs := []platform.Definition{
		{Ref: ordersRef, Page: &platform.Page{Name: "orders", Title: "Orders", Object: platform.AssetRef{Name: "opsguide.order"}}},
		{Ref: auditRef, Page: &platform.Page{Name: "audit", Title: "Audit", Object: platform.AssetRef{Name: "opsguide.audit"}}},
	}

	manager := platform.Member{ID: "m1", Roles: map[string]string{"opsguide": "manager"}}
	projManager, ok := pages.ProjectApplication(reg, publication.ApplicationProjection{
		Member: manager,
		Pages:  map[platform.AssetRef]bool{ordersRef: true, auditRef: true},
	})
	if !ok {
		t.Fatalf("manager should see application")
	}
	viewsManager := apps.Applications(append([]platform.Definition{projManager}, pageDefs...))
	if len(viewsManager) != 1 || len(viewsManager[0].Menu) != 1 || len(viewsManager[0].Menu[0].Groups) != 2 {
		t.Fatalf("manager menu mismatch: %+v", viewsManager)
	}
	if viewsManager[0].Menu[0].Groups[0].ID != "guides" || !viewsManager[0].Menu[0].Groups[0].Collapsed {
		t.Fatalf("expected guides subgroup collapsed with stable id: %+v", viewsManager[0].Menu[0].Groups[0])
	}
	if viewsManager[0].Menu[0].Groups[0].Entries[0].Kind != "note" || viewsManager[0].Menu[0].Groups[0].Entries[0].Body != "Follow standard checklist." {
		t.Fatalf("expected note entry: %+v", viewsManager[0].Menu[0].Groups[0].Entries[0])
	}

	clerk := platform.Member{ID: "c1", Roles: map[string]string{"opsguide": "clerk"}}
	projClerk, ok := pages.ProjectApplication(reg, publication.ApplicationProjection{
		Member: clerk,
		Pages:  map[platform.AssetRef]bool{ordersRef: true, auditRef: true},
	})
	if !ok {
		t.Fatalf("clerk should see application")
	}
	viewsClerk := apps.Applications(append([]platform.Definition{projClerk}, pageDefs...))
	if len(viewsClerk) != 1 || len(viewsClerk[0].Menu) != 1 || len(viewsClerk[0].Menu[0].Groups) != 1 {
		t.Fatalf("clerk should not see restricted empty subgroup: %+v", viewsClerk)
	}
	if viewsClerk[0].Menu[0].Groups[0].ID != "guides" {
		t.Fatalf("clerk remaining subgroup mismatch: %+v", viewsClerk[0].Menu[0].Groups[0])
	}
}
