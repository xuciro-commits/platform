package platformserver

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type resultProjection struct {
	notes []string
	plans int
	fail  bool
}

func (*resultProjection) Manifest() platform.Manifest {
	return platform.Manifest{ID: "projection", Title: "Projection", Version: "1",
		Actions: platform.NewCatalog(), Reads: []string{"notes"}}
}
func (*resultProjection) Declarations() []*pb.AuthorityDeclaration { return nil }
func (*resultProjection) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, unknown()
}
func (*resultProjection) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, unknown()
}
func (p *resultProjection) Read(platform.Caller, string) (any, *kernel.Error) { return p.notes, nil }
func (p *resultProjection) Snapshot() (json.RawMessage, error)                { return json.Marshal(p.notes) }
func (p *resultProjection) Restore(raw json.RawMessage) error                 { return json.Unmarshal(raw, &p.notes) }
func (*resultProjection) Observe(platform.Event, []string) {
	panic("an accepted event called today's observer")
}
func (p *resultProjection) PlanObserved(c platform.Caller, e platform.Event, _ []string) (json.RawMessage, error) {
	p.plans++
	if p.fail {
		return nil, errors.New("today's observer cannot plan this event")
	}
	c.Notify(platform.Notification{Title: "Record changed", Key: e.App + "/" + e.Record.GetChangeId()},
		e.Record.GetRecordedTime().AsTime(), platform.Recipient{Member: "ana"})
	return json.Marshal([]string{e.Record.GetSubmission().GetSchema().GetName() + ":" + target(e.Record.GetSubmission())})
}
func (*resultProjection) ValidateObserved(raw json.RawMessage) error {
	var notes []string
	if err := json.Unmarshal(raw, &notes); err != nil {
		return err
	}
	if len(notes) != 1 || notes[0] == "" {
		return errors.New("invalid saved note")
	}
	return nil
}
func (p *resultProjection) ApplyObserved(raw json.RawMessage) error {
	if err := p.ValidateObserved(raw); err != nil {
		return err
	}
	var notes []string
	_ = json.Unmarshal(raw, &notes)
	p.notes = append(p.notes, notes...)
	return nil
}

func TestAcceptedObservationsArePrivateAndRecoveredWithoutReplanning(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("observation", NewConsole("observation", Seat{Subjects: []string{"ana"},
			Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk"}}}),
			newStock("observation"), &resultProjection{})
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose()
	ana, _ := live.Member("ana")
	at := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)
	var entries []Entry
	fail := true
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if len(live.app("projection").(*resultProjection).notes) != 0 || len(live.notices.all) != 0 {
			t.Fatal("an observer projection or notice escaped before append")
		}
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	sub := &pb.Submission{TenantId: live.ID, PrincipalId: ana.ID, Authority: "stock", IdempotencyKey: "create",
		Target: &pb.EntityRef{Type: "stock.item", Id: "I1"}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1},
		Payload: []byte(`{"name":"Bolt","qty":2,"line":"L1"}`)}
	before := snapshot(live)
	if _, err := live.Submit(ana, sub, at); err == nil || snapshot(live) != before {
		t.Fatal("failed observer append changed authoritative state")
	}
	fail = false
	if _, err := live.Submit(ana, sub, at); err != nil {
		t.Fatal(err)
	}
	batch, _, err := decodeAcceptedBatch(entries[0].Body)
	if err != nil || len(batch.Observations) != 1 || !batch.Decisions[0].Event.Observed ||
		len(live.notices.all) != 1 || len(live.app("projection").(*resultProjection).notes) != 1 {
		t.Fatalf("observer result missing: %+v %v", batch, err)
	}
	CheckReplay(t, live, entries, compose)
	recovered := compose()
	recovered.app("projection").(*resultProjection).fail = true
	if err := recovered.Replay(entries); err != nil || snapshot(recovered) != snapshot(live) ||
		recovered.app("projection").(*resultProjection).plans != 0 {
		t.Fatalf("recovery re-planned the saved observation: %v", err)
	}
	// A late invalid projection cannot advance even the earlier valid ledger.
	batch.Observations[0].Body = json.RawMessage(`{}`)
	batch.Digest, _ = digestAcceptedBatch(batch)
	bad, _ := json.Marshal(batch)
	empty := compose()
	before = snapshot(empty)
	if _, err := empty.applyAcceptedBatch(empty.app("stock").(platform.ResultApp).AcceptedLedger(), bad); err == nil ||
		snapshot(empty) != before {
		t.Fatal("invalid observation leaked an earlier decision")
	}
}
