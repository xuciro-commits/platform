package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformserver/platform"
)

func dispatchDefinition() construction.Object {
	destination := platform.ExprReference("text", "destination")
	return construction.Object{Name: "movement", Title: "Movement", Fields: []construction.Field{{Name: "status", Title: "Status", Type: "text"}, {Name: "destination", Title: "Destination", Type: "text"}}, States: []construction.State{{Name: "ready", Title: "Ready"}},
		DispatchCommands: []platform.DispatchCommand{{Name: "move", Title: "Move to target", EndpointKind: "controller", Writeback: "local-command"}},
		Actions: []construction.Action{{Name: "send", Title: "Send", From: []string{"ready"}, Transaction: &platform.BusinessTxn{Subject: "build.movement", Steps: []platform.TxnStep{{Name: "mark", Kind: "update", Target: platform.TxnRef{Type: "build.movement", ID: "$subject"}, Changes: []platform.TxnSet{{Field: "status", From: "=sent"}}}},
			Dispatch: &platform.Dispatch{Command: "move", Target: "local-target", Key: "$subject", DeadlineSeconds: 60, ReviewerRole: construction.Builder, OnStuck: "manual", OnFail: "manual", Payload: []platform.TxnSet{{Field: "destination", Expr: &destination}}, Readback: platform.DispatchRead{Name: "movement-status", Source: "local-status", KeyField: "key", ResultField: "status", EverySeconds: 1, States: map[string]string{"moving": "running", "done": "completed", "no-status": "unknown", "error": "failed"}}},
		}}}}
}

func TestDispatchPublicationRejectsUnsafeRetryAndInvalidDefinitions(t *testing.T) {
	b := New("tenant")
	base := dispatchDefinition()
	if err := b.checkTransactions(base); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"unsafe stuck retry", "unsafe failure retry", "undeclared command", "duplicate command", "missing status read", "ambiguous status read", "missing stuck policy", "unknown status mapping", "input business key", "undeclared compensation", "reserved key parameter"} {
		t.Run(name, func(t *testing.T) {
			var object construction.Object
			if err := json.Unmarshal(platform.Raw(base), &object); err != nil {
				t.Fatal(err)
			}
			spec := object.Actions[0].Transaction.Dispatch
			switch name {
			case "unsafe stuck retry":
				spec.OnStuck = "retry"
			case "unsafe failure retry":
				spec.OnFail = "retry"
			case "undeclared command":
				spec.Command = "unlisted"
			case "duplicate command":
				object.DispatchCommands = append(object.DispatchCommands, object.DispatchCommands[0])
			case "missing status read":
				spec.Readback.Source = ""
			case "ambiguous status read":
				spec.Readback.Observation = "another"
			case "missing stuck policy":
				spec.OnStuck = ""
			case "unknown status mapping":
				spec.Readback.States["guess"] = "maybe"
			case "input business key":
				spec.Key = "input.request"
			case "undeclared compensation":
				spec.OnFail = "compensate"
			case "reserved key parameter":
				spec.Payload[0].Field = "key"
			}
			if err := b.checkTransactions(object); err == nil {
				t.Fatal("Invalid dispatch was allowed for publication")
			}
		})
	}
	for _, safety := range []string{"idempotent", "receiver-deduplicates"} {
		t.Run(safety, func(t *testing.T) {
			object := dispatchDefinition()
			object.Actions[0].Transaction.Dispatch.OnFail = "retry"
			object.DispatchCommands[0].Idempotent = safety == "idempotent"
			object.DispatchCommands[0].ReceiverDeduplicates = safety == "receiver-deduplicates"
			if err := b.checkTransactions(object); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDispatchReleaseBindsStableTransportAssetsOnStartingAction(t *testing.T) {
	b, rows := lifecycleBuild()
	b.host = confirmationBindingHost{lifecycleHost: b.host.(lifecycleHost), records: map[string]any{
		construction.WritebackType + "/local-command": construction.Writeback{Record: platform.Record{ID: "local-command"}, Name: "move-request", Object: "build.movement", On: "dispatch", State: "published"},
		construction.SourceType + "/local-status":     construction.Source{Record: platform.Record{ID: "local-status"}, Name: "movement-status", Connection: "local-target", State: "published"},
		construction.ConnectionType + "/local-target": construction.Connection{Record: platform.Record{ID: "local-target"}, Name: "device-gateway", Kind: "http", State: "ready"},
	}}
	for _, record := range b.host.(confirmationBindingHost).records {
		rows.rows[reflect.TypeOf(record)] = append(rows.rows[reflect.TypeOf(record)], record)
	}
	object := dispatchDefinition()
	object.ID = "movement"
	object.Published = published(object)
	assets, err := releaseAssets([]construction.Object{object}, nil, nil, nil, "1")
	if err != nil {
		t.Fatal(err)
	}
	assets, err = b.bindDispatchAssets(assets)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range assets {
		if asset.Ref.Kind == platform.AssetObject && slices.ContainsFunc(asset.Requires, func(ref platform.AssetRef) bool {
			return ref.Kind == platform.AssetWriteback || ref.Kind == platform.AssetSource
		}) {
			t.Fatal("Dispatch created an object/transport dependency cycle")
		}
		if asset.Ref.Kind != platform.AssetAction || asset.Ref.Name != "build.movement.send" {
			continue
		}
		for _, ref := range []platform.AssetRef{{App: construction.ID, Kind: platform.AssetWriteback, Name: "move-request"}, {App: construction.ID, Kind: platform.AssetConnection, Name: "device-gateway"}, {App: construction.ID, Kind: platform.AssetSource, Name: "movement-status"}} {
			if !slices.Contains(asset.Requires, ref) {
				t.Fatalf("Dispatch omitted its original dependency %s", ref)
			}
		}
		if strings.Contains(string(asset.Body), "local-command") || strings.Contains(string(asset.Body), "local-status") || strings.Contains(string(asset.Body), "local-target") {
			t.Fatal("Dispatch action retained local mutable transport identities")
		}
		if err := platform.CheckAssetDependencies(asset); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("Dispatch action was not produced")
}
