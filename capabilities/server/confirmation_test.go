package platformserver

import "platformserver/internal/construction"

import (
	"fmt"
	"slices"

	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
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
	return composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:user", "stock:keeper"), seatOf("reviewer", "build:reviewer")}, apps...)
}

func prepareBusinessConfirmation(t *testing.T, tn *Tenant, at time.Time, spec *platform.Confirmation) {
	t.Helper()
	start := construction.Action{Name: "start", Title: "Start receipt", From: []string{"open"}, Roles: []string{construction.User, construction.Builder}, Inputs: []construction.Input{{Name: "item", Title: "Item", Type: "text"}}, Transaction: &platform.BusinessTxn{Subject: "build.receipt", Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.receipt", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", From: "=waiting"}}}}}}
	actions := []construction.Action{start, {Name: "complete", Title: "Complete receipt", From: []string{"open"}, Sets: []construction.Set{{Field: "status", From: "=complete"}}}, {Name: "partial", Title: "Partial receipt", From: []string{"open"}, Sets: []construction.Set{{Field: "status", From: "=partial"}}}}
	publishObject(t, tn, "builder", "O", map[string]any{"name": "receipt", "title": "Receipt", "fields": []construction.Field{{Name: "status", Title: "Status", Type: "text"}}, "states": []construction.State{{Name: "open", Title: "Open"}}, "access": []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true}, {Role: "reviewer", Read: "all", Edit: true}}, "actions": actions}, at)
	for _, id := range []string{"erp", "wms"} {
		decide(t, tn, "builder", construction.ID, "build.connection.create", construction.ConnectionType, id, map[string]any{"name": id, "title": id, "kind": "http", "address": "https://" + id + ".example/"}, at)
		decide(t, tn, "builder", construction.ID, "build.connection.check", construction.ConnectionType, id, map[string]any{}, at)
	}
	tn.CheckConnections(at)
	for _, id := range []string{"erp", "wms"} {
		for _, reverse := range []bool{false, true} {
			name, path := id, id
			if reverse {
				name, path = id+"-reverse", id+"/reverse"
			}
			decide(t, tn, "builder", construction.ID, "build.writeback.create", construction.WritebackType, name, map[string]any{"name": strings.ReplaceAll(name, "-", ""), "title": name, "connection": id, "object": "build.receipt", "on": "confirmation", "path": path}, at)
			decide(t, tn, "builder", construction.ID, "build.writeback.publish", construction.WritebackType, name, map[string]any{}, at)
		}
	}
	decide(t, tn, "builder", construction.ID, "build.source.create", construction.SourceType, "SOURCE", map[string]any{"name": "receiptsource", "title": "External receipt status", "connection": "wms", "profile": "json", "url": "rows", "object": "build.receipt", "key": "id", "mapping": []construction.SourceField{{From: "state", To: "status"}}}, at)
	decide(t, tn, "builder", construction.ID, "build.source.publish", construction.SourceType, "SOURCE", map[string]any{}, at)
	actions[0].Transaction.Confirmation = spec
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "O", map[string]any{"actions": actions}, at)
	decide(t, tn, "builder", construction.ID, construction.SchemaPublish, construction.ObjectType, "O", map[string]any{}, at)
}

func confirmationResponse(code int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: code, Status: fmt.Sprintf("%d answer", code), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func currentConfirmation(t *testing.T, tn *Tenant) platform.ConfirmationRun {
	t.Helper()
	runs, _, err := platform.Find[platform.ConfirmationRun](tn.automation(construction.ID, false), platform.Query{})
	if err != nil || len(runs) != 1 {
		t.Fatalf("confirmation count=%d: %v", len(runs), err)
	}
	return runs[0]
}
