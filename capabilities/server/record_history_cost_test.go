package platformserver

import (
	"encoding/json"
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"testing"
	"time"
)

func TestOrdinaryAcceptedEditDoesNotReadRetainedHistory(t *testing.T) {
	for _, count := range []int{10, 1000, 100000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			live := stockTenant(t)
			at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
			decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 1, "line": "L1"}, at)
			et := live.records.types["stock.item"]
			prior := et.rows.Get("I1")
			history := make([]records.RecordChange, count)
			for i := range history {
				history[i] = prior.RecordHistory()[0]
			}
			retained := *prior
			retained.History = history
			retained.HistoryParent = nil
			retained.HistoryCount = count
			retained.HistoryHead = ""
			head, err := retained.HistoryDigest()
			if err != nil {
				t.Fatal(err)
			}
			reads := 0
			retained.History = nil
			retained.HistoryHead = head
			retained.LoadHistory = func(start int) []records.RecordChange { reads++; return history[start:] }
			et.setRow("I1", &retained)
			var saved Entry
			live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { saved = e; return e.Body, nil }
			decide(t, live, "ana", "stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 2}, at.Add(time.Second))
			if reads != 0 {
				t.Fatalf("ordinary edit read retained history %d times", reads)
			}
			result, _, err := acceptedformat.DecodeRecord(saved.Body)
			if err != nil {
				t.Fatal(err)
			}
			if result.Version != 5 || result.Row.HistoryRef.Base != count || len(result.Row.History) != 1 || len(saved.Body) > 10000 {
				t.Fatal("edit did not retain a bounded history suffix")
			}
			current := et.rows.Get("I1")
			_ = current // promotion replaces the type image
			current = live.records.types["stock.item"].rows.Get("I1")
			if current.HistoryLength() != count+1 {
				t.Fatal("history length lost")
			}
			all := current.RecordHistory()
			if reads != 1 || len(all) != count+1 || all[count].Schema != "stock.item.edit" {
				t.Fatal("explicit historical read lost its original prefix")
			}
			broken := result
			ref := *result.Row.HistoryRef
			ref.Prior = "bad"
			broken.Row.HistoryRef = &ref
			broken.Digest, _ = acceptedformat.DigestRecord(broken)
			raw, _ := json.Marshal(broken)
			if _, _, err := acceptedformat.DecodeRecord(raw); err == nil {
				t.Fatal("invalid historical chain was accepted")
			}
		})
	}
}

func TestSegmentedHistoryReadPreservesOrderAndPrefix(t *testing.T) {
	const count = 3000
	var current *records.Row
	for n := 0; n < count; n++ {
		next := &records.Row{}
		if err := records.AppendRowHistory(next, current, []records.RecordChange{{Change: fmt.Sprint(n)}}); err != nil {
			t.Fatal(err)
		}
		current = next
	}
	full := current.RecordHistory()
	if len(full) != count {
		t.Fatal("segmented history lost entries", len(full))
	}
	for n, h := range full {
		if h.Change != fmt.Sprint(n) {
			t.Fatal("segmented history reordered entry", n)
		}
	}
	for _, start := range []int{0, 1, 1500, count - 1, count} {
		suffix := current.HistoryAfter(start)
		if len(suffix) != count-start {
			t.Fatal("wrong history suffix", start, len(suffix))
		}
		if len(suffix) > 0 && suffix[0].Change != fmt.Sprint(start) {
			t.Fatal("wrong first suffix entry", start)
		}
	}
}
