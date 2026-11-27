package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK24InstalledPackageContributesOnlyActiveAuthorizedNavigationAndActions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k24",
		protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin"}}},
		protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}},
		protocol.Seat{Subjects: []string{"auditor"}, Member: platform.Member{ID: "auditor"}},
	)
	const name, typ, schema = "productionprovider", "build.batchproviderorder", "build.batchproviderorder.start"
	raw, err := os.ReadFile("../../../deploy/packages/production-provider.json")
	if err != nil {
		t.Fatal(err)
	}
	var offer platform.PackageDescriptor
	if err := json.Unmarshal(raw, &offer); err != nil {
		t.Fatal(err)
	}
	for i, asset := range offer.Blueprint.Assets {
		if asset.Ref.Name == "build.stockreceipt" {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(asset.Body, &object); err != nil {
				t.Fatal(err)
			}
			object["access"] = platform.Raw([]ontology.Access{{Role: ontology.UserRole, Read: "none"}})
			offer.Blueprint.Assets[i].Body = platform.Raw(object)
		}
		if asset.Ref.Kind == platform.AssetApp && asset.Ref.Name == name {
			var body map[string]json.RawMessage
			var pages []string
			if json.Unmarshal(asset.Body, &body) != nil || json.Unmarshal(body["pages"], &pages) != nil {
				t.Fatal("application body")
			}
			body["groups"] = platform.Raw([]platform.AppGroup{{Title: "Orders", Layout: "settings", Pages: pages}})
			offer.Blueprint.Assets[i].Body = platform.Raw(body)
		}
	}
	preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(offer)})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := preview.Descriptor
	admin, _ := live.Member("admin")
	submit := func(schema string, body any) {
		t.Helper()
		request := live.submission(schema, protocol.PackageType, descriptor.ID, body)
		request.Authority, request.PrincipalId = "platform", admin.ID
		if _, refusal := live.Submit(admin, request, invariantClock); refusal != nil {
			t.Fatalf("%s: %s %s", schema, refusal.Code, refusal.Message)
		}
	}
	_, handler := observationHost(live.Tenant)
	get := func(role, path string) *bytes.Buffer {
		t.Helper()
		res := observationRequest(t, handler, live.ID, observationToken(live.ID, role), path, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", role, path, res.Code, res.Body)
		}
		return res.Body
	}
	app := func(role string) (protocol.ApplicationView, bool) {
		t.Helper()
		var views []protocol.ApplicationView
		raw := get(role, "/v1/applications")
		if json.Unmarshal(raw.Bytes(), &views) != nil {
			t.Fatal(raw)
		}
		index := slices.IndexFunc(views, func(view protocol.ApplicationView) bool { return view.Name == name })
		if index < 0 {
			return protocol.ApplicationView{}, false
		}
		return views[index], true
	}
	if _, exists := app("builder"); exists {
		t.Fatal("uninstalled app appeared in the shell")
	}
	submit(packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": preview.Digest})
	submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version})
	for _, role := range []string{"builder", "other"} {
		view, exists := app(role)
		if !exists || len(view.Menu) != 1 || !slices.ContainsFunc(view.Menu[0].Entries, func(page protocol.ApplicationPage) bool {
			return page.Name == "batchproviderorder" && page.Object == typ
		}) {
			t.Fatalf("%s application navigation: %+v", role, view)
		}
		if view.Menu[0].Title != "Orders" || view.Menu[0].Layout != "settings" {
			t.Fatalf("%s menu group keeps its title and layout: %+v", role, view.Menu[0])
		}
		hasReceipt := slices.ContainsFunc(view.Menu[0].Entries, func(page protocol.ApplicationPage) bool { return page.Name == "stockreceipt" })
		if hasReceipt != (role == "builder") {
			t.Fatalf("%s stock receipt page visibility: %+v", role, view.Menu)
		}
	}
	if _, exists := app("auditor"); exists {
		t.Fatal("member without access saw the app")
	}
	live.accept(t, typ+".create", typ, "ORDER", map[string]any{"number": "ORDER", "product": "sample", "quantity": 1, "unitcost": 2})
	recordOf := func(role string) objects.RecordView {
		t.Helper()
		var view objects.RecordView
		raw := get(role, "/v1/records/"+typ+"/ORDER")
		if json.Unmarshal(raw.Bytes(), &view) != nil {
			t.Fatal(raw)
		}
		return view
	}
	hasAction := func(action objects.RecordAction) bool { return action.Schema == schema && action.Allowed }
	for _, role := range []string{"builder", "other"} {
		if actions := recordOf(role).Actions; !slices.ContainsFunc(actions, hasAction) {
			t.Fatalf("%s actions: %+v", role, actions)
		}
	}
	inputs := map[string]any{"shoporder": "SHOP-1"}
	live.accept(t, schema, typ, "ORDER", inputs)
	submit(packages.SchemaPackageDrain, map[string]any{"id": descriptor.ID})
	submit(packages.SchemaPackageRetire, map[string]any{"id": descriptor.ID})
	if _, exists := app("builder"); exists {
		t.Fatal("retired application remained in the shell")
	}
	linkPath := "/v1/applications/" + name + "/batchproviderorder/records/ORDER"
	for _, role := range []string{"builder", "other"} {
		var link protocol.RetainedRecordLink
		if raw := get(role, linkPath); json.Unmarshal(raw.Bytes(), &link) != nil || link.Object != typ {
			t.Fatalf("%s retained record link: %s", role, raw)
		}
		view := recordOf(role)
		if record, ok := view.Record.(map[string]any); !ok || record["number"] != "ORDER" || record["firstshop"] != "SHOP-1" {
			t.Fatalf("%s retained record: %+v", role, view.Record)
		}
		if slices.ContainsFunc(view.Actions, func(action objects.RecordAction) bool { return action.Schema == schema }) {
			t.Fatalf("%s saw the retired action on the retained record", role)
		}
	}
	if res := observationRequest(t, handler, live.ID, observationToken(live.ID, "auditor"), linkPath, nil); res.Code != http.StatusNotFound {
		t.Fatalf("member without access resolved the retired link: %d %s", res.Code, res.Body)
	}
	req := live.submission(schema, typ, "ORDER", inputs)
	if _, refusal := live.Submit(live.member(t), req, invariantClock); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED || refusal.Message != "Authorization denied at verb: The package contribution is retired" {
		t.Fatalf("retired package action direct submission: %+v", refusal)
	}
}

func TestInvariantK24PackageDemoSeedInstallAndClear(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k24-demo",
		protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: ontology.BuilderRole}}},
	)
	const name, typ = "productionprovider", "build.batchproviderorder"
	raw, err := os.ReadFile("../../../deploy/packages/production-provider.json")
	if err != nil {
		t.Fatal(err)
	}
	var offer platform.PackageDescriptor
	if err := json.Unmarshal(raw, &offer); err != nil {
		t.Fatal(err)
	}
	offer.Blueprint.Seed = []json.RawMessage{
		platform.Raw(map[string]any{"type": typ, "id": "demo-order-1", "values": map[string]any{"number": "DEMO-1", "product": "sample", "quantity": 2, "unitcost": 5},
			"then": []map[string]any{{"action": "edit", "with": map[string]any{"product": "sample after an action"}}}}),
	}
	preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(offer)})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := preview.Descriptor
	admin, _ := live.Member("admin")
	submit := func(schema string, body any) *platform.Error {
		t.Helper()
		request := live.submission(schema, protocol.PackageType, descriptor.ID, body)
		request.Authority, request.PrincipalId = "platform", admin.ID
		_, refusal := live.Submit(admin, request, invariantClock)
		if refusal == nil {
			live.Work(invariantClock)
		}
		return refusal
	}
	_, handler := observationHost(live.Tenant)
	get := func(path string) *bytes.Buffer {
		t.Helper()
		res := observationRequest(t, handler, live.ID, observationToken(live.ID, "admin"), path, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.Code, res.Body)
		}
		return res.Body
	}
	appView := func() protocol.ApplicationView {
		t.Helper()
		var views []protocol.ApplicationView
		if err := json.Unmarshal(get("/v1/applications").Bytes(), &views); err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(views, func(view protocol.ApplicationView) bool { return view.Name == name })
		if index < 0 {
			t.Fatalf("missing application %s", name)
		}
		return views[index]
	}
	recordIDs := func() []string {
		t.Helper()
		var page struct {
			Records []struct {
				ID string `json:"id"`
			} `json:"records"`
		}
		if err := json.Unmarshal(get("/v1/records/"+typ).Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(page.Records))
		for _, r := range page.Records {
			ids = append(ids, r.ID)
		}
		return ids
	}
	if refusal := submit(packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": preview.Digest}); refusal != nil {
		t.Fatalf("import: %v", refusal)
	}
	if refusal := submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version}); refusal != nil {
		t.Fatalf("install: %v", refusal)
	}
	if view := appView(); view.Package != descriptor.ID || !view.HasDemo {
		t.Fatalf("expected active demo data on app view, got %+v", view)
	}
	if ids := recordIDs(); !slices.Equal(ids, []string{"demo-order-1"}) {
		t.Fatalf("expected seeded demo record, got %v", ids)
	}
	if view, refusal := live.RecordOf(t.Context(), admin, typ, "demo-order-1", invariantClock); refusal != nil || !strings.Contains(string(platform.Raw(view.Record)), "sample after an action") {
		t.Fatalf("a demo record's then steps run after it is created, got %v %v", view.Record, refusal)
	}
	live.accept(t, typ+".create", typ, "user-order-1", map[string]any{"number": "USER-1", "product": "real", "quantity": 1, "unitcost": 10})
	offer.Version = "0.1.6"
	offer.Blueprint.Seed = []json.RawMessage{
		platform.Raw(map[string]any{"type": typ, "id": "user-order-1", "values": map[string]any{"number": "WRONG", "product": "collision", "quantity": 1, "unitcost": 1}}),
	}
	upgrade, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(offer)})
	if err != nil {
		t.Fatal(err)
	}
	if refusal := submit(packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": upgrade.Digest}); refusal != nil {
		t.Fatalf("upgrade import: %v", refusal)
	}
	if refusal := submit(packages.SchemaPackageUpgrade, map[string]any{"id": descriptor.ID, "version": offer.Version}); refusal != nil {
		t.Fatalf("upgrade: %v", refusal)
	}
	if ids := recordIDs(); !slices.Equal(ids, []string{"demo-order-1", "user-order-1"}) {
		t.Fatalf("upgrade must not seed new version, got %v", ids)
	}
	if refusal := submit(packages.SchemaPackageClearDemo, map[string]any{"id": descriptor.ID}); refusal != nil {
		t.Fatalf("clear-demo: %v", refusal)
	}
	if view := appView(); view.Package != descriptor.ID || view.HasDemo {
		t.Fatalf("expected hasDemo=false after clearing, got %+v", view)
	}
	if ids := recordIDs(); !slices.Equal(ids, []string{"user-order-1"}) {
		t.Fatalf("expected only user record after clear-demo, got %v", ids)
	}
	if refusal := submit(packages.SchemaPackageClearDemo, map[string]any{"id": descriptor.ID}); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("second clear-demo should be refused, got %v", refusal)
	}
	if refusal := submit(packages.SchemaPackageDrain, map[string]any{"id": descriptor.ID}); refusal != nil {
		t.Fatalf("drain: %v", refusal)
	}
	if refusal := submit(packages.SchemaPackageRetire, map[string]any{"id": descriptor.ID}); refusal != nil {
		t.Fatalf("retire: %v", refusal)
	}
	offer.Version = "0.1.7"
	clashing, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(offer)})
	if err != nil {
		t.Fatal(err)
	}
	if refusal := submit(packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": clashing.Digest}); refusal != nil {
		t.Fatalf("clashing import: %v", refusal)
	}
	if refusal := submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": offer.Version}); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("install with existing user record as seed must be rejected, got %v", refusal)
	}
}

func TestInvariantMultiPackageTranslationsScopeByNamespaceAndTranslateFieldGroupsAndLabels(t *testing.T) {
	builder := newInvariantTenant(t, "invariant-multi-pkg-source")
	contact := invariantObject("contact", "Contact")
	contact["fields"] = []ontology.Field{{Name: "fullname", Title: "Full name", Type: "text"}, {Name: "jobtitle", Title: "Job title", Type: "text"}}
	contact["access"] = []ontology.Access{{Role: "steward", Read: "all", Create: true, Edit: true}}
	builder.accept(t, "build.object.create", build.ObjectType, "CONTACT", contact)
	builder.accept(t, "build.app.create", protocol.AppType, "PARTYAPP", map[string]any{
		"name":      "partyapp",
		"title":     "Business partners",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.contact"}},
		"languages": platform.Languages{"zh-CN": {"Full name": "全名", "Job title": "职位"}},
	})
	partyCID := invariantPublishData(t, builder, builder.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "CONTACT"},
		{Kind: platform.AssetApp, ID: "PARTYAPP"},
	}, "multi-i18n-party")
	partyCand, err := PublicationResolver().ReadCandidate(partyCID, builder.releases.Raw(partyCID))
	if err != nil {
		t.Fatal(err)
	}
	partyDesc, err := packages.Rebind(platform.PackageDescriptor{
		ID: "party", Version: "0.2.2", Namespace: "party", Title: "Business partners",
		Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "partyapp"}}, Assets: partyCand.Assets},
	}, "party")
	if err != nil {
		t.Fatal(err)
	}

	crmBuilder := newInvariantTenant(t, "invariant-multi-pkg-crm-source")
	lead := invariantObject("lead", "Lead")
	lead["fields"] = []ontology.Field{{Name: "fullname", Title: "Full name", Type: "text"}, {Name: "jobtitle", Title: "Job title", Type: "text"}}
	lead["access"] = []ontology.Access{{Role: "salesmanager", Read: "all", Create: true, Edit: true}}
	deal := invariantObject("deal", "Deal")
	deal["fields"] = []ontology.Field{
		{Name: "weighted", Title: "Weighted amount", Type: "decimal", Group: "Forecast"},
		{Name: "nextstep", Title: "Next step", Type: "text", Group: "Next step"},
	}
	deal["access"] = []ontology.Access{{Role: "salesmanager", Read: "all", Create: true, Edit: true}}
	crmBuilder.accept(t, "build.object.create", build.ObjectType, "LEAD", lead)
	crmBuilder.accept(t, "build.object.create", build.ObjectType, "DEAL", deal)
	crmBuilder.accept(t, "build.actiontype.create", "build.actiontype", "CONVERT", map[string]any{
		"name":   "invariantproject.lead.convert",
		"object": "invariantproject.lead",
		"definition": map[string]any{
			"name":  "convert",
			"title": "Convert to deal",
			"roles": []string{"salesmanager"},
			"inputs": []ontology.Input{
				{Name: "account", Title: "Existing account", Type: "text"},
			},
			"transaction": map[string]any{
				"subject": "invariantproject.lead",
				"steps": []map[string]any{{
					"name":    "update",
					"kind":    "update",
					"target":  map[string]any{"type": "invariantproject.lead", "id": "$subject"},
					"changes": []map[string]any{{"field": "jobtitle", "expr": map[string]any{"kind": "text", "ref": "input.account"}}},
				}},
			},
		},
	})
	crmBuilder.accept(t, "build.app.create", protocol.AppType, "CRMAPP", map[string]any{
		"name":  "crmapp",
		"title": "CRM",
		"resources": []platform.AssetRef{
			{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.lead"},
			{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.deal"},
			{App: build.ID, Kind: platform.AssetAction, Name: "invariantproject.lead.convert"},
		},
		"languages": platform.Languages{"zh-CN": {
			"Full name":        "姓名",
			"Job title":        "职务",
			"Forecast":         "预测",
			"Next step":        "下一步",
			"Existing account": "已有客户",
		}},
	})
	crmCID := invariantPublishData(t, crmBuilder, crmBuilder.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "LEAD"},
		{Kind: platform.AssetObject, ID: "DEAL"},
		{Kind: platform.AssetAction, ID: "CONVERT"},
		{Kind: platform.AssetApp, ID: "CRMAPP"},
	}, "multi-i18n-crm")
	crmCand, err := PublicationResolver().ReadCandidate(crmCID, crmBuilder.releases.Raw(crmCID))
	if err != nil {
		t.Fatal(err)
	}
	crmDesc, err := packages.Rebind(platform.PackageDescriptor{
		ID: "crm", Version: "0.3.0", Namespace: "crm", Title: "CRM",
		Blueprint: &platform.PackageBlueprint{Roots: []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "crmapp"}}, Assets: crmCand.Assets},
	}, "crm")
	if err != nil {
		t.Fatal(err)
	}

	adminSeat := protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: ontology.BuilderRole, "crm": "salesmanager", "party": "steward"}}}
	live := newInvariantTenant(t, "invariant-multi-pkg-i18n", adminSeat)
	admin, _ := live.Member("admin")
	for _, offer := range []platform.PackageDescriptor{partyDesc, crmDesc} {
		preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(offer)})
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []struct {
			schema string
			body   map[string]any
		}{
			{packages.SchemaPackageImport, map[string]any{"id": preview.Descriptor.ID, "digest": preview.Digest}},
			{packages.SchemaPackageInstall, map[string]any{"id": preview.Descriptor.ID, "version": preview.Descriptor.Version}},
		} {
			req := live.submission(step.schema, protocol.PackageType, preview.Descriptor.ID, step.body)
			req.Authority, req.PrincipalId = "platform", admin.ID
			if _, refusal := live.Submit(admin, req, invariantClock); refusal != nil {
				t.Fatalf("%s %s: %s %s", offer.ID, step.schema, refusal.Code, refusal.Message)
			}
		}
	}
	_, handler := observationHost(live.Tenant)
	getZh := func(path string) []byte {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "admin"))
		req.Header.Set("Platform-Tenant", live.ID)
		req.Header.Set("Accept-Language", "zh-CN")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		return rec.Body.Bytes()
	}
	var entities []platform.RecordTypeInfo
	if err := json.Unmarshal(getZh("/v1/entities"), &entities); err != nil {
		t.Fatal(err)
	}
	fieldOf := func(typ, name string) platform.FieldInfo {
		t.Helper()
		idx := slices.IndexFunc(entities, func(e platform.RecordTypeInfo) bool { return e.Type == typ })
		if idx < 0 {
			t.Fatalf("missing entity %s", typ)
		}
		f, ok := entities[idx].Field(name)
		if !ok {
			t.Fatalf("missing field %s.%s", typ, name)
		}
		return f
	}
	if got := fieldOf("party.contact", "fullname").Title; got != "全名" {
		t.Fatalf("party.contact.fullname expected 全名, got %q", got)
	}
	if got := fieldOf("crm.lead", "fullname").Title; got != "姓名" {
		t.Fatalf("crm.lead.fullname expected 姓名, got %q", got)
	}
	if got := fieldOf("crm.lead", "jobtitle").Title; got != "职务" {
		t.Fatalf("crm.lead.jobtitle expected 职务, got %q", got)
	}
	if got := fieldOf("party.contact", "jobtitle").Title; got != "职位" {
		t.Fatalf("party.contact.jobtitle expected 职位, got %q", got)
	}
	if got := fieldOf("crm.deal", "weighted").Group; got != "预测" {
		t.Fatalf("crm.deal.weighted group expected 预测, got %q", got)
	}
	if got := fieldOf("crm.deal", "nextstep").Group; got != "下一步" {
		t.Fatalf("crm.deal.nextstep group expected 下一步, got %q", got)
	}
	var actions []platform.Action
	if err := json.Unmarshal(getZh("/v1/actions"), &actions); err != nil {
		t.Fatal(err)
	}
	convIdx := slices.IndexFunc(actions, func(a platform.Action) bool { return a.Schema == "crm.lead.convert" })
	if convIdx < 0 {
		t.Fatal("missing action crm.lead.convert")
	}
	paramIdx := slices.IndexFunc(actions[convIdx].Payload, func(p platform.Field) bool { return p.Name == "account" })
	if paramIdx < 0 || actions[convIdx].Payload[paramIdx].Label != "已有客户" {
		t.Fatalf("crm.lead.convert account label expected 已有客户, got %+v", actions[convIdx].Payload)
	}
}

func TestInvariantK24PackageReferenceReadiness(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k24-readiness",
		protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: ontology.BuilderRole}}},
		protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}},
	)
	raw, err := os.ReadFile("../../../scripts/frontend-fixtures/party-0.1.0.json")
	if err != nil {
		t.Fatal(err)
	}
	var offer platform.PackageDescriptor
	if err := json.Unmarshal(raw, &offer); err != nil {
		t.Fatal(err)
	}
	preview, err := live.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(offer)})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := preview.Descriptor
	admin, _ := live.Member("admin")
	submit := func(schema string, body any) {
		t.Helper()
		request := live.submission(schema, protocol.PackageType, descriptor.ID, body)
		request.Authority, request.PrincipalId = "platform", admin.ID
		if _, refusal := live.Submit(admin, request, invariantClock); refusal != nil {
			t.Fatalf("%s: %+v", schema, refusal)
		}
	}
	submit(packages.SchemaPackageImport, map[string]any{"id": descriptor.ID, "digest": preview.Digest})
	submit(packages.SchemaPackageInstall, map[string]any{"id": descriptor.ID, "version": descriptor.Version, "namespace": "readyparty"})
	_, handler := observationHost(live.Tenant)
	path := "/v1/packages/" + descriptor.ID + "/readiness?version=" + descriptor.Version
	readiness := func(want packages.PackageReadiness) {
		t.Helper()
		res := observationRequest(t, handler, live.ID, observationToken(live.ID, "admin"), path, nil)
		var actual packages.PackageReadiness
		if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &actual) != nil || actual != want {
			t.Fatalf("readiness: %d %s; want %+v", res.Code, res.Body, want)
		}
	}
	readiness(packages.PackageReadiness{Pending: 20})
	if res := observationRequest(t, handler, live.ID, observationToken(live.ID, "other"), path, nil); res.Code != http.StatusForbidden {
		t.Fatalf("non-admin package readiness: %d %s", res.Code, res.Body)
	}
	live.Work(invariantClock)
	readiness(packages.PackageReadiness{Ready: true})
}
