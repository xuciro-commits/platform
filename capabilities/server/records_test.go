package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

// stock is a minimal app on the application model: items on lines, each in a bin.
type Bin struct {
	platform.Record
	Code string `json:"code" field:"required,search"`
}

type Item struct {
	platform.Record
	Name   string            `json:"name" field:"required,search"`
	Note   string            `json:"note" knowledge:"true"`
	Secret string            `json:"secret,omitempty" read:"lead" title:"Tolerance" help:"Only a lead reads it"`
	Qty    int               `json:"qty"`
	Price  platform.Money    `json:"price"`
	Line   string            `json:"line"`
	Owner  string            `json:"owner" field:"readonly"`
	Kind   string            `json:"kind" choices:"part,tool"`
	Serial string            `json:"serial,omitempty" field:"required" when:"kind=tool"` // #138: tools carry a serial, parts never do
	Bin    platform.Ref[Bin] `json:"bin"`
	Tags   []string          `json:"tags"`
	Due    string            `json:"due" type:"date"`
}

type stock struct{ ledger *platform.Ledger }

func (s *stock) AcceptedLedger() *platform.Ledger { return s.ledger }

func stockEntities() []platform.Entity {
	return []platform.Entity{
		{Type: "stock.bin", Title: "Bin", Model: Bin{}, Standard: platform.Standard{Create: true, Roles: []string{"clerk", "lead"}}},
		{Type: "stock.item", Title: "Item", Model: Item{}, Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"clerk", "lead"}},
			KnowledgeFiles: true,
			Scope:          platform.Scope{Structure: "site", Unit: "line", Owner: "owner", Levels: map[string]string{"clerk": platform.ScopeOwn, "lead": platform.ScopeBelow, "line": platform.ScopeUnit}}},
	}
}

func newStock(tenant string) *stock {
	var actions []platform.Action
	for _, e := range stockEntities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	actions = append(actions, platform.Action{Schema: "stock.item.count", Target: "stock.item", Capability: "counts", Title: "Count", Description: "Record a count.",
		Payload: []platform.Field{{Name: "qty", Type: "integer", Required: true, Description: "Counted"}}, Roles: []string{"line"}})
	return &stock{ledger: platform.NewLedger(tenant, "stock", platform.NewCatalog(actions...), "stock.bin", "stock.item")}
}

func (s *stock) Manifest() platform.Manifest {
	return platform.Manifest{ID: "stock", Version: "1", Actions: s.ledger.Catalog, Entities: stockEntities()}
}
func (s *stock) Snapshot() (json.RawMessage, error)       { return s.ledger.Snapshot() }
func (s *stock) Restore(raw json.RawMessage) error        { return s.ledger.Restore(raw) }
func (s *stock) Declarations() []*pb.AuthorityDeclaration { return s.ledger.Declarations() }
func (s *stock) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (s *stock) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
func (s *stock) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if r, err, ok := s.ledger.Generated(c, sub, now, nil, stockEntities()...); ok {
		return r, err
	}
	return s.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		item, ok := platform.Get[Item](c, sub.GetTarget().GetId())
		if !ok {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		var p struct{ Qty int }
		json.Unmarshal(sub.GetPayload(), &p)
		item.Qty = p.Qty
		return func(r *pb.ChangeRecord) { c.Put(r, item) }, nil
	})
}

func stockTenant(t testing.TB, extra ...platform.App) *Tenant {
	seat := func(id, role string) Seat {
		return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{"stock": role}}}
	}
	org := enterprise.New("t-1", platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Site", Kind: "site"}},
		Units: []platform.Unit{{ID: "plant", Kind: "plant"}, {ID: "L1", Kind: "line"}, {ID: "L2", Kind: "line"}},
		Edges: []platform.Edge{{Structure: "site", Unit: "L1", Parent: "plant"}, {Structure: "site", Unit: "L2", Parent: "plant"}},
		Memberships: []platform.Membership{{Party: "member:lead", Unit: "plant", Role: "lead"}, {Party: "member:op", Unit: "L1", Role: "op"},
			{Party: "member:boss", Unit: "plant", Role: "lead"}}})
	apps := []platform.App{NewConsole("t-1", seat("ana", "clerk"), seat("bo", "clerk"), seat("lead", "lead"), seat("op", "line"), seat("boss", "line")), org, newStock("t-1")}
	apps = append(apps, extra...)
	tn, err := NewTenant("t-1", apps...)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func TestRecordDraftIsolationAndPromotion(t *testing.T) {
	tn := stockTenant(t)
	c := platform.NewCaller(runtime{tn}, platform.Member{ID: "ana", Tenant: "t-1"}, "stock", false, false)
	at := timestamppb.New(time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC))
	change := func(id string, revision uint32) *pb.ChangeRecord {
		return &pb.ChangeRecord{ChangeId: id, RecordedTime: at, Revision: revision,
			Submission: &pb.Submission{PrincipalId: "ana", Target: &pb.EntityRef{Type: "stock.item", Id: "I1"},
				Schema: &pb.SchemaRef{Name: "stock.item.create"}}}
	}
	put := func(s *recordStore, id string, revision uint32, qty int) {
		t.Helper()
		if err := s.put(c, change(id, revision), Item{Record: platform.Record{ID: "I1"}, Name: "Bolt", Qty: qty}); err != nil {
			t.Fatal(err)
		}
	}
	put(tn.records, "c1", 1, 2)
	var touched []string
	tn.records.touched = func(typ, id string) { touched = append(touched, typ+"/"+id) }
	discarded := tn.records.forkRecords()
	put(discarded, "discarded", 2, 99)
	live, _ := tn.records.get(c, reflect.TypeFor[Item](), "I1")
	if live.(Item).Qty != 2 || len(touched) != 0 {
		t.Fatal("discarded draft modified or projected a live row")
	}
	draft := tn.records.forkRecords()
	put(draft, "c2", 2, 3)
	if got := len(touched); got != 0 {
		t.Fatalf("uncommitted draft projected %d rows", got)
	}
	live, _ = tn.records.get(c, reflect.TypeFor[Item](), "I1")
	staged, _ := draft.get(c, reflect.TypeFor[Item](), "I1")
	if live.(Item).Qty != 2 || staged.(Item).Qty != 3 {
		t.Fatalf("draft leaked into live records: live=%+v staged=%+v", live, staged)
	}
	if len(tn.records.types["stock.item"].rows["I1"].history) != 1 {
		t.Fatal("draft changed live history before promotion")
	}
	draft.types["stock.item"].rows["I1"].history[0].Fields[0].After[0] ^= 1
	if draft.types["stock.item"].rows["I1"].history[0].Fields[0].After[0] ==
		tn.records.types["stock.item"].rows["I1"].history[0].Fields[0].After[0] {
		t.Fatal("draft shares history bytes with live store")
	}
	draft.types["stock.item"].rows["I1"].history[0].Fields[0].After[0] ^= 1
	if err := tn.records.promoteRecords(draft); err != nil {
		t.Fatal(err)
	}
	if len(touched) != 1 || touched[0] != "stock.item/I1" {
		t.Fatalf("projected rows after promotion: %v", touched)
	}
	live, _ = tn.records.get(c, reflect.TypeFor[Item](), "I1")
	if live.(Item).Qty != 3 || len(tn.records.types["stock.item"].rows["I1"].history) != 2 {
		t.Fatalf("accepted image or history missing: %+v", live)
	}
	if err := tn.records.promoteRecords(draft); err == nil {
		t.Fatal("draft was promoted twice")
	}
	if err := draft.put(c, change("c3", 3), Item{Record: platform.Record{ID: "I1"}, Name: "Bolt", Qty: 4}); err == nil {
		t.Fatal("promoted draft remained writable")
	}
	stale := tn.records.forkRecords()
	put(tn.records, "c3", 3, 4)
	if err := tn.records.promoteRecords(stale); err == nil {
		t.Fatal("stale draft replaced a newer live record")
	}
	live, _ = tn.records.get(c, reflect.TypeFor[Item](), "I1")
	if live.(Item).Qty != 4 {
		t.Fatal("rejecting stale draft changed live record")
	}
}

func TestRecords(t *testing.T) {
	var journal []Entry
	tn := stockTenant(t)
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: "stock", IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	for _, c := range []struct {
		got, want string
	}{
		{do("ana", "stock.bin.create", "stock.bin", "B1", map[string]any{"code": "A-01"}), "ok"},
		{do("ana", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Bolt", "qty": 5, "line": "L1", "kind": "part", "bin": "B1",
			"price": map[string]any{"amount": 150, "currency": "EUR"}, "tags": []string{"m6", "steel"}, "due": "2026-10-01"}), "ok"},
		{do("bo", "stock.item.create", "stock.item", "I2", map[string]any{"name": "Wrench", "qty": 2, "line": "L2", "kind": "tool"}), "ERROR_CODE_INVALID_ARGUMENT"}, // a tool needs its serial
		{do("bo", "stock.item.create", "stock.item", "I2", map[string]any{"name": "Wrench", "qty": 2, "line": "L2", "kind": "tool", "serial": "W-7"}), "ok"},
		{do("ana", "stock.item.create", "stock.item", "I9", map[string]any{"name": "Washer", "kind": "part", "serial": "no"}), "ERROR_CODE_INVALID_ARGUMENT"}, // a part has none
		{do("bo", "stock.item.edit", "stock.item", "I2", map[string]any{"kind": "part"}), "ERROR_CODE_INVALID_ARGUMENT"},                                      // the serial would be left behind
		{do("bo", "stock.item.edit", "stock.item", "I2", map[string]any{"kind": "part", "serial": ""}), "ok"},
		{do("bo", "stock.item.edit", "stock.item", "I2", map[string]any{"kind": "tool", "serial": "W-7"}), "ok"},
		{do("ana", "stock.item.create", "stock.item", "I3", map[string]any{"name": "Nut", "qty": 40, "line": "L1", "kind": "part", "bin": "B1"}), "ok"},
		{do("ana", "stock.item.create", "stock.item", "I4", map[string]any{"name": "x", "kind": "gadget"}), "ERROR_CODE_INVALID_ARGUMENT"}, // not a choice
		{do("ana", "stock.item.create", "stock.item", "I4", map[string]any{"name": "x", "bin": "B9"}), "ERROR_CODE_INVALID_REFERENCE"},     // no such bin
		{do("ana", "stock.item.create", "stock.item", "I4", map[string]any{"name": "x", "due": "1 Oct"}), "ERROR_CODE_INVALID_ARGUMENT"},   // not a date
		{do("ana", "stock.item.create", "stock.item", "I4", map[string]any{"qty": 1}), "ERROR_CODE_INVALID_ARGUMENT"},                      // required name
		{do("ana", "stock.item.create", "stock.item", "I4", map[string]any{"name": "x", "owner": "ana"}), "ERROR_CODE_INVALID_ARGUMENT"},   // read-only
		{do("ana", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 7}), "ok"},
		{do("op", "stock.item.count", "stock.item", "I1", map[string]any{"qty": 6}), "ok"},
		{do("ana", "stock.item.archive", "stock.item", "I3", struct{}{}), "ok"},
	} {
		if c.got != c.want {
			t.Fatalf("got %s, want %s", c.got, c.want)
		}
	}
	ids := func(who string, q platform.Query) string {
		page, err := tn.Records(member(who), "stock.item", q, now)
		if err != nil {
			return err.Error()
		}
		var out []string
		for _, r := range page.Records {
			out = append(out, r.(Item).ID)
		}
		return fmt.Sprint(page.Total, out)
	}
	domain := func(d ...any) json.RawMessage { raw, _ := json.Marshal(d); return raw }
	for _, c := range []struct {
		who  string
		q    platform.Query
		want string
	}{
		{"lead", platform.Query{}, "2 [I1 I2]"}, // archived records are left out
		{"lead", platform.Query{Archived: true}, "3 [I1 I2 I3]"},
		{"lead", platform.Query{Sort: []string{"-qty"}, Archived: true}, "3 [I3 I1 I2]"},
		{"lead", platform.Query{Domain: domain([]any{"kind", "=", "part"}), Archived: true}, "2 [I1 I3]"},
		{"lead", platform.Query{Domain: domain("|", []any{"qty", ">", 10}, []any{"name", "like", "WRE"}), Archived: true}, "2 [I2 I3]"},
		{"lead", platform.Query{Domain: domain("!", []any{"line", "in", []string{"L1"}})}, "1 [I2]"},
		{"lead", platform.Query{Domain: domain([]any{"tags", "=", "steel"})}, "1 [I1]"},
		{"lead", platform.Query{Domain: domain([]any{"price", ">=", 100})}, "1 [I1]"},
		{"lead", platform.Query{Domain: domain([]any{"due", "<", "2026-12-01"})}, "1 [I1]"},
		{"lead", platform.Query{Search: "bol"}, "1 [I1]"},
		{"lead", platform.Query{Sort: []string{"name"}, Offset: 1, Limit: 1}, "2 [I2]"},
		{"lead", platform.Query{Domain: domain([]any{"nope", "=", 1})}, "ERROR_CODE_INVALID_ARGUMENT"},
		{"lead", platform.Query{Domain: domain([]any{"created", ">=", "2026-09-25"}, []any{"created", "<", "2026-09-26"})}, "2 [I1 I2]"}, // a drill-down into a day
		{"lead", platform.Query{Domain: domain([]any{"changed", "<", "2026-09-25"})}, "0 []"},
		// Scope (D4): a clerk sees their own, a line member their line, a lead the plant and below.
		{"ana", platform.Query{Archived: true}, "2 [I1 I3]"},
		{"bo", platform.Query{}, "1 [I2]"},
		{"op", platform.Query{}, "1 [I1]"},
		{"boss", platform.Query{}, "0 []"}, // role line, but a member of the plant, not of a line
	} {
		if got := ids(c.who, c.q); got != c.want {
			t.Fatalf("%s %+v: %s, want %s", c.who, c.q, got, c.want)
		}
	}
	view, err := tn.RecordOf(member("lead"), "stock.item", "I1", now)
	if err != nil {
		t.Fatal(err)
	}
	item := view.Record.(Item)
	if item.Qty != 6 || item.Owner != "ana" || item.Revision != 3 || item.Created.By != "ana" || item.Changed.By != "op" {
		t.Fatalf("item %+v", item)
	}
	var changes []string
	for _, h := range view.History {
		for _, f := range h.Fields {
			changes = append(changes, h.Schema+":"+f.Field+"="+string(f.After))
		}
	}
	if !slices.Contains(changes, "stock.item.count:qty=6") || !slices.Contains(changes, "stock.item.edit:qty=7") || view.History[2].Schema != "stock.item.create" {
		t.Fatalf("history %v", changes)
	}
	if _, err := tn.RecordOf(member("bo"), "stock.item", "I1", now); err == nil {
		t.Fatal("bo read a record outside his scope")
	}
	bin, _ := tn.RecordOf(member("lead"), "stock.bin", "B1", now)
	if len(bin.Related) != 1 || bin.Related[0].Total != 1 { // I3 is archived
		t.Fatalf("related %+v", bin.Related)
	}
	// Aggregates (ADR-0019 D1): the list's domain and scope, grouped and measured.
	agg := func(who string, q AggregateQuery) string {
		out, err := tn.Aggregate(member(who), "stock.item", q, now)
		if err != nil {
			return err.Error()
		}
		raw, _ := json.Marshal(out.Rows)
		return string(raw)
	}
	for _, c := range []struct {
		who  string
		q    AggregateQuery
		want string
	}{
		{"lead", AggregateQuery{}, `[{"count":2}]`},
		{"lead", AggregateQuery{Groups: []string{"line"}, Measures: []string{"count", "sum:qty"}, Archived: true}, `[{"count":2,"line":"L1","sum:qty":46},{"count":1,"line":"L2","sum:qty":2}]`},
		{"lead", AggregateQuery{Groups: []string{"kind", "due:month"}}, `[{"count":1,"due:month":"2026-10","kind":"part"},{"count":1,"due:month":"","kind":"tool"}]`},
		{"lead", AggregateQuery{Measures: []string{"sum:price", "avg:qty", "max:qty"}}, `[{"avg:qty":2,"max:qty":2,"price.currency":"","sum:price":0},{"avg:qty":6,"max:qty":6,"price.currency":"EUR","sum:price":150}]`},
		{"lead", AggregateQuery{Groups: []string{"created:month"}, Domain: domain([]any{"kind", "=", "tool"})}, `[{"count":1,"created:month":"2026-09"}]`},
		{"ana", AggregateQuery{Groups: []string{"line"}, Archived: true}, `[{"count":2,"line":"L1"}]`}, // scope: never a record ana could not list
		{"bo", AggregateQuery{Measures: []string{"sum:qty"}}, `[{"sum:qty":2}]`},
		{"lead", AggregateQuery{Groups: []string{"tags"}}, "ERROR_CODE_INVALID_ARGUMENT"},
		{"lead", AggregateQuery{Measures: []string{"sum:name"}}, "ERROR_CODE_INVALID_ARGUMENT"},
		{"lead", AggregateQuery{Groups: []string{"kind:month"}}, "ERROR_CODE_INVALID_ARGUMENT"},
		{"lead", AggregateQuery{Groups: []string{"line:month"}}, `[{"count":2,"line:month":""}]`}, // text that is not a date has no bucket
	} {
		if got := agg(c.who, c.q); got != c.want {
			t.Errorf("%s %+v: %s, want %s", c.who, c.q, got, c.want)
		}
	}
	CheckReplay(t, tn, journal, func() *Tenant { return stockTenant(t) })
}
