package platformserver

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/relations"
)

func TestAcceptedRelationGraphIsPrivateUntilCommit(t *testing.T) {
	compose := func() *Tenant { return stockTenant(t, relations.New("t-1")) }
	tn := compose()
	ana, _ := tn.Member("ana")
	at := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)
	var entries []Entry
	fail := true
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		links, _ := tn.Read(ana, "links")
		if len(links.([]relations.Link)) != 0 ||
			len(tn.app(relations.ID).(*relations.Relations).AcceptedLedger().Changes.Records(tn.ID)) != 0 {
			t.Fatal("link or ledger became visible before append")
		}
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	body, _ := json.Marshal(map[string]string{"from": "stock.item/I1", "to": "stock.item/I2"})
	sub := &pb.Submission{TenantId: tn.ID, PrincipalId: ana.ID, Authority: relations.ID,
		IdempotencyKey: "link-I1-I2", Target: &pb.EntityRef{Type: relations.LinkType, Id: "L1"},
		Schema: &pb.SchemaRef{Name: relations.SchemaLink, Version: 1}, Payload: body}
	if _, err := tn.Submit(ana, sub, at); err == nil || len(entries) != 0 {
		t.Fatalf("append failure accepted a graph: %v", err)
	}
	fail = false
	first, err := tn.Submit(ana, sub, at)
	if err != nil || len(entries) != 1 {
		t.Fatalf("relation not accepted: %v", err)
	}
	batch, _, errDecode := decodeAcceptedBatch(entries[0].Body)
	if errDecode != nil || len(batch.Rows) != 0 || len(batch.States) != 1 || batch.States[0].App != relations.ID {
		t.Fatalf("relation graph is not a bounded owner state: %+v, %v", batch, errDecode)
	}
	links, refusal := tn.Read(ana, "links")
	if refusal != nil || len(links.([]relations.Link)) != 1 {
		t.Fatalf("committed link missing: %+v, %v", links, refusal)
	}
	second, err := tn.Submit(ana, sub, at.Add(time.Hour))
	if err != nil || first.GetChangeId() != second.GetChangeId() || len(entries) != 1 {
		t.Fatalf("retry reran graph decision: %v", err)
	}
	CheckReplay(t, tn, entries, compose)
}
