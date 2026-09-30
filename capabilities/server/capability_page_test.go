package platformserver

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"platformserver/platform"
)

type pageComputeStock struct{ *computeStock }

func (s pageComputeStock) Manifest() platform.Manifest {
	m := s.computeStock.Manifest()
	m.Entities[1].Seed = []any{Item{Record: platform.Record{ID: "I"}, Name: "Source", Qty: 4, Owner: "ana"}}
	return m
}

func TestPageComputeBindingRetainsScopedSources(t *testing.T) {
	app := pageComputeStock{&computeStock{stock: newStock("page-compute")}}
	member := platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}
	tn, err := NewTenant("page-compute", NewConsole("page-compute", Seat{Subjects: []string{"ana"}, Member: member}), app)
	if err != nil {
		t.Fatal(err)
	}
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { return entry.Body, nil }
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	member, _ = tn.Member("ana")
	if _, refusal := tn.RecordOf(member, "stock.item", "I", now); refusal != nil {
		t.Fatalf("source setup: %s; rows=%v", refusal.Message, tn.records.types["stock.item"].rows)
	}
	ref := platform.AssetRef{App: "stock", Kind: platform.AssetCompute, Name: "double"}
	q := platform.CapabilityInvocation{Ref: ref, Key: "page-call", Inputs: json.RawMessage(`{}`), Record: "stock.item/I", Bindings: map[string]platform.Binding{"value": {Source: "subject", Path: []string{"qty"}}}}
	result, refusal := tn.InvokeCapability(member, q, now)
	if refusal != nil || result.State != "pending" || result.Call == "" || app.calls != 0 {
		t.Fatalf("page call did not create the canonical pending operation: %+v %v", result, refusal)
	}
	for _, execute := range tn.operationDispatches(now) {
		execute()
	}
	answer, refusal := tn.ReadOperation(member, result.Call)
	if refusal != nil || answer.State != "completed" || string(answer.Output) != `{"doubled":8}` || app.calls != 1 {
		t.Fatalf("page result: %+v %v", answer, refusal)
	}
	var retained operationBinding
	for _, effect := range tn.outbound {
		if effect.ID == result.Call {
			_ = json.Unmarshal([]byte(effect.Body), &retained)
		}
	}
	if !slices.Contains(retained.Call.Sources, "stock.item/I") || !slices.Contains(retained.Call.Sources, "stock.item/I#qty") {
		t.Fatal("host did not derive record and field source labels")
	}
	for i := range tn.records.types["stock.item"].info.Fields {
		if tn.records.types["stock.item"].info.Fields[i].Name == "qty" {
			tn.records.types["stock.item"].info.Fields[i].Read = []string{"lead"}
		}
	}
	if _, refusal := tn.ReadOperation(member, result.Call); refusal == nil {
		t.Fatal("stored page result escaped a revoked source field")
	}
	if _, refusal := tn.InvokeCapability(member, platform.CapabilityInvocation{Ref: ref, Key: "private", Inputs: json.RawMessage(`{}`), Record: "stock.item/I", Bindings: map[string]platform.Binding{"value": {Source: "subject", Path: []string{"secret"}}}}, now); refusal == nil {
		t.Fatal("page binding read a private field")
	}
}
