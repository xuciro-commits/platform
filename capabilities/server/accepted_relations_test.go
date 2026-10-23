package platformserver

import (
	"errors"
	resultformat "platformserver/internal/acceptance/result"
	"strings"
	"testing"
	"time"

	"platformserver/apps/relations"
)

func TestSameAuthorityAdHocRelationshipIsFrozen(t *testing.T) {
	tn := stockTenant(t, relations.New("t-1"))
	got := refuse(t, tn, "lead", relations.ID, relations.SchemaLink, relations.LinkType, "legacy", map[string]string{"from": "stock.item/I1", "to": "stock.item/I2"}, time.Now())
	if !strings.Contains(got, "declared relationship transaction") {
		t.Fatalf("ad-hoc authority write stayed live: %s", got)
	}
}

func TestAcceptedRelationGraphIsPrivateUntilCommit(t *testing.T) {
	compose := func() *Tenant {
		return composeTenant(t, "t-1", []Seat{seatOf("ana", "stock:clerk", "invariants:operator")}, newStock("t-1"), newConstraintOwner("t-1"), relations.New("t-1"))
	}
	tn := compose()
	ana, _ := tn.Member("ana")
	at := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	decide(t, tn, "ana", "invariants", "invariants.entry.create", "invariants.entry", "C1", validConstraintEntry("C1"), at)
	baseline := len(tn.app(relations.ID).(*relations.Relations).AcceptedLedger().RecordsFor(tn.ID))
	fail := true
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		// Append already holds the tenant lane. Inspect the committed owner
		// here; public coordinated reads cannot re-enter that same lane.
		owner := tn.app(relations.ID)
		links, refusal := readApp(owner, tn.caller(ana, owner, false), "links")
		if refusal != nil {
			t.Fatal(refusal)
		}
		if len(links.([]relations.Link)) != 0 ||
			len(tn.app(relations.ID).(*relations.Relations).AcceptedLedger().RecordsFor(tn.ID)) != baseline {
			t.Fatal("link or ledger became visible before append")
		}
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	sub := submission(tn, ana, relations.ID, relations.SchemaLink, relations.LinkType, "L1", map[string]string{"from": "stock.item/I1", "to": "invariants.entry/C1"})
	if _, err := tn.Submit(ana, sub, at); err == nil || len(entries) != 1 {
		t.Fatalf("append failure accepted a graph: %v", err)
	}
	fail = false
	first, err := tn.Submit(ana, sub, at)
	if err != nil || len(entries) != 2 {
		t.Fatalf("relation not accepted: %v", err)
	}
	batch, _, errDecode := resultformat.DecodeBatch(entries[1].Body)
	if errDecode != nil || len(batch.Rows) != 0 || len(batch.States) != 1 || batch.States[0].App != relations.ID {
		t.Fatalf("relation graph is not a bounded owner state: %+v, %v", batch, errDecode)
	}
	links, refusal := tn.Read(ana, "links")
	if refusal != nil || len(links.([]relations.Link)) != 1 {
		t.Fatalf("committed link missing: %+v, %v", links, refusal)
	}
	second, err := tn.Submit(ana, sub, at.Add(time.Hour))
	if err != nil || first.GetChangeId() != second.GetChangeId() || len(entries) != 2 {
		t.Fatalf("retry reran graph decision: %v", err)
	}
	CheckReplay(t, tn, entries, compose)
}
