package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
	controlowner "platformserver/kernel/control"
	"strings"
	"testing"

	"platformserver/apps/core"
	"platformserver/apps/work"
	"platformserver/studio/build"

	"platformserver/platform"
)

func writebackConfigurationTenant(t *testing.T, id string) *Tenant {
	t.Helper()
	tn := composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder", "platform:admin")}, build.New(id))
	publishObject(t, tn, "builder", "OBJECT", map[string]any{"name": "delivery", "title": "Delivery", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, txnAt)
	publishObject(t, tn, "builder", "OTHER", map[string]any{"name": "independent", "title": "Independent", "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}, txnAt)
	decide(t, tn, "builder", build.ID, "build.connection.create", build.ConnectionType, "DESTINATION", map[string]any{"name": "destination", "title": "Destination", "kind": "http", "address": "https://destination.example/api"}, txnAt)
	decide(t, tn, "builder", build.ID, "build.writeback.create", build.WritebackType, "WRITEBACK", map[string]any{"name": "deliveryexport", "title": "Delivery export", "connection": "DESTINATION", "object": "build.delivery", "on": "create", "path": "deliveries"}, txnAt)
	decide(t, tn, "builder", build.ID, "build.writeback.publish", build.WritebackType, "WRITEBACK", map[string]any{}, txnAt)
	return tn
}

// Configure through the original credential and connection decisions. Tests
// can then withdraw the value or its binding without falsifying readiness.
func configureWritebackCredential(t *testing.T, tn *Tenant, connectionID string) (platform.CredentialWrite, *bool) {
	t.Helper()
	connection, ok := platform.Get[build.Connection](tn.automation(build.ID, false), connectionID)
	if !ok {
		t.Fatal("connection fixture is missing")
	}
	available := true
	tn.Secrets = func(name string) ([]byte, bool) {
		return []byte("prepared-test-value"), available && name == "configured-value"
	}
	credential := platform.CredentialWrite{CredentialDefinition: platform.CredentialDefinition{ID: "writeback-credential", Title: "Writeback credential", Kind: "bearer", Source: "deployment", Reference: "configured-value", Bindings: []platform.CredentialBinding{{Consumer: "connection/" + connectionID, Origin: controlowner.CredentialOrigin(connection.Address)}}}}
	decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, credential.ID, credential, txnAt)
	decide(t, tn, "builder", build.ID, "build.connection.edit", build.ConnectionType, connectionID, map[string]any{"secret": credential.ID}, txnAt)
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
	decide(t, tn, "builder", build.ID, "build.connection.check", build.ConnectionType, connectionID, map[string]any{}, txnAt)
	tn.CheckConnections(txnAt)
	connection, _ = platform.Get[build.Connection](tn.automation(build.ID, false), connectionID)
	if connection.State != "ready" {
		t.Fatal("configured connection did not become ready", connection.Last)
	}
	tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("preparing a business decision sent an external request")
		return nil, io.ErrUnexpectedEOF
	}
	return credential, &available
}

func TestAutomaticWritebackConfigurationRefusalPrecedesAcceptance(t *testing.T) {
	for _, mode := range []string{"unready", "missing value", "blank value", "wrong consumer", "wrong origin"} {
		t.Run(mode, func(t *testing.T) {
			tn := writebackConfigurationTenant(t, "writeback-preparation")
			credential, available := configureWritebackCredential(t, tn, "DESTINATION")
			switch mode {
			case "unready":
				decide(t, tn, "builder", build.ID, "build.connection.retire", build.ConnectionType, "DESTINATION", map[string]any{}, txnAt)
			case "missing value":
				*available = false
			case "blank value":
				tn.Secrets = func(name string) ([]byte, bool) { return []byte("  "), name == "configured-value" }
			default:
				credential.ExpectedVersion = 1
				if mode == "wrong consumer" {
					credential.Bindings[0].Consumer = "connection/another"
				} else {
					credential.Bindings[0].Origin = "https://another.example"
				}
				decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, credential.ID, credential, txnAt)
			}
			if reason := refuse(t, tn, "builder", build.ID, "build.delivery.create", "build.delivery", "REFUSED", map[string]any{"note": "Must stay private"}, txnAt); reason == "ok" {
				t.Fatal("unconfigured writeback accepted the triggering decision")
			}
			view, refusal := tn.RecordOf(memberOf(t, tn, "builder"), "build.delivery", "REFUSED", txnAt)
			if refusal == nil && view.Record != nil || len(tn.Effects(txnAt)) != 0 {
				t.Fatal("refused preparation committed a record or outbound intent", view, refusal)
			}
		})
	}
}

func TestUnavailableWritebackDoesNotBlockAnUnrelatedFunction(t *testing.T) {
	tn := writebackConfigurationTenant(t, "writeback-independent")
	decide(t, tn, "builder", build.ID, "build.connection.archive", build.ConnectionType, "DESTINATION", map[string]any{}, txnAt)
	decide(t, tn, "builder", build.ID, "build.independent.create", "build.independent", "LOCAL", map[string]any{"note": "Independent local work"}, txnAt)
	if len(tn.Effects(txnAt)) != 0 {
		t.Fatal("unrelated decision generated a writeback")
	}
}

func TestLegacyWritebackReconstructionDoesNotRequireCurrentReadiness(t *testing.T) {
	tn := writebackConfigurationTenant(t, "writeback-legacy-plan")
	_, available := configureWritebackCredential(t, tn, "DESTINATION")
	receipt := decide(t, tn, "builder", build.ID, "build.delivery.create", "build.delivery", "ORIGINAL", map[string]any{"note": "Original input"}, txnAt)
	decide(t, tn, "builder", build.ID, "build.connection.retire", build.ConnectionType, "DESTINATION", map[string]any{}, txnAt)
	*available = false
	event := platform.Event{App: build.ID, Record: receipt}
	legacy, refusal := tn.eventEffects(event, []string{"build.delivery.create"}, false)
	if refusal != nil || len(legacy) != 1 {
		t.Fatal("legacy input acquired current readiness rules", legacy, refusal)
	}
	var request build.WritebackRequest
	if err := json.Unmarshal([]byte(legacy[0].Body), &request); err != nil || request.URL != "https://destination.example/api/deliveries" || request.Secret != "writeback-credential" {
		t.Fatal("legacy frozen request changed", request, err)
	}
	if _, refusal := tn.eventEffects(event, []string{"build.delivery.create"}, true); refusal == nil {
		t.Fatal("current preparation bypassed readiness")
	}
}

func TestBusinessTransportsCheckConnectionCredentialAtPreparation(t *testing.T) {
	for _, transport := range []string{"transaction", "confirmation", "dispatch"} {
		t.Run(transport, func(t *testing.T) {
			var tn *Tenant
			var connection, who, schema, typ, id string
			payload := map[string]any{}
			switch transport {
			case "transaction":
				tn = composeTenant(t, "transaction-credential", []platform.Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited")}, core.New("transaction-credential"), work.New("transaction-credential"), build.New("transaction-credential"))
				txnSetup(t, tn)
				connection, who, schema, typ, id = "DESTINATION", "operator", "build.request.apply", "build.request", "operator"
				payload["label"] = "No accepted effect without its credential"
			case "confirmation":
				tn = confirmationTenant(t, "confirmation-credential")
				tn.Outbound = func(*http.Request, bool) (*http.Response, error) { return confirmationResponse(200, `{}`) }
				prepareBusinessConfirmation(t, tn, txnAt, businessConfirmationSpec("hold", "hold"))
				decide(t, tn, "operator", build.ID, "build.receipt.create", "build.receipt", "RECEIPT", map[string]any{"status": "new"}, txnAt)
				connection, who, schema, typ, id = "erp", "operator", "build.receipt.start", "build.receipt", "RECEIPT"
			case "dispatch":
				tn = deviceDispatchTenant(t)
				prepareDeviceDispatch(t, tn, txnAt, "record", true)
				decide(t, tn, "operator", build.ID, "build.movement.create", "build.movement", "MOVEMENT", map[string]any{"status": "new", "device": "controller-1"}, txnAt)
				connection, who, schema, typ, id = "TARGET", "operator", "build.movement.send", "build.movement", "MOVEMENT"
			}
			credential, available := configureWritebackCredential(t, tn, connection)
			switch transport {
			case "transaction":
				*available = false
			case "confirmation":
				credential.ExpectedVersion = 1
				credential.Bindings[0].Consumer = "connection/another"
				decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, credential.ID, credential, txnAt)
			case "dispatch":
				credential.ExpectedVersion = 1
				credential.Bindings[0].Origin = "https://another.example"
				decide(t, tn, "builder", controlowner.PlatformApp, controlowner.SchemaCredentialSave, controlowner.CredentialType, credential.ID, credential, txnAt)
			}
			before := txnState(tn)
			if reason := refuse(t, tn, who, build.ID, schema, typ, id, payload, txnAt); !strings.Contains(reason, "credential") {
				t.Fatal("transport did not report the actual credential failure", reason)
			}
			if txnState(tn) != before || len(tn.Effects(txnAt)) != 0 {
				t.Fatal("transport preparation changed business records or queued an effect")
			}
		})
	}
}
