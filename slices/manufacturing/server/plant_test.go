package mes

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
	"platformserver/platform"
)

var (
	tenant = "plant-sz"
	sup    = member("sup-1", Supervisor, "plant-sz") // the plant, so both lines below it
	op1    = member("op-l1", Operator, "L1")
	op2    = member("op-l2", Operator, "L2")
	qa1    = member("qa-1", Quality)
	qa2    = member("qa-2", Quality)
	gw     = member("gateway-l1", Gateway)
	erp    = member("erp", ERP)
	t0     = time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	keys   = 0
)

// units are the test members' memberships in the site structure.
var units []platform.Membership

// member is a member with a role in the plant, belonging to units (lines or the plant).
func member(id string, role Role, in ...string) platform.Caller {
	for _, u := range in {
		units = append(units, platform.Membership{Party: "member:" + id, Unit: u, Role: string(role)})
	}
	return platform.As("mes", platform.Member{ID: id, Tenant: tenant, Roles: map[string]string{"mes": string(role)}})
}

// testPlant sends every input through a tenant on the platform host, so it is
// journaled as in production (ADR-0010).
type testPlant struct {
	*Plant
	tenant  *platformserver.Tenant
	journal []platformserver.Entry
}

func (p *testPlant) Submit(who platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return p.tenant.Submit(who.Member, s, now)
}

func (p *testPlant) DeliverStates(who platform.Caller, b StateBatch, now time.Time) (*pb.FactRecord, *kernel.Error) {
	raw, _ := json.Marshal(b)
	out, err := p.tenant.Input(who.Member, "states", raw, now)
	fact, _ := out.(*pb.FactRecord)
	return fact, err
}

func (p *testPlant) DeliverPlanned(who platform.Caller, page PlannedPage, now time.Time) *kernel.Error {
	raw, _ := json.Marshal(page)
	_, err := p.tenant.Input(who.Member, "planned-orders", raw, now)
	return err
}

func plantTenant(t *testing.T, disable ...string) (*Plant, *platformserver.Tenant) {
	return plantTenantOf(t, slices.Clone(units), disable...)
}

// plantTenantOf composes the plant with the given memberships, so a replay gets
// the organisation its live tenant started with.
func plantTenantOf(t *testing.T, seed []platform.Membership, disable ...string) (*Plant, *platformserver.Tenant) {
	p := NewPlant(tenant, DemoMaster())
	for _, c := range disable {
		if !p.Disable(c) {
			t.Fatalf("no capability %s", c)
		}
	}
	tn, err := platformserver.NewTenant(tenant, platformserver.NewConsole(tenant), platformserver.NewOrganization(tenant, DemoOrganization(seed)), p)
	if err == nil {
		err = tn.Connect(DemoConnectors(tenant)...)
	}
	if err != nil {
		t.Fatal(err)
	}
	return p, tn
}

// newPlant journals every accepted input; when the test ends, a second plant
// replays the journal and must show the same state and kernel logs (ADR-0007).
func newPlant(t *testing.T) *testPlant {
	seed := slices.Clone(units)
	plant, tn := plantTenantOf(t, seed)
	p := &testPlant{Plant: plant, tenant: tn}
	tn.Record = func(e platformserver.Entry) {
		raw, _ := json.Marshal(e) // stored as JSON, as the PostgreSQL journal does
		var stored platformserver.Entry
		json.Unmarshal(raw, &stored)
		p.journal = append(p.journal, stored)
	}
	t.Cleanup(func() {
		platformserver.CheckReplay(t, p.tenant, p.journal, func() *platformserver.Tenant { _, tn := plantTenantOf(t, seed); return tn })
		again, tn := plantTenantOf(t, seed)
		if err := tn.Replay(p.journal); err != nil {
			t.Fatalf("replay: %v", err)
		}
		view := func(p *Plant) string {
			raw, _ := json.Marshal([]any{p.Orders(), p.SFCs(), p.Downtime(), p.Planned()}) // heartbeats are not journaled
			return string(raw)
		}
		if view(again) != view(plant) {
			t.Fatalf("replayed plant differs:\n%s\n%s", view(plant), view(again))
		}
		inbox := func(tn *platformserver.Tenant) string {
			out, _ := tn.Read(sup.Member, "notifications")
			raw, _ := json.Marshal(out)
			return string(raw)
		}
		if inbox(tn) != inbox(p.tenant) {
			t.Fatalf("replayed notifications differ:\n%s\n%s", inbox(p.tenant), inbox(tn))
		}
		logs := func(p *Plant) []proto.Message {
			var out []proto.Message
			for _, r := range p.ledger.Changes.Records(tenant) {
				out = append(out, r)
			}
			for _, r := range p.facts.Records(tenant) {
				out = append(out, r)
			}
			return out
		}
		a, b := logs(plant), logs(again)
		if len(a) != len(b) {
			t.Fatalf("replayed logs: %d records, want %d", len(b), len(a))
		}
		for i := range a {
			if !proto.Equal(a[i], b[i]) {
				t.Fatalf("replayed record %d differs:\n%v\n%v", i, a[i], b[i])
			}
		}
	})
	return p
}

func submit(p *testPlant, who platform.Caller, schema, targetType, id string, payload any, evidence ...string) string {
	return submitAt(p, who, schema, targetType, id, payload, nil, evidence...)
}

func submitAt(p *testPlant, who platform.Caller, schema, targetType, id string, payload any, revision *uint32, evidence ...string) string {
	raw, _ := json.Marshal(payload)
	keys++
	_, err := p.Submit(who, &pb.Submission{TenantId: who.Tenant, PrincipalId: who.ID, Authority: Authority,
		Target: &pb.EntityRef{Type: targetType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1},
		IdempotencyKey: fmt.Sprint("k", keys), Payload: raw, EvidenceFactIds: evidence, ExpectedRevision: revision}, t0)
	if err != nil {
		return err.Error()
	}
	return "ok"
}

func expect(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func sfc(p *testPlant, id string) SFC {
	for _, s := range p.SFCs() {
		if s.ID == id {
			return s
		}
	}
	return SFC{}
}

func TestOrderFromERPClaimThroughRouting(t *testing.T) {
	p := newPlant(t)
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, PlannedPage{CursorTo: "page-1",
		Orders: []PlannedOrder{{ERPID: "PO-9001", Product: "P-100", Quantity: 2, Due: "2026-10-01"}}}, t0)), "<nil>")
	claim := p.Planned()[0].FactID
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 2, SFCs: 2, Planned: "PO-9001"}, claim), "ok")
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-2", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}, "no-such-claim"), "ERROR_CODE_INVALID_REFERENCE")
	for _, resource := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
		expect(t, submit(p, op1, SchemaStart, SFCType, "SO-1-001", sfcPayload{Resource: resource}), "ok")
		expect(t, submit(p, op1, SchemaComplete, SFCType, "SO-1-001", sfcPayload{}), "ok")
	}
	expect(t, sfc(p, "SO-1-001").State, "done")
	stale := uint32(1)
	expect(t, submitAt(p, op1, SchemaStart, SFCType, "SO-1-002", sfcPayload{Resource: "FURNACE-1"}, &stale), "ERROR_CODE_CONFLICT") // stale screen (C12)
	expect(t, submit(p, op1, SchemaStart, SFCType, "SO-1-002", sfcPayload{Resource: "CNC-11"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect(t, fmt.Sprint(sfc(p, "SO-1-001").Revision), "6")
}

// F-8 refuted: plant hierarchy is policy context; the kernel needed no change.
func TestPolicyScopedByLine(t *testing.T) {
	p := newPlant(t)
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok")
	expect(t, submit(p, op2, SchemaStart, SFCType, "SO-1-001", sfcPayload{Resource: "FURNACE-1"}), "ERROR_CODE_POLICY_DENIED")
	expect(t, submit(p, op1, SchemaStart, SFCType, "SO-1-001", sfcPayload{Resource: "FURNACE-1"}), "ok")
	l2only := member("sup-2", Supervisor, "L2")
	expect(t, submit(p, l2only, SchemaRelease, OrderType, "SO-2", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ERROR_CODE_POLICY_DENIED")
}

// F-7 refuted: a two-signature disposition is a domain workflow over single decisions.
func TestDispositionNeedsTwoSignatures(t *testing.T) {
	p := newPlant(t)
	submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1})
	submit(p, op1, SchemaStart, SFCType, "SO-1-001", sfcPayload{Resource: "FURNACE-1"})
	submit(p, op1, SchemaComplete, SFCType, "SO-1-001", sfcPayload{})
	expect(t, submit(p, op1, SchemaNC, SFCType, "SO-1-001", sfcPayload{Code: "POROSITY"}), "ok")
	expect(t, sfc(p, "SO-1-001").State, "hold")
	expect(t, submit(p, op1, SchemaSign, SFCType, "SO-1-001", signPayload{Action: "rework", Meaning: "reviewed"}), "ERROR_CODE_POLICY_DENIED")
	expect(t, submit(p, qa1, SchemaSign, SFCType, "SO-1-001", signPayload{Action: "rework", Meaning: "reviewed", ReworkStep: 0}), "ok")
	expect(t, submit(p, qa1, SchemaSign, SFCType, "SO-1-001", signPayload{Action: "rework", Meaning: "approved", ReworkStep: 0}), "ERROR_CODE_CONFLICT")
	expect(t, sfc(p, "SO-1-001").State, "hold")
	expect(t, submit(p, qa2, SchemaSign, SFCType, "SO-1-001", signPayload{Action: "rework", Meaning: "approved", ReworkStep: 0}), "ok")
	s := sfc(p, "SO-1-001")
	if s.State != "queued" || s.Step != 0 {
		t.Fatalf("rework not applied: %+v", s)
	}
}

func states(start time.Time, pattern string) []Sample {
	names := map[rune]string{'r': "run", 'i': "idle", 'd': "down"}
	var out []Sample
	for i, c := range pattern {
		out = append(out, Sample{At: start.Add(time.Duration(i) * time.Minute), State: names[c]})
	}
	return out
}

// F-5 refuted: one provenance per batch; 600 samples are one observation.
func TestStateBatchIsOneObservation(t *testing.T) {
	p := newPlant(t)
	samples := make([]Sample, 600)
	for i := range samples {
		samples[i] = Sample{At: t0.Add(time.Duration(i) * 100 * time.Millisecond), State: "run"}
	}
	b := StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: samples}
	first, err := p.DeliverStates(gw, b, t0)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := p.DeliverStates(gw, b, t0.Add(time.Second))
	if again.GetFactId() != first.GetFactId() || len(p.facts.Records(tenant)) != 1 {
		t.Fatal("a redelivered batch was recorded twice")
	}
}

// F-6 refuted by K1: a derived event referenced by a decision is an entity; when
// late samples move or split it, redirects keep the decision attached.
func TestDowntimeReasonSurvivesRecomputation(t *testing.T) {
	p := newPlant(t)
	p.DeliverStates(gw, StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: states(t0.Add(2*time.Minute), "ddrr")}, t0)
	event := p.Downtime()[0].ID
	expect(t, submit(p, op1, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Tool change"}), "ok")
	// A buffered earlier batch arrives: the stop really began two minutes earlier.
	p.DeliverStates(gw, StateBatch{BatchID: "b-0", Resource: "CNC-11", Samples: states(t0, "dd")}, t0)
	events := p.Downtime()
	if len(events) != 1 || events[0].ID != event || !events[0].Start.Equal(t0) || events[0].Reason != "Tool change" {
		t.Fatalf("the moved event should keep its identity and reason: %+v", events)
	}
	// A late "run" sample inside the stop splits it: the old reason needs a check.
	p.DeliverStates(gw, StateBatch{BatchID: "b-2", Resource: "CNC-11", Samples: []Sample{{At: t0.Add(90 * time.Second), State: "run"}}}, t0)
	events = p.Downtime()
	if len(events) != 2 || !events[0].NeedsCheck || !events[1].NeedsCheck {
		t.Fatalf("split event should ask for a check: %+v", events)
	}
	expect(t, submit(p, op1, SchemaReason, DowntimeType, event, reasonPayload{Reason: "Tool change"}), "ERROR_CODE_CONFLICT")
	expect(t, submit(p, op1, SchemaReason, DowntimeType, events[1].ID, reasonPayload{Reason: "Material shortage"}), "ok")
	expect(t, p.Downtime()[1].Reason, "Material shortage")
	expect(t, submit(p, op2, SchemaReason, DowntimeType, events[0].ID, reasonPayload{Reason: "x"}), "ERROR_CODE_POLICY_DENIED")
}

// #97: a downtime that starts tells the supervisors of its line; one still
// without a reason reminds them once, after the plant's setting (ADR-0013).
func TestDowntimeTellsSupervisors(t *testing.T) {
	p := newPlant(t)
	inbox := func(who platform.Caller) []string {
		out, err := p.tenant.Read(who.Member, "notifications")
		if err != nil {
			t.Fatal(err)
		}
		titles := []string{}
		for _, n := range out.([]platform.Notification) {
			titles = append(titles, n.Title)
		}
		return titles
	}
	p.DeliverStates(gw, StateBatch{BatchID: "b-1", Resource: "CNC-11", Samples: states(t0.Add(2*time.Minute), "ddrr")}, t0)
	p.DeliverStates(gw, StateBatch{BatchID: "b-0", Resource: "CNC-11", Samples: states(t0, "dd")}, t0) // the same stop, begun earlier
	p.DeliverStates(gw, StateBatch{BatchID: "b-3", Resource: "CNC-21", Samples: states(t0, "rd")}, t0)
	expect(t, fmt.Sprint(inbox(sup)), "[Downtime on CNC-21 Downtime on CNC-11]")
	expect(t, fmt.Sprint(inbox(op1), inbox(op2)), "[] []") // operators are not supervisors
	for _, d := range p.Downtime() {
		if d.Resource == "CNC-21" {
			expect(t, submit(p, op2, SchemaReason, DowntimeType, d.ID, reasonPayload{Reason: "Setup"}), "ok")
		}
	}
	p.tenant.Work(t0.Add(10 * time.Minute)) // before the default 15 minutes
	p.tenant.Work(t0.Add(20 * time.Minute))
	p.tenant.Work(t0.Add(30 * time.Minute)) // reminded once only
	expect(t, fmt.Sprint(inbox(sup)), "[Downtime without a reason on CNC-11 Downtime on CNC-21 Downtime on CNC-11]")
	// An administrator turns the downtime notice off in Settings.
	admin := platform.Member{ID: "admin", Tenant: tenant, Roles: map[string]string{platformserver.PlatformApp: platformserver.Admin}}
	if _, err := p.tenant.Submit(admin, &pb.Submission{TenantId: tenant, PrincipalId: "admin", Authority: platformserver.PlatformApp, IdempotencyKey: "s1",
		Target: &pb.EntityRef{Type: platformserver.SettingType, Id: "mes/" + SettingNotifyDowntime}, Schema: &pb.SchemaRef{Name: platformserver.SchemaSettingSet, Version: 1},
		Payload: []byte(`{"value":"false"}`)}, t0); err != nil {
		t.Fatal(err)
	}
	p.DeliverStates(gw, StateBatch{BatchID: "b-4", Resource: "CNC-12", Samples: states(t0, "d")}, t0)
	expect(t, fmt.Sprint(len(inbox(sup))), "3")
}

// F-9 confirmed and resolved by K8: push and poll connectors share one descriptor.
func TestConnectors(t *testing.T) {
	p := newPlant(t)
	page := PlannedPage{CursorTo: "page-1", Orders: []PlannedOrder{{ERPID: "PO-1", Product: "P-200", Quantity: 5}}}
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, page, t0)), "<nil>")
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, page, t0)), "ERROR_CODE_CONFLICT") // the same page again
	expect(t, fmt.Sprint(len(p.Planned())), "1")
	views := p.tenant.Connectors(t0.Add(time.Minute))
	expect(t, views[0].ID+" "+views[0].Health+" "+views[0].Cursor, "erp ok page-1")
	expect(t, views[1].ID+" "+views[1].Health, "gateway-l1 stale")
}
