package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// shop is a test app: orders reserved, paid, packed and shipped by a flow.
// An item named "broken" cannot ship, "none" cannot be reserved, and "stuck"
// cannot be released either.
type ShopOrder struct {
	platform.Record
	Item   string `json:"item" field:"required"`
	Status string `json:"status" field:"readonly" choices:"placed,reserved,paid,labelled,billed,shipped,released"`
	Owner  string `json:"owner" field:"readonly"`
}

type shop struct {
	ledger *platform.Ledger
	flows  []platform.Flow
}

func shopEntities() []platform.Entity {
	return []platform.Entity{{Type: "shop.order", Title: "Order", Model: ShopOrder{}}}
}

func newShop(tenant string, flows ...platform.Flow) *shop {
	var actions []platform.Action
	for _, schema := range []string{"place", "reserve", "release", "pay", "label", "bill", "ship"} {
		actions = append(actions, platform.Action{Schema: "shop.order." + schema, Target: "shop.order", Capability: "orders", Title: schema,
			Description: schema + " an order", Payload: []platform.Field{}, Roles: []string{"clerk"}})
	}
	return &shop{ledger: platform.NewLedger(tenant, "shop", platform.NewCatalog(actions...), "shop.order"), flows: flows}
}

func (s *shop) Manifest() platform.Manifest {
	return platform.Manifest{ID: "shop", Version: "1", Actions: s.ledger.Catalog, Entities: shopEntities(), Flows: s.flows}
}
func (s *shop) Declarations() []*pb.AuthorityDeclaration { return s.ledger.Declarations() }
func (s *shop) Snapshot() (json.RawMessage, error)       { return s.ledger.Snapshot() }
func (s *shop) Restore(raw json.RawMessage) error        { return s.ledger.Restore(raw) }
func (s *shop) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (s *shop) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
func (s *shop) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return s.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		id, schema := sub.GetTarget().GetId(), strings.TrimPrefix(sub.GetSchema().GetName(), "shop.order.")
		o, known := platform.Get[ShopOrder](c, id)
		refuse := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		switch {
		case schema == "place":
			if known {
				return nil, refuse
			}
			var p struct{ Item string }
			json.Unmarshal(sub.GetPayload(), &p)
			o = ShopOrder{Record: platform.Record{ID: id}, Item: p.Item, Status: "placed"}
		case !known:
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		case schema == "reserve" && o.Item == "none", schema == "ship" && o.Item == "broken", schema == "release" && o.Item == "stuck":
			return nil, refuse
		default:
			o.Status = map[string]string{"reserve": "reserved", "release": "released", "pay": "paid", "label": "labelled", "bill": "billed", "ship": "shipped"}[schema]
		}
		return func(r *pb.ChangeRecord) { c.Put(r, o) }, nil
	})
}

func order(r *platform.Run) string { return r.Key }

func shopAct(action string) *platform.Act {
	return &platform.Act{Action: "shop.order." + action, Target: func(_ platform.Caller, r *platform.Run) string { return order(r) }}
}

var clerks = func(platform.Caller, *platform.Run) []platform.Recipient { return []platform.Recipient{{AppRole: "clerk"}} }

// fulfil v1: reserve, wait for payment (an hour, then chase), ship.
func fulfil(version int) platform.Flow {
	f := platform.Flow{Name: "fulfil", Title: "Fulfil order", Version: version, Owners: []string{"clerk"},
		Start: platform.Start{On: "shop.order.place", Begin: func(_ platform.Caller, e platform.Event) (string, any, bool) {
			return e.Record.GetSubmission().GetTarget().GetId(), map[string]string{"placed": "yes"}, true
		}},
		Steps: []platform.Step{
			{Name: "reserve", Act: shopAct("reserve"), Undo: shopAct("release"), Next: "paid"},
			{Name: "paid", Next: "ship", Timeout: time.Hour, OnTimeout: "chase", Wait: &platform.Wait{On: "shop.order.pay",
				Match: func(_ platform.Caller, r *platform.Run, e platform.Event) bool { return e.Record.GetSubmission().GetTarget().GetId() == order(r) }}},
			{Name: "chase", Ask: &platform.Ask{Title: func(_ platform.Caller, r *platform.Run) string { return "Chase payment of " + order(r) }, To: clerks,
				Answers: []string{"wait", "cancel"}},
				Choose: func(_ platform.Caller, r *platform.Run) (string, string) {
					if r.Answer == "cancel" {
						return platform.Compensate, "the clerk canceled"
					}
					return "paid", "the clerk waits"
				}},
			{Name: "ship", Act: shopAct("ship")},
		}}
	if version == 2 { // pack in parallel before shipping: a label, and the bill through a sub-flow
		f.Steps[1].Next = "pack"
		f.Steps = append(f.Steps, platform.Step{Name: "pack", All: []string{"label", "invoice"}, Next: "ship"},
			platform.Step{Name: "label", Act: shopAct("label")},
			platform.Step{Name: "invoice", Call: &platform.Call{Flow: "bill"}})
		f.From = map[string]string{"reserve": "reserve", "paid": "paid", "chase": "chase", "ship": "ship"}
	}
	return f
}

// bill is called by fulfil v2: it bills the order in the evening.
var bill = platform.Flow{Name: "bill", Title: "Bill", Version: 1,
	Start: platform.Start{On: "shop.order.bill", Begin: func(platform.Caller, platform.Event) (string, any, bool) { return "", nil, false }},
	Steps: []platform.Step{
		{Name: "later", Wait: &platform.Wait{At: func(_ platform.Caller, r *platform.Run) time.Time { return time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC) }}, Next: "bill"},
		{Name: "bill", Act: &platform.Act{Action: "shop.order.bill", Target: func(c platform.Caller, r *platform.Run) string {
			id, _, _ := strings.Cut(r.Key, "/") // the caller's key, "<flow>:<order>/<n>"
			return strings.TrimPrefix(id, "shop.fulfil:")
		}}},
	}}

func TestFlows(t *testing.T) {
	var journal []Entry
	build := func(flows ...platform.Flow) *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{"shop": role, FlowApp: FlowAdmin}}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("ana", "clerk"), seat("bo", "clerk")), NewWork("t-1"), NewFlows("t-1"), newShop("t-1", flows...))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build(fulfil(1))
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, authority, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	place := func(id, item string) { do("ana", "shop", "shop.order.place", "shop.order", id, map[string]string{"item": item}) }
	tick := func(d time.Duration) { // time passes; the host works every second of it that matters
		for end := now.Add(d); !now.After(end); now = now.Add(time.Second) {
			tn.Work(now)
		}
	}
	instance := func(id string) FlowInstance {
		x, _ := platform.Get[FlowInstance](tn.automation(FlowApp, false), id)
		return x
	}
	status := func(id string) string {
		o, _ := platform.Get[ShopOrder](tn.automation("shop", false), id)
		return o.Status
	}
	trace := func(id string) string {
		var out []string
		for _, l := range instance(id).Trace {
			out = append(out, strings.TrimSpace(l.Step+" "+l.What))
		}
		return strings.Join(out, ", ")
	}
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s:\n got %s\nwant %s", what, got, want)
		}
	}
	tasks := func(who string) []WorkTask {
		out, _ := tn.Read(member(who), "inbox")
		return out.([]WorkTask)
	}

	// An order placed starts the flow: reserved, then waiting for payment; paid, it ships.
	place("O1", "apple")
	tick(time.Second)
	expect("O1 waits", instance("shop.fulfil:O1").State+" "+status("O1")+" / "+trace("shop.fulfil:O1"), "waiting reserved / started, reserve acted, paid waiting")
	expect("pay", do("bo", "shop", "shop.order.pay", "shop.order", "O1", struct{}{}), "ok")
	tick(time.Second)
	expect("O1 done", instance("shop.fulfil:O1").State+" "+status("O1"), "done shipped")
	expect("on behalf of", instance("shop.fulfil:O1").OnBehalf, "ana")

	// Unpaid for an hour: a clerk is asked; answering cancel undoes the reservation.
	place("O2", "pear")
	tick(time.Hour + 2*time.Second)
	inbox := tasks("bo")
	expect("chase task", fmt.Sprintf("%d %s %v", len(inbox), inbox[0].Title, inbox[0].Answers), "1 Chase payment of O2 [wait cancel]")
	expect("a wrong answer", do("bo", WorkApp, "work.task.complete", TaskType, inbox[0].ID, map[string]string{"answer": "maybe"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect("cancel", do("bo", WorkApp, "work.task.complete", TaskType, inbox[0].ID, map[string]string{"answer": "cancel"}), "ok")
	tick(time.Second)
	expect("O2 compensated", instance("shop.fulfil:O2").State+" "+status("O2")+" / "+trace("shop.fulfil:O2"),
		"compensated released / started, reserve acted, paid waiting, paid timeout, chase asked, chase answered, chase chose, compensating, reserve undone, ended")

	// Shipping fails every time: retried with backoff, then the reservation is undone.
	place("O3", "broken")
	tick(time.Second)
	do("ana", "shop", "shop.order.pay", "shop.order", "O3", struct{}{})
	tick(time.Minute)
	expect("O3 compensated", instance("shop.fulfil:O3").State+" "+status("O3"), "compensated released")
	expect("O3 retried", fmt.Sprint(strings.Count(trace("shop.fulfil:O3"), "ship retry")), "4")

	// Nothing to undo when the first act fails; stuck when an undo keeps failing:
	// the owners are asked; an administrator skips the undo, and the flow ends.
	place("O4", "none")
	tick(time.Minute)
	expect("O4", instance("shop.fulfil:O4").State+" "+status("O4"), "compensated placed")
	do("ana", "shop", "shop.order.place", "shop.order", "O5", map[string]string{"item": "stuck"})
	tick(time.Second)
	now = now.Add(2 * time.Hour) // unpaid past the chase; the clerk cancels; the release is refused
	tick(time.Second)
	chase := slices.IndexFunc(tasks("ana"), func(w WorkTask) bool { return w.Title == "Chase payment of O5" })
	do("ana", WorkApp, "work.task.complete", TaskType, tasks("ana")[chase].ID, map[string]string{"answer": "cancel"})
	tick(time.Minute)
	stuck := instance("shop.fulfil:O5")
	expect("O5 stuck", stuck.State+" "+fmt.Sprint(slices.ContainsFunc(tasks("bo"), func(w WorkTask) bool { return w.Title == "Flow Fulfil order O5 is stuck" })), "stuck true")
	expect("skip", do("ana", FlowApp, SchemaFlowSkip, InstanceType, "shop.fulfil:O5", map[string]int{"token": stuck.Tokens[0].ID}), "ok")
	expect("O5 ended", instance("shop.fulfil:O5").State+" "+status("O5"), "compensated reserved")
	expect("stuck task closed", fmt.Sprint(slices.ContainsFunc(tasks("bo"), func(w WorkTask) bool { return strings.HasPrefix(w.Title, "Flow") })), "false")

	// One running instance per key; a new one once it ended.
	place("O6", "fig")
	tick(time.Second)
	expect("cancel O6", do("ana", FlowApp, SchemaFlowStop, InstanceType, "shop.fulfil:O6", struct{}{}), "ok")
	expect("canceled", instance("shop.fulfil:O6").State+" "+status("O6"), "canceled reserved")

	// Version 2 ships, O7 still waits on version 1: it keeps it; moved, it packs in
	// parallel (a label, and the bill through a sub-flow after its time).
	place("O7", "plum")
	tick(time.Second)
	CheckReplay(t, tn, journal, func() *Tenant { return build(fulfil(1)) })
	v2 := build(fulfil(1), bill, fulfil(2))
	if err := v2.Replay(journal); err != nil {
		t.Fatal(err)
	}
	tn, journal = v2, slices.Clone(journal)
	tn.Record = func(e Entry) { journal = append(journal, e) }
	if err := tn.flows.Check(); err != nil {
		t.Fatal(err)
	}
	expect("pinned", fmt.Sprint(instance("shop.fulfil:O7").Version), "1")
	expect("move", do("ana", FlowApp, SchemaFlowMove, InstanceType, "shop.fulfil:O7", struct{}{}), "ok")
	expect("moved", fmt.Sprint(instance("shop.fulfil:O7").Version), "2")
	do("bo", "shop", "shop.order.pay", "shop.order", "O7", struct{}{})
	tick(time.Second)
	expect("packing", instance("shop.fulfil:O7").State+" "+status("O7"), "waiting labelled")
	now = time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC) // the bill's time, reached by the timer
	tick(2 * time.Second)
	expect("O7 done", instance("shop.fulfil:O7").State+" "+status("O7")+" "+instance("shop.fulfil:O7/3").State, "done shipped done")
	expect("O7 trace", trace("shop.fulfil:O7"), "started, reserve acted, paid waiting, moved, paid event, pack branched, label acted, invoice called, invoice returned, pack joined, ship acted, ended")

	// A journal with an instance on a version the code dropped refuses to start.
	place("O8", "kiwi")
	tick(time.Second)
	CheckReplay(t, tn, journal, func() *Tenant { return build(fulfil(1), bill, fulfil(2)) })
	dropped := build(bill, platform.Flow{Name: "fulfil", Title: "Fulfil order", Version: 3, Start: fulfil(1).Start, Steps: fulfil(1).Steps})
	if err := dropped.Replay(journal); err == nil {
		if err := dropped.flows.Check(); err == nil || !strings.Contains(err.Error(), "version 2") {
			t.Fatalf("a dropped version was not refused: %v", err)
		}
	}

	// Declarations are checked at composition.
	for _, bad := range []platform.Flow{
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: "shop.order.place", Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Next: "b", Act: shopAct("ship")}}},
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: "crm.won", Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Act: shopAct("ship")}}},
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: "shop.order.place", Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Act: shopAct("ship"), Ask: &platform.Ask{}}}},
		{Name: "x", Title: "X", Version: 1, Start: platform.Start{On: "shop.order.place", Begin: fulfil(1).Start.Begin}, Steps: []platform.Step{{Name: "a", Wait: &platform.Wait{On: "shop.order.pay"}, Timeout: time.Hour}}},
	} {
		if _, err := NewTenant("t", NewFlows("t"), newShop("t", bad)); err == nil {
			t.Errorf("flow accepted: %+v", bad.Steps)
		}
	}
	if _, err := NewTenant("t", newShop("t", fulfil(1))); err == nil {
		t.Error("flows without a flow app were accepted")
	}
}
