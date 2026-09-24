package mes

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

var (
	tenant = "plant-sz"
	sup    = Principal{ID: "sup-1", Tenant: tenant, Role: Supervisor, Lines: []string{"L1", "L2"}}
	op1    = Principal{ID: "op-l1", Tenant: tenant, Role: Operator, Lines: []string{"L1"}}
	op2    = Principal{ID: "op-l2", Tenant: tenant, Role: Operator, Lines: []string{"L2"}}
	qa1    = Principal{ID: "qa-1", Tenant: tenant, Role: Quality}
	qa2    = Principal{ID: "qa-2", Tenant: tenant, Role: Quality}
	gw     = Principal{ID: "gateway-l1", Tenant: tenant, Role: Gateway}
	erp    = Principal{ID: "erp", Tenant: tenant, Role: ERP}
	t0     = time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	keys   = 0
)

func newPlant(t *testing.T) *Plant {
	p := NewPlant(tenant, DemoMaster())
	for _, d := range DemoConnectors(tenant) {
		if err := p.RegisterConnector(d); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func submit(p *Plant, who Principal, schema, targetType, id string, payload any, evidence ...string) string {
	raw, _ := json.Marshal(payload)
	keys++
	_, err := p.Submit(who, &pb.Submission{TenantId: who.Tenant, PrincipalId: who.ID, Authority: Authority,
		Target: &pb.EntityRef{Type: targetType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1},
		IdempotencyKey: fmt.Sprint("k", keys), Payload: raw, EvidenceFactIds: evidence}, t0)
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

func sfc(p *Plant, id string) SFC {
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
	for step, resource := range []string{"FURNACE-1", "CNC-11", "CMM-1"} {
		expect(t, submit(p, op1, SchemaStart, SFCType, "SO-1-001", stepPayload{Step: step, Resource: resource}), "ok")
		expect(t, submit(p, op1, SchemaComplete, SFCType, "SO-1-001", stepPayload{Step: step}), "ok")
	}
	expect(t, sfc(p, "SO-1-001").State, "done")
	expect(t, submit(p, op1, SchemaStart, SFCType, "SO-1-002", stepPayload{Step: 1, Resource: "CNC-11"}), "ERROR_CODE_CONFLICT") // stale step
	expect(t, submit(p, op1, SchemaStart, SFCType, "SO-1-002", stepPayload{Step: 0, Resource: "CNC-11"}), "ERROR_CODE_INVALID_ARGUMENT")
}

// F-8 refuted: plant hierarchy is policy context; the kernel needed no change.
func TestPolicyScopedByLine(t *testing.T) {
	p := newPlant(t)
	expect(t, submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ok")
	expect(t, submit(p, op2, SchemaStart, SFCType, "SO-1-001", stepPayload{Step: 0, Resource: "FURNACE-1"}), "ERROR_CODE_POLICY_DENIED")
	expect(t, submit(p, op1, SchemaStart, SFCType, "SO-1-001", stepPayload{Step: 0, Resource: "FURNACE-1"}), "ok")
	l2only := Principal{ID: "sup-2", Tenant: tenant, Role: Supervisor, Lines: []string{"L2"}}
	expect(t, submit(p, l2only, SchemaRelease, OrderType, "SO-2", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1}), "ERROR_CODE_POLICY_DENIED")
}

// F-7 refuted: a two-signature disposition is a domain workflow over single decisions.
func TestDispositionNeedsTwoSignatures(t *testing.T) {
	p := newPlant(t)
	submit(p, sup, SchemaRelease, OrderType, "SO-1", releasePayload{Product: "P-100", Quantity: 1, SFCs: 1})
	submit(p, op1, SchemaStart, SFCType, "SO-1-001", stepPayload{Step: 0, Resource: "FURNACE-1"})
	submit(p, op1, SchemaComplete, SFCType, "SO-1-001", stepPayload{Step: 0})
	expect(t, submit(p, op1, SchemaNC, SFCType, "SO-1-001", stepPayload{Step: 1, Code: "POROSITY"}), "ok")
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

// F-9 confirmed and resolved by K8: push and poll connectors share one descriptor.
func TestConnectors(t *testing.T) {
	p := newPlant(t)
	page := PlannedPage{CursorTo: "page-1", Orders: []PlannedOrder{{ERPID: "PO-1", Product: "P-200", Quantity: 5}}}
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, page, t0)), "<nil>")
	expect(t, fmt.Sprint(p.DeliverPlanned(erp, page, t0)), "ERROR_CODE_CONFLICT") // the same page again
	expect(t, fmt.Sprint(len(p.Planned())), "1")
	views := p.Connectors(t0.Add(time.Minute))
	expect(t, views[0].ID+" "+views[0].Health, "erp CONNECTOR_HEALTH_OK")
	expect(t, views[1].ID+" "+views[1].Health, "gateway-l1 CONNECTOR_HEALTH_STALE")
}
