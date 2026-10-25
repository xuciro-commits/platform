package platformserver

import "platformserver/internal/construction"

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestBusinessRefusalCorrectionUsesOriginalIdentityAndProjection(t *testing.T) {
	for _, label := range []string{"flag", "accepted"} {
		t.Run(label, func(t *testing.T) {
			tn := txnTenant(t)
			var entries []Entry
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
			businessRefusalOwnerSetup(t, tn)
			member := memberOf(t, tn, "operator")
			original := txnRequest(t, tn, "operator", "block")
			before := txnState(tn)
			_, refusal, issues := tn.submitDiagnosed(member, original, txnAt)
			if refusal == nil || len(issues) != 1 || issues[0].BusinessCode != "blockreason" {
				t.Fatalf("business reason missing: %v %+v", refusal, issues)
			}
			txnUnchanged(t, tn, before)
			count := len(entries)
			_, same, again := tn.submitDiagnosed(member, original, txnAt.Add(time.Second))
			if same == nil || *same != *refusal || len(again) != 1 || len(entries) != count {
				t.Fatal("same key made another refusal")
			}
			corrected := txnRequest(t, tn, "operator", label)
			corrected.CorrelationId = "correction:" + original.IdempotencyKey
			receipt, err := tn.Submit(member, corrected, txnAt.Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := tn.Submit(member, corrected, txnAt.Add(2*time.Second))
			if err != nil || !proto.Equal(receipt, repeated) {
				t.Fatal("correction was not idempotent")
			}
			page, err := tn.BusinessRefusals(member, "build.request", "operator", 0, 200, txnAt)
			if err != nil || page.Total != 2 {
				t.Fatalf("history count: %+v %v", page, err)
			}
			if page.Records[0].CorrectionOf != original.IdempotencyKey || len(page.Records[0].Changes) != 1 || page.Records[0].Changes[0].Field != "label" || len(page.Records[0].Facts) != map[string]int{"flag": 1, "accepted": 0}[label] {
				t.Fatalf("correction lineage missing: %+v", page)
			}
			other, err := tn.BusinessRefusals(memberOf(t, tn, "limited"), "build.request", "operator", 0, 200, txnAt)
			if err != nil || other.Total != 0 {
				t.Fatal("another member read refused input history")
			}
			forged := proto.Clone(corrected).(*pb.Submission)
			forged.CorrelationId = "correction:another"
			if _, err := tn.Submit(member, forged, txnAt); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
				t.Fatal("same key switched correction origin")
			}
			CheckReplay(t, tn, entries, func() *Tenant { return txnTenant(t) })
			restored := txnTenant(t)
			if err := restored.Replay(entries); err != nil {
				t.Fatal(err)
			}
			recovered, err := restored.BusinessRefusals(memberOf(t, restored, "operator"), "build.request", "operator", 0, 200, txnAt)
			if err != nil || !reflect.DeepEqual(page, recovered) {
				t.Fatalf("restored correction evidence differs: %+v %v", recovered, err)
			}
		})
	}
}

func TestBusinessRefusalPersistenceReopensWithoutExecutingRules(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			ctx := context.Background()
			id := fmt.Sprintf("refusal-persist-%d", time.Now().UnixNano())
			compose := func() *Tenant {
				return composeTenant(t, id, []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited")}, build.New(id))
			}
			store := backend.open(t, id)
			defer backend.cleanup(t, id)
			if _, err := store.Entries(ctx, id, 0); err != nil {
				t.Fatal(err)
			}
			live := compose()
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) { return store.AppendAccepted(ctx, id, e, key, hash) }
			businessRefusalOwnerSetup(t, live)
			original := txnRequest(t, live, "operator", "block")
			live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
				if _, err := store.AppendAccepted(ctx, id, e, key, hash); err != nil {
					t.Fatal(err)
				}
				panic("after refusal append")
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("crash not injected")
					}
				}()
				_, _ = live.Submit(memberOf(t, live, "operator"), original, txnAt)
			}()
			store.Close()
			reopened := backend.reopen(t, id)
			defer reopened.Close()
			saved, err := reopened.Entries(ctx, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			restored := compose()
			if err := restored.Replay(saved); err != nil {
				t.Fatal(err)
			}
			page, readErr := restored.BusinessRefusals(memberOf(t, restored, "operator"), "build.request", "operator", 0, 200, txnAt)
			if readErr != nil || page.Total != 1 || page.Records[0].Code != "blockreason" {
				t.Fatalf("durable refusal facts lost: %+v %v", page, readErr)
			}
			before := txnState(restored)
			_, refusal := restored.Submit(memberOf(t, restored, "operator"), original, txnAt.Add(time.Hour))
			if refusal == nil {
				t.Fatal("reopened key executed instead of returning refusal")
			}
			txnUnchanged(t, restored, before)
			// A later flag is accepted with its original snapshot, then survives reopening.
			restored.AcceptResult = func(e Entry, key, hash string) ([]byte, error) { return reopened.AppendAccepted(ctx, id, e, key, hash) }
			changed := txnRequest(t, restored, "operator", "flag")
			changed.CorrelationId = "correction:" + original.IdempotencyKey
			if _, err := restored.Submit(memberOf(t, restored, "operator"), changed, txnAt.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			all, err := reopened.Entries(ctx, id, 0)
			if err != nil {
				t.Fatal(err)
			}
			CheckReplay(t, restored, all, compose)
		})
	}
}

func TestBusinessCorrectionDistinguishesAbsentAndNullInputs(t *testing.T) {
	changes := changedBusinessInputs([]byte(`{}`), []byte(`{"value":null}`))
	if len(changes) != 1 || changes[0].Before != nil || string(changes[0].After) != "null" {
		t.Fatalf("missing and null collapsed: %+v", changes)
	}
}

func TestBusinessRefusalPrivateSavepointDoesNotLeakFacts(t *testing.T) {
	tn := txnTenant(t)
	d := tn.newStagedDecision()
	d.at = txnAt
	fact := platform.BusinessRefusalFact{Level: "block", Code: "resource", Message: "Unavailable", Subject: "build.request/operator", Action: "build.request.apply", Member: "operator", At: txnAt, Original: json.RawMessage(`{"id":"operator"}`)}
	parent := platform.NewCaller(d, memberOf(t, tn, "operator"), construction.ID, false, false)
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
