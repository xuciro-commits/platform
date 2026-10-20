package platformserver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// This owner exercises relationship writes inside the real accepted decision,
// including a newly created endpoint, rather than constructing staged rows.
type recordSetOwner struct {
	*constraintOwner
	link platform.LinkType
}

func newRecordSetOwner(tenant string) *recordSetOwner {
	base := newConstraintOwner(tenant)
	base.entity.Standard.Roles = []string{"operator", "observer"}
	base.entity.Scope = platform.Scope{Levels: map[string]string{"operator": platform.ScopeTenant, "observer": platform.ScopeOwn}, Default: platform.ScopeNone, Owner: platform.OwnerCreated}
	ref := platform.AssetRef{App: "invariants", Kind: platform.AssetObject, Name: base.entity.Type}
	link := platform.LinkType{Name: "children", Title: "Children", Description: "Owned child records", Parent: ref, Child: ref, Forward: "children", Reverse: "parent", Storage: "edge", Cardinality: "one-to-one", DeletePolicy: "restrict-active", Roles: []string{"operator"}}
	base.entity.Sets = []platform.RecordSet{{Name: "children", From: link.Name, Binding: &platform.AssetBinding{Ref: platform.AssetRef{App: ref.App, Kind: platform.AssetLinkType, Name: link.Name}, SourceVersion: "1"}, Direction: "forward", Object: ref.Name, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}, Limit: 2}}
	actions := platform.EntityActions(base.entity)
	actions = append(actions, platform.Action{Schema: "invariants.entry.attach", Target: ref.Name, Title: "Attach", Description: "Attach a child in the same decision", Capability: ref.Name, Roles: []string{"operator"}, Payload: []platform.Field{{Name: "child", Type: "text", Required: true, Description: "Child"}, {Name: "create", Type: "boolean", Description: "Create child"}, {Name: "remove", Type: "boolean", Description: "Remove relationship"}}})
	base.ledger = platform.NewLedger(tenant, ref.App, platform.NewCatalog(actions...), ref.Name, platform.OwnedRelationType(ref.App))
	return &recordSetOwner{constraintOwner: base, link: link}
}

func (o *recordSetOwner) Manifest() platform.Manifest {
	m := o.constraintOwner.Manifest()
	m.Entities = append(m.Entities, platform.RelationEntity(m.ID))
	m.LinkTypes = []platform.LinkType{o.link}
	return m
}

func (o *recordSetOwner) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if s.GetSchema().GetName() != "invariants.entry.attach" {
		r, err, _ := o.ledger.Generated(c, s, at, nil, o.entity)
		return r, err
	}
	var payload struct {
		Child  string `json:"child"`
		Create bool   `json:"create"`
		Remove bool   `json:"remove"`
	}
	if json.Unmarshal(s.GetPayload(), &payload) != nil {
		return nil, unknown()
	}
	return o.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return func(receipt *pb.ChangeRecord) {
			if payload.Create {
				row := constraintEntry{Record: platform.Record{ID: payload.Child}, Number: payload.Child, Qty: 2, Start: "2026-10-12", End: "2026-10-13", Version: "v1", State: "draft"}
				if c.Put(receipt, row) != nil {
					return
				}
			}
			_ = c.PutOwnedRelation(receipt, platform.TxnLink{Binding: *o.entity.Sets[0].Binding, Parent: platform.TxnRef{Type: o.entity.Type, ID: s.GetTarget().GetId()}, Child: platform.TxnRef{Type: o.entity.Type, ID: payload.Child}}, payload.Remove, at)
		}, nil
	})
}

func recordSetTenant(t *testing.T, owner *recordSetOwner) *Tenant {
	t.Helper()
	return composeTenant(t, "record-sets", []Seat{seatOf("writer", "invariants:operator"), seatOf("viewer", "invariants:observer")}, owner)
}

func TestOwnedRelationsAtomicCardinalityAndReplay(t *testing.T) {
	owner := newRecordSetOwner("record-sets")
	tn := recordSetTenant(t, owner)
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	for _, id := range []string{"root", "other"} {
		decide(t, tn, "writer", "invariants", "invariants.entry.create", owner.entity.Type, id, validConstraintEntry(id), at)
	}
	decide(t, tn, "writer", "invariants", "invariants.entry.attach", owner.entity.Type, "root", map[string]any{"child": "child", "create": true}, at)
	for range 12 {
		if got := refuse(t, tn, "writer", "invariants", "invariants.entry.attach", owner.entity.Type, "other", map[string]any{"child": "child"}, at); !strings.Contains(got, "Constraint relation.children") {
			t.Fatalf("duplicate child: %s", got)
		}
	}
	if got := refuse(t, tn, "writer", "invariants", "invariants.entry.attach", owner.entity.Type, "root", map[string]any{"child": "rejected", "create": true}, at); !strings.Contains(got, "Constraint relation.children") {
		t.Fatalf("duplicate parent: %s", got)
	}
	if tn.records.types[platform.OwnedRelationType("invariants")].rows.Len() != 1 {
		t.Fatal("refusal left a relationship row")
	}
	if _, err := tn.RecordOf(memberOf(t, tn, "writer"), owner.entity.Type, "rejected", at); err == nil {
		t.Fatal("refused relation left a new endpoint")
	}
	if got := refuse(t, tn, "writer", "invariants", "invariants.entry.archive", owner.entity.Type, "root", nil, at); !strings.Contains(got, "Constraint relation.children") {
		t.Fatalf("archive policy: %s", got)
	}
	caller := tn.caller(memberOf(t, tn, "writer"), owner, false)
	page, err := caller.ReadRecordSet(owner.entity.Type, "root", "children", at)
	if err != nil || len(page.Records) != 1 || page.Partial {
		t.Fatalf("owned set: %+v %v", page, err)
	}
	recoveredOwner := newRecordSetOwner("record-sets")
	recovered := recordSetTenant(t, recoveredOwner)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	read := recovered.caller(memberOf(t, recovered, "writer"), recoveredOwner, false)
	got, failure := read.ReadRecordSet(owner.entity.Type, "root", "children", at)
	if failure != nil || string(platform.Raw(got)) != string(platform.Raw(page)) {
		t.Fatalf("replayed set differs: %+v %v", got, failure)
	}
	decide(t, tn, "writer", "invariants", "invariants.entry.attach", owner.entity.Type, "root", map[string]any{"child": "child", "remove": true}, at)
	decide(t, tn, "writer", "invariants", "invariants.entry.archive", owner.entity.Type, "root", nil, at)
}

func TestDeclaredSetVisibilityOrderAndLimit(t *testing.T) {
	owner := newRecordSetOwner("record-sets")
	owner.link.Cardinality = "many-to-many"
	tn := recordSetTenant(t, owner)
	at := time.Now().UTC()
	for _, item := range []struct{ id, who string }{{"root", "viewer"}, {"z", "viewer"}, {"a", "writer"}} {
		decide(t, tn, item.who, "invariants", "invariants.entry.create", owner.entity.Type, item.id, validConstraintEntry(item.id), at)
	}
	for _, id := range []string{"z", "a"} {
		decide(t, tn, "writer", "invariants", "invariants.entry.attach", owner.entity.Type, "root", map[string]any{"child": id}, at)
	}
	viewer := tn.caller(memberOf(t, tn, "viewer"), owner, false)
	visible, err := viewer.ReadRecordSet(owner.entity.Type, "root", "children", at)
	if err != nil || !visible.Partial || len(visible.Records) != 1 || strings.Contains(string(platform.Raw(visible)), "invariants.entry/a") {
		t.Fatalf("visibility: %+v %v", visible, err)
	}
	aggregate, aggregateErr := tn.Aggregate(memberOf(t, tn, "viewer"), owner.entity.Type, AggregateQuery{Collection: &platform.RecordSetRef{Object: owner.entity.Type, ID: "root", Name: "children"}, Measures: []string{"count"}}, at)
	if aggregateErr != nil || !aggregate.Partial || len(aggregate.Rows) != 1 || aggregate.Rows[0]["count"] != 1 {
		t.Fatalf("aggregate leaked hidden membership: %v %v", aggregate, aggregateErr)
	}
	writer := tn.caller(memberOf(t, tn, "writer"), owner, false)
	all, err := writer.ReadRecordSet(owner.entity.Type, "root", "children", at)
	if err != nil || all.Partial || len(all.Records) != 2 || !strings.Contains(string(all.Records[0]), `"id":"a"`) {
		t.Fatalf("stable order: %+v %v", all, err)
	}
	decide(t, tn, "writer", "invariants", "invariants.entry.attach", owner.entity.Type, "root", map[string]any{"child": "overflow", "create": true}, at)
	if _, err := writer.ReadRecordSet(owner.entity.Type, "root", "children", at); err == nil || !strings.Contains(err.Message, "limit") {
		t.Fatalf("set was silently truncated: %v", err)
	}
}
