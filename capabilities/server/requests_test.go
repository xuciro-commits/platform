package platformserver

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// ADR-0026: a decision changes one app. Its rules probe another app; once it
// is accepted, its requests run at the provider and their answers come back
// to the app's reply action. "none" cannot be held.

type Hold struct {
	platform.Record
	Item string `json:"item" field:"required"`
}

type Cart struct {
	platform.Record
	Item   string `json:"item" field:"required"`
	Status string `json:"status" field:"readonly" choices:"asked,held,refused"`
	Detail string `json:"detail,omitempty" field:"readonly"`
}

func testProtocol() platform.Protocol {
	return platform.Protocol{Name: "stock.hold", Version: 1, Actions: []platform.Action{{Schema: "hold", Title: "Hold", Description: "Hold an item.",
		Payload: []platform.Field{{Name: "item", Type: "string", Required: true, Description: "Item"}}}}}
}

type depot struct{ ledger *platform.Ledger }

func (s *depot) Manifest() platform.Manifest {
	return platform.Manifest{ID: "stock", Version: "1", Actions: s.ledger.Catalog,
		Entities: []platform.Entity{{Type: "stock.hold", Title: "Hold", Model: Hold{}}},
		Provides: []platform.Provision{{Protocol: testProtocol(), Actions: map[string]string{"hold": "stock.hold.make"}}}}
}
func (s *depot) Declarations() []*pb.AuthorityDeclaration { return s.ledger.Declarations() }
func (s *depot) Snapshot() (json.RawMessage, error)       { return s.ledger.Snapshot() }
func (s *depot) Restore(raw json.RawMessage) error        { return s.ledger.Restore(raw) }
func (s *depot) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, nil
}
func (s *depot) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, nil
}
func (s *depot) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return s.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var h Hold
		json.Unmarshal(sub.GetPayload(), &h)
		if h.Item == "none" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		h.ID = sub.GetTarget().GetId()
		return func(r *pb.ChangeRecord) { c.Put(r, h) }, nil
	})
}

type cart struct{ ledger *platform.Ledger }

func (a *cart) Manifest() platform.Manifest {
	return platform.Manifest{ID: "cart", Version: "1", Actions: a.ledger.Catalog,
		Entities: []platform.Entity{{Type: "cart.line", Title: "Line", Model: Cart{}}},
		Consumes: []platform.Consumption{{Protocol: testProtocol().ID(), Optional: true}}}
}
func (a *cart) Declarations() []*pb.AuthorityDeclaration { return a.ledger.Declarations() }
func (a *cart) Snapshot() (json.RawMessage, error)       { return a.ledger.Snapshot() }
func (a *cart) Restore(raw json.RawMessage) error        { return a.ledger.Restore(raw) }
func (a *cart) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, nil
}
func (a *cart) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, nil
}
func (a *cart) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	id := s.GetTarget().GetId()
	return a.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		line, known := platform.Get[Cart](c, id)
		switch s.GetSchema().GetName() {
		case "cart.line.add", "cart.line.ask":
			var p struct{ Item string }
			json.Unmarshal(s.GetPayload(), &p)
			if s.GetSchema().GetName() == "cart.line.add" {
				if err := c.Probe(testProtocol().ID(), "hold", id, p, now); err != nil {
					return nil, err
				}
			}
			return func(r *pb.ChangeRecord) {
				c.Put(r, Cart{Record: platform.Record{ID: id}, Item: p.Item, Status: "asked"})
				c.Request(r, platform.Request{Protocol: testProtocol().ID(), Action: "hold", Target: id, Payload: p, Reply: "cart.line.answer"})
			}, nil
		case "cart.line.greedy": // probes, then refuses: nothing may be left behind
			if err := c.Probe(testProtocol().ID(), "hold", id, map[string]string{"item": "x"}, now); err != nil {
				return nil, err
			}
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		case "cart.line.answer":
			var ans platform.Answer
			if json.Unmarshal(s.GetPayload(), &ans) != nil || !known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			line.Status, line.Detail = map[string]string{"accepted": "held", "refused": "refused"}[ans.Outcome], ans.Code+ans.Ref
			return func(r *pb.ChangeRecord) { c.Put(r, line) }, nil
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	})
}

func newRequestsTenant(t *testing.T, withStock bool) *Tenant {
	var actions []platform.Action
	for _, schema := range []string{"add", "ask", "greedy", "answer"} {
		payload := []platform.Field{{Name: "item", Type: "string", Description: "Item"}}
		if schema == "answer" {
			payload = platform.AnswerFields()
		}
		actions = append(actions, platform.Action{Schema: "cart.line." + schema, Target: "cart.line", Title: schema, Description: schema + " a line",
			Payload: payload, Roles: []string{"buyer"}})
	}
	apps := []platform.App{NewConsole("t"), &cart{ledger: platform.NewLedger("t", "cart", platform.NewCatalog(actions...), "cart.line")}}
	if withStock {
		apps = append(apps, &depot{ledger: platform.NewLedger("t", "stock", platform.NewCatalog(platform.Action{Schema: "stock.hold.make", Target: "stock.hold",
			Title: "Hold", Description: "Hold an item.", Payload: testProtocol().Actions[0].Payload, Roles: []string{"keeper"}}), "stock.hold")})
	}
	tn, err := NewTenant("t", apps...)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func TestRequestsAcrossApps(t *testing.T) {
	at := time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC)
	tn := newRequestsTenant(t, true)
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	buyer := platform.Member{ID: "b", Tenant: "t", Roles: map[string]string{"cart": "buyer", "stock": "keeper"}}
	cartOnly := platform.Member{ID: "c", Tenant: "t", Roles: map[string]string{"cart": "buyer"}}
	submit := func(m platform.Member, schema, id, item string) string {
		raw, _ := json.Marshal(map[string]string{"item": item})
		_, err := tn.Submit(m, &pb.Submission{TenantId: "t", PrincipalId: m.ID, Authority: "cart", Target: &pb.EntityRef{Type: "cart.line", Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: schema + id, Payload: raw}, at)
		if err != nil {
			return err.Code.String()
		}
		return "ok"
	}
	line := func(tn *Tenant, id string) Cart {
		v, err := tn.RecordOf(buyer, "cart.line", id, at)
		if err != nil {
			return Cart{}
		}
		return v.Record.(Cart)
	}
	holds := func(tn *Tenant) int {
		page, _ := tn.Records(buyer, "stock.hold", platform.Query{}, at)
		return page.Total
	}
	// Accepted, the request runs and its answer comes back in the same submission.
	if got := submit(buyer, "cart.line.add", "L1", "pen"); got != "ok" || line(tn, "L1").Status != "held" || line(tn, "L1").Detail != "stock.hold/L1" {
		t.Fatalf("add: %s %+v", got, line(tn, "L1"))
	}
	// The probe refuses at once what the provider would refuse: nothing is recorded.
	if got := submit(buyer, "cart.line.add", "L2", "none"); got != "ERROR_CODE_CONFLICT" || line(tn, "L2").ID != "" {
		t.Fatalf("probe: %s", got)
	}
	// Refused after probing, the decision leaves nothing at the provider.
	if got := submit(buyer, "cart.line.greedy", "L3", ""); got != "ERROR_CODE_CONFLICT" || holds(tn) != 1 {
		t.Fatalf("greedy: %s, %d holds", got, holds(tn))
	}
	// Unprobed, the provider's refusal is the answer, on the requester's record;
	// so is the provider's policy: the member holds no role there.
	if submit(buyer, "cart.line.ask", "L4", "none"); line(tn, "L4").Status != "refused" || line(tn, "L4").Detail != "ERROR_CODE_CONFLICT" {
		t.Fatalf("refused answer: %+v", line(tn, "L4"))
	}
	if submit(cartOnly, "cart.line.ask", "L5", "pen"); line(tn, "L5").Detail != "ERROR_CODE_POLICY_DENIED" || holds(tn) != 1 {
		t.Fatalf("policy at the provider: %+v", line(tn, "L5"))
	}
	CheckReplay(t, tn, journal, func() *Tenant { return newRequestsTenant(t, true) })

	// With no provider, the request is answered NOT_FOUND; the app works alone.
	alone := newRequestsTenant(t, false)
	tn = alone
	if submit(buyer, "cart.line.ask", "L6", "pen"); line(alone, "L6").Status != "refused" || line(alone, "L6").Detail != "ERROR_CODE_NOT_FOUND" {
		t.Fatalf("no provider: %+v", line(alone, "L6"))
	}
}
