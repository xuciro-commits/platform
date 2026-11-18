package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"

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
	preview, err := live.inspectPackage(t.Context(), protocol.PackageInspectRequest{Content: raw})
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
		if !exists || len(view.Menu) == 0 || !slices.ContainsFunc(view.Menu[0].Entries, func(page protocol.ApplicationPage) bool {
			return page.Name == "batchproviderorder" && page.Object == typ
		}) {
			t.Fatalf("%s application navigation: %+v", role, view)
		}
	}
	if _, exists := app("auditor"); exists {
		t.Fatal("member without access saw the app")
	}
	live.accept(t, typ+".create", typ, "ORDER", map[string]any{"number": "ORDER", "product": "sample", "quantity": 1, "unitcost": 2})
	actionsOf := func(role string) []objects.RecordAction {
		t.Helper()
		var view objects.RecordView
		raw := get(role, "/v1/records/"+typ+"/ORDER")
		if json.Unmarshal(raw.Bytes(), &view) != nil {
			t.Fatal(raw)
		}
		return view.Actions
	}
	hasAction := func(action objects.RecordAction) bool { return action.Schema == schema && action.Allowed }
	for _, role := range []string{"builder", "other"} {
		if actions := actionsOf(role); !slices.ContainsFunc(actions, hasAction) {
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
	for _, action := range actionsOf("builder") {
		if action.Schema == schema {
			t.Fatal("retired package action remained on the retained record")
		}
	}
	req := live.submission(schema, typ, "ORDER", inputs)
	if _, refusal := live.Submit(live.member(t), req, invariantClock); refusal == nil {
		t.Fatal("a retired package action still accepted direct submission")
	}
}
