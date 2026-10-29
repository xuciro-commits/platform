package platformserver

import "platformserver/studio/build"

import (
	"encoding/json"
	"platformserver/engine/business"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func TestBusinessCorrectionDistinguishesAbsentAndNullInputs(t *testing.T) {
	changes := business.ChangedBusinessInputs([]byte(`{}`), []byte(`{"value":null}`))
	if len(changes) != 1 || changes[0].Before != nil || string(changes[0].After) != "null" {
		t.Fatalf("missing and null collapsed: %+v", changes)
	}
}

func TestBusinessRefusalPrivateSavepointDoesNotLeakFacts(t *testing.T) {
	tn := txnTenant(t)
	d := tn.newStagedDecision()
	d.at = txnAt
	fact := platform.BusinessRefusalFact{Level: "block", Code: "resource", Message: "Unavailable", Subject: "build.request/operator", Action: "build.request.apply", Member: "operator", At: txnAt, Original: json.RawMessage(`{"id":"operator"}`)}
	parent := platform.NewCaller(d, memberOf(t, tn, "operator"), build.ID, false, false)
	_, err := platform.Attempt(parent, func() (*pb.ChangeRecord, *kernel.Error) { return nil, parent.RefuseBusiness(fact) })
	if err == nil || len(d.businessRefusals) != 0 || len(d.inputIssues) != 0 {
		t.Fatal("handled refusal polluted parent")
	}
}

func TestBusinessRefusalUnknownCorrectionCannotReserveAKey(t *testing.T) {
	tn := txnTenant(t)
	businessRefusalOwnerSetup(t, tn)
	changed := txnRequest(t, tn, "operator", "accepted")
	changed.CorrelationId = "correction:missing"
	if _, err := tn.Submit(memberOf(t, tn, "operator"), changed, txnAt); err == nil {
		t.Fatal("unknown correction accepted")
	}
	changed.CorrelationId = ""
	if _, err := tn.Submit(memberOf(t, tn, "operator"), changed, txnAt); err != nil {
		t.Fatalf("invalid correction reserved its key: %v", err)
	}
}
