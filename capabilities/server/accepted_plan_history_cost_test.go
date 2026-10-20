package platformserver

import (
	"fmt"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/records"
	"testing"
	"time"

	"platformserver/platform"
)

func TestAcceptedEffectDirectAndChildDoNotReadRetainedHistory(t *testing.T) {
	for _, count := range []int{10, 1000, 100000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			const tenantID = "effect-history-cost"
			at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
			seed := func() (*Tenant, *int) {
				owner := &mixedAnswerStock{&answerStock{newStock(tenantID)}}
				live := composeTenant(t, tenantID, []Seat{seatOf("ana", "stock:clerk")}, owner)
				decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1",
					map[string]any{"name": "Bolt", "qty": 1, "line": "L1"}, at)
				et := live.records.types["stock.item"]
				prior := et.rows.Get("I1")
				history := make([]records.RecordChange, count)
				first := prior.RecordHistory()[0]
				for i := range history {
					history[i] = first
				}
				retained := *prior
				retained.History, retained.HistoryParent = history, nil
				retained.HistoryCount, retained.HistoryHead = count, ""
				head, err := retained.HistoryDigest()
				if err != nil {
					t.Fatal(err)
				}
				reads := 0
				retained.History, retained.HistoryHead = nil, head
				retained.LoadHistory = func(start int) []records.RecordChange {
					reads++
					return history[start:]
				}
				et.setRow("I1", &retained)
				live.outbound = append(live.outbound, &effect{Effect: platform.Effect{ID: "retained-effect", App: "stock",
					Endpoint: "sink", Event: "stock/reply", State: "pending", At: at, Due: at}})
				return live, &reads
			}
			live, reads := seed()
			var saved Entry
			live.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
				if *reads != 0 || live.records.types["stock.bin"].rows.Get("B2") != nil {
					t.Fatal("sealing loaded retained history or exposed the child before acceptance")
				}
				if _, err := acceptedStorageDelta(entry.Body); err != nil {
					t.Fatal(err)
				}
				saved = entry
				return entry.Body, nil
			}
			live.settle("retained-effect", platform.Outcome{Effect: "retained-effect", Result: "delivered", Detail: "accepted"}, at.Add(time.Second))
			result, err := resultformat.DecodeEffect(saved.Body)
			if err != nil || live.quarantined() || *reads != 0 {
				t.Fatalf("effect sealing/application read retained history: reads=%d, error=%v, fault=%+v", *reads, err, live.fault.Load())
			}
			if len(result.Rows) != 1 || result.Rows[0].HistoryRef.Base != count || len(result.Rows[0].History) != 1 ||
				len(result.Changes) == 0 || len(saved.Body) > 32000 {
				t.Fatal("mixed effect did not save a bounded direct suffix with its child")
			}
			fresh, recoveryReads := seed()
			if _, applied, err := fresh.applyAcceptedEffect(saved.Body); err != nil || !applied || *recoveryReads != 0 {
				t.Fatalf("saved effect application read retained history: reads=%d, error=%v, applied=%t", *recoveryReads, err, applied)
			}
			current := fresh.records.types["stock.item"].rows.Get("I1")
			if current.HistoryLength() != count+1 || current.Value.Interface().(Item).Note != "accepted" ||
				fresh.records.types["stock.bin"].rows.Get("B2") == nil {
				t.Fatal("pure application lost the direct suffix or child record")
			}
			all := current.RecordHistory()
			if *recoveryReads != 1 || len(all) != count+1 || all[count].Schema != "Effect answer" {
				t.Fatal("explicit historical read lost its retained prefix")
			}
		})
	}
}
