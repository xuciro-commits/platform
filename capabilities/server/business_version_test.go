package platformserver

import "platformserver/studio/build"

import (
	"encoding/json"
	"strings"
	"testing"

	"platformserver/platform"
)

func businessVersionSetup(t *testing.T, tn *Tenant, policy string) {
	t.Helper()
	access := []build.Access{{Role: build.User, Read: "all", Create: true, Edit: true, Archive: true}}
	publishObject(t, tn, "builder", "ROUTE", map[string]any{"name": "routing", "title": "Routing", "fields": []build.Field{
		{Name: "code", Title: "Code", Type: "text", Required: true}, {Name: "version", Title: "Business version", Type: "integer", Required: true},
		{Name: "from", Title: "Valid from", Type: "date", Required: true}, {Name: "until", Title: "Valid until", Type: "date"}, {Name: "name", Title: "Name", Type: "text"},
	}, "access": access, "versioning": platform.Versioning{Key: "code", Version: "version", ValidFrom: "from", ValidUntil: "until", Pin: policy}}, txnAt)
	publishObject(t, tn, "builder", "OPERATION", map[string]any{"name": "operation", "title": "Operation", "fields": []build.Field{{Name: "routing", Title: "Routing", Type: "reference", Ref: "build.routing", Inverse: "operations", Required: true}, {Name: "ordinal", Title: "Ordinal", Type: "integer", Required: true}}, "access": access}, txnAt)
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "ROUTE", map[string]any{"sets": []platform.RecordSet{{Name: "operations", From: "operations", Direction: "inverse", Object: "build.operation", Order: []platform.RecordSetSort{{Expr: platform.ExprReference("integer", "ordinal")}}, Limit: 100}}}, txnAt)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "ROUTE", nil, txnAt)
	publishObject(t, tn, "builder", "ORDER", map[string]any{"name": "order", "title": "Order", "fields": []build.Field{{Name: "routing", Title: "Routing", Type: "reference", Ref: "build.routing", Required: true}, {Name: "routingversion", Title: "Pinned routing version", Type: "integer"}, {Name: "note", Title: "Note", Type: "text"}}, "states": []build.State{{Name: "draft", Title: "Draft"}, {Name: "released", Title: "Released"}}, "access": access,
		"versionPins": []platform.VersionPinBinding{{Reference: "routing", Version: "routingversion"}},
		"constraints": []platform.Constraint{{Name: "fixedrouting", Kind: "immutable", Fields: []string{"routing", "routingversion"}, Message: "Released routing stays fixed", Translations: map[string]string{"zh-CN": "已下达工单的工艺必须固定"}}},
		"actions":     []build.Action{{Name: "release", Title: "Release", From: []string{"draft"}, To: "released", Roles: []string{build.User}}},
	}, txnAt)
}
func TestPinnedBusinessVersionPreservesSourceReadPermissions(t *testing.T) {
	tn := builderTenant(t, "version-permissions")
	businessVersionSetup(t, tn, "on_create")
	decide(t, tn, "builder", build.ID, "build.routing.create", "build.routing", "v1", map[string]any{"code": "R", "version": 1, "from": "2025-01-01"}, txnAt)
	decide(t, tn, "builder", build.ID, "build.order.create", "build.order", "first", map[string]any{"routing": "v1"}, txnAt)
	// Retain the same source shape while revoking its version field from users.
	draft, _ := tn.RecordOf(memberOf(t, tn, "builder"), build.ObjectType, "ROUTE", txnAt)
	var source build.Object
	_ = json.Unmarshal(platform.Raw(draft.Record), &source)
	for i := range source.Fields {
		if source.Fields[i].Name == "version" {
			source.Fields[i].Read = []string{build.Builder}
		}
	}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "ROUTE", map[string]any{"fields": source.Fields}, txnAt)
	decide(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "ROUTE", nil, txnAt)
	view, err := tn.RecordOf(memberOf(t, tn, "user"), "build.order", "first", txnAt)
	var values map[string]any
	_ = json.Unmarshal(platform.Raw(view.Record), &values)
	if err != nil || values["routingversion"] != nil || values["versionPins"] != nil {
		t.Fatalf("pin leaked revoked source value: %v %v", values, err)
	}
	state := txnState(tn)
	if got := refuse(t, tn, "user", build.ID, "build.order.create", "build.order", "unreadable", map[string]any{"routing": "v1"}, txnAt); !strings.Contains(got, "not readable") {
		t.Fatal(got)
	}
	if txnState(tn) != state {
		t.Fatal("unreadable version pin changed business state")
	}
}

func TestBusinessVersionOnCreateAndPublicationBoundaries(t *testing.T) {
	tn := builderTenant(t, "creation-versions")
	businessVersionSetup(t, tn, "on_create")
	decide(t, tn, "user", build.ID, "build.routing.create", "build.routing", "v1", map[string]any{"code": "R", "version": 1, "from": "2025-01-01"}, txnAt)
	decide(t, tn, "user", build.ID, "build.order.create", "build.order", "first", map[string]any{"routing": "v1"}, txnAt)
	view, _ := tn.RecordOf(memberOf(t, tn, "user"), "build.order", "first", txnAt)
	var values map[string]any
	_ = json.Unmarshal(platform.Raw(view.Record), &values)
	if values["routingversion"] != float64(1) {
		t.Fatal("on-create did not pin")
	}
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "ROUTE", map[string]any{"versioning": nil}, txnAt)
	if got := refuse(t, tn, "builder", build.ID, "build.object.publish", build.ObjectType, "ROUTE", nil, txnAt); !strings.Contains(got, "retained") {
		t.Fatal(got)
	}
}
