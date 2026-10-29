package platformserver

import (
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func TestLiveRuntimeRefusesNewDecisionsAndRetainsHistoricalReplay(t *testing.T) {
	for _, replay := range []bool{false, true} {
		name := "live"
		if replay {
			name = "historical replay"
		}
		t.Run(name, func(t *testing.T) {
			tn := composeTenant(t, "legacy", []platform.Seat{seatOf("ana", "stock:clerk")}, newStock("legacy"))
			member := memberOf(t, tn, "ana")
			app := tn.app("stock")
			caller := tn.caller(member, app, replay)
			at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
			record, err := platform.Decide(caller, app,
				submission(tn, member, "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 2}), at)
			item, found := platform.Get[Item](caller, "I1")
			if !replay {
				if err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || record != nil || found || len(app.AcceptedLedger().RecordsFor(tn.ID)) != 0 {
					t.Fatalf("live runtime bypassed private acceptance: record=%v item=%+v err=%v", record, item, err)
				}
				return
			}
			if err != nil || record == nil || !found || item.Qty != 2 || caller.Staging() ||
				len(app.(platform.App).AcceptedLedger().RecordsFor(tn.ID)) != 1 {
				t.Fatalf("legacy decision path changed: record=%v item=%+v err=%v", record, item, err)
			}
			called := false
			refusal := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
			_, err = platform.Attempt(caller, func() (*pb.ChangeRecord, *kernel.Error) {
				called = true
				return nil, refusal
			})
			if !called || err != refusal {
				t.Fatal("legacy attempt no longer retains direct execution")
			}
		})
	}
}

type missingPrivateLedgerRuntime struct{ platform.Runtime }

func (missingPrivateLedgerRuntime) HoldChanges(*platform.Ledger) (func(), *kernel.Error) {
	return func() {}, nil
}

func (missingPrivateLedgerRuntime) DraftChanges(*platform.Ledger, func() platform.ChangeLog) platform.ChangeLog {
	return nil
}

func TestStagedRuntimeWithoutPrivateLedgerRefusesBeforeApplication(t *testing.T) {
	tn := composeTenant(t, "private", []platform.Seat{seatOf("ana", "stock:clerk")}, newStock("private"))
	member := memberOf(t, tn, "ana")
	app := tn.app("stock")
	caller := platform.NewCaller(missingPrivateLedgerRuntime{runtime{tn}}, member, "stock", false, false)
	before := snapshot(tn)
	_, err := platform.Decide(caller, app,
		submission(tn, member, "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 2}), time.Now())
	if err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || !caller.Staging() ||
		len(app.(platform.App).AcceptedLedger().RecordsFor(tn.ID)) != 0 || snapshot(tn) != before {
		t.Fatalf("missing private ledger fell back to authoritative state: %v", err)
	}
}
