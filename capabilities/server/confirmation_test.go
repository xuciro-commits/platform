package platformserver

import "platformserver/studio/build"

import (
	"fmt"
	"slices"

	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/apps/work"
	"platformserver/platform"
)

func businessConfirmationSpec(partial, expiry string) *platform.Confirmation {
	return &platform.Confirmation{
		Parties: []platform.ConfirmationParty{
			{Name: "erp", Title: "ERP", Writeback: "erp", CompensateWriteback: "erp-reverse"},
			{Name: "wms", Title: "WMS", Writeback: "wms", CompensateWriteback: "wms-reverse", Reconcile: &platform.ConfirmationRead{Source: "SOURCE", KeyField: "id", ResultField: "state", Expected: json.RawMessage(`"confirmed"`), EverySeconds: 1}},
		}, DeadlineSeconds: 2, OnPartial: partial, OnExpire: expiry, OnComplete: "complete", OnPartialAction: "partial", ReviewerRole: "reviewer",
	}
}

func confirmationTenant(t *testing.T, id string, extra ...platform.App) *Tenant {
	t.Helper()
	apps := append(slices.Clone(extra), build.New(id), work.New(id))
	return composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user", "stock:keeper"), seatOf("reviewer", "build:reviewer")}, apps...)
}

func prepareBusinessConfirmation(t *testing.T, tn *Tenant, at time.Time, spec *platform.Confirmation) {
	t.Helper()
	start := build.Action{Name: "start", Title: "Start receipt", From: []string{"open"}, Roles: []string{build.User, build.Builder}, Inputs: []build.Input{{Name: "item", Title: "Item", Type: "text"}}, Transaction: &platform.BusinessTxn{Subject: "build.receipt", Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.receipt", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", From: "=waiting"}}}}}}
	actions := []build.Action{start, {Name: "complete", Title: "Complete receipt", From: []string{"open"}, Sets: []build.Set{{Field: "status", From: "=complete"}}}, {Name: "partial", Title: "Partial receipt", From: []string{"open"}, Sets: []build.Set{{Field: "status", From: "=partial"}}}}
	publishObject(t, tn, "builder", "O", map[string]any{"name": "receipt", "title": "Receipt", "fields": []build.Field{{Name: "status", Title: "Status", Type: "text"}}, "states": []build.State{{Name: "open", Title: "Open"}}, "access": []build.Access{{Role: build.User, Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all", Edit: true}}, "actions": actions}, at)
	for _, id := range []string{"erp", "wms"} {
		decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, id, map[string]any{"name": id, "title": id, "kind": "http", "address": "https://" + id + ".example/"}, at)
		decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, id, map[string]any{}, at)
	}
	tn.CheckConnections(at)
	for _, id := range []string{"erp", "wms"} {
		for _, reverse := range []bool{false, true} {
			name, path := id, id
			if reverse {
				name, path = id+"-reverse", id+"/reverse"
			}
			decide(t, tn, "builder", build.ID, "build.writeback.create", build.WritebackType, name, map[string]any{"name": strings.ReplaceAll(name, "-", ""), "title": name, "connection": id, "object": "build.receipt", "on": "confirmation", "path": path}, at)
			decide(t, tn, "builder", build.ID, "build.writeback.publish", build.WritebackType, name, map[string]any{}, at)
		}
	}
	decide(t, tn, "builder", build.ID, "build.source.create", build.SourceType, "SOURCE", map[string]any{"name": "receiptsource", "title": "External receipt status", "connection": "wms", "profile": "json", "url": "rows", "object": "build.receipt", "key": "id", "mapping": []build.SourceField{{From: "state", To: "status"}}}, at)
	decide(t, tn, "builder", build.ID, "build.source.publish", build.SourceType, "SOURCE", map[string]any{}, at)
	actions[0].Transaction.Confirmation = spec
	decide(t, tn, "builder", build.ID, "build.object.edit", build.ObjectType, "O", map[string]any{"actions": actions}, at)
	decide(t, tn, "builder", build.ID, build.SchemaPublish, build.ObjectType, "O", map[string]any{}, at)
}

func confirmationResponse(code int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: code, Status: fmt.Sprintf("%d answer", code), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func currentConfirmation(t *testing.T, tn *Tenant) platform.ConfirmationRun {
	t.Helper()
	runs, _, err := platform.Find[platform.ConfirmationRun](tn.automation(build.ID, false), platform.Query{})
	if err != nil || len(runs) != 1 {
		t.Fatalf("confirmation count=%d: %v", len(runs), err)
	}
	return runs[0]
}
