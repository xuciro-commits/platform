package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"slices"
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
	preview, err := live.inspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(offer)})
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
