package mes

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Sample is one equipment state reading from a PLC (via the edge gateway).
type Sample struct {
	At    time.Time `json:"at"`
	State string    `json:"state"` // run, idle, down
}

// StateBatch is one gateway delivery: many samples, one provenance (K3).
type StateBatch struct {
	BatchID  string   `json:"batchId"`
	Resource string   `json:"resource"`
	Samples  []Sample `json:"samples"`
}

// Downtime is derived from samples (K2 derived); its identity is a K1 entity so
// decisions about it (the reason) survive recomputation through redirects.
type Downtime struct {
	ID         string     `json:"id"`
	Resource   string     `json:"resource"`
	Start      time.Time  `json:"start"`
	End        *time.Time `json:"end,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	NeedsCheck bool       `json:"needsCheck,omitempty"` // a reason was given for an event that later split
}

// Event IDs are opaque and never reused (K1). Deriving them from the start time
// would revive a retired ID when a split recreates an event at the same start.
func resourceOfEvent(id string) string { resource, _, _ := strings.Cut(id, "#"); return resource }

// DeliverStates records a gateway batch as one observation and re-derives
// downtime; a downtime that starts tells the supervisors of its line, when the
// plant's setting says so (ADR-0013).
func (p *Plant) DeliverStates(gateway platform.Caller, b StateBatch, now time.Time) (*pb.FactRecord, *kernel.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if gateway.Tenant != p.tenant || roleOf(gateway) != Gateway && !gateway.Replaying {
		return nil, denied
	}
	if b.BatchID == "" || b.Resource == "" || len(b.Samples) == 0 || p.resourceLine(b.Resource) == "" {
		return nil, invalid
	}
	if err := gateway.Deliver(ResourceType, "", "", now); err != nil {
		return nil, err
	}
	slices.SortFunc(b.Samples, func(x, y Sample) int { return x.At.Compare(y.At) })
	raw, _ := json.Marshal(b)
	fact, err := p.facts.Record(&pb.Fact{TenantId: p.tenant, Kind: pb.FactKind_FACT_KIND_OBSERVATION,
		Subject: &pb.EntityRef{Type: ResourceType, Id: b.Resource}, Attribute: "state",
		Schema: &pb.SchemaRef{Name: schemaStates, Version: 1}, IdempotencyKey: gateway.ID + ":" + b.BatchID, Payload: raw,
		Provenance: &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: gateway.ID}, SourceTime: timestamppb.New(b.Samples[0].At)}}, now)
	if err != nil {
		return nil, err
	}
	started := p.deriveDowntime(b.Resource)
	if gateway.Setting(SettingNotifyDowntime) == "true" {
		for _, d := range started {
			gateway.Notify(platform.Notification{Title: "Downtime on " + d.Resource,
				Body: fmt.Sprintf("Line %s, since %s UTC. Give it a reason.", p.resourceLine(d.Resource), d.Start.UTC().Format("15:04")),
				Ref:  DowntimeType + "/" + d.ID, Key: "down:" + d.ID}, now, p.supervisorsOf(d.Resource))
		}
	}
	return fact, nil
}

// supervisorsOf are whoever supervises the resource's line or a unit above it (ADR-0012).
func (p *Plant) supervisorsOf(resource string) platform.Recipient {
	return platform.Recipient{Structure: SiteStructure, Unit: p.resourceLine(resource), Role: string(Supervisor)}
}

// deriveDowntime recomputes a resource's downtime from all its samples. Events
// whose start moved are merged into the new event; events cut in two are split,
// so decisions made about the old event keep resolving (K1 redirects). It
// returns the events that started: those overlapping no previous event.
func (p *Plant) deriveDowntime(resource string) (started []Downtime) {
	var samples []Sample
	for _, r := range p.facts.Records(p.tenant) {
		if r.GetFact().GetSubject().GetId() == resource && r.GetFact().GetSchema().GetName() == schemaStates {
			var b StateBatch
			json.Unmarshal(r.GetFact().GetPayload(), &b)
			samples = append(samples, b.Samples...)
		}
	}
	slices.SortFunc(samples, func(x, y Sample) int { return x.At.Compare(y.At) })
	var events []Downtime
	for i, s := range samples {
		down := s.State == "down"
		wasDown := i > 0 && samples[i-1].State == "down"
		switch {
		case down && !wasDown:
			events = append(events, Downtime{Resource: resource, Start: s.At})
		case !down && wasDown:
			end := s.At
			events[len(events)-1].End = &end
		}
	}
	// Match to the previous events by overlap: one-to-one keeps the entity (its
	// bounds moved); one old to several new is a split; several old to one new a merge.
	old := p.downtime[resource]
	overlapping := func(e Downtime, in []Downtime) []int {
		var out []int
		for i, x := range in {
			if overlaps(e, x) {
				out = append(out, i)
			}
		}
		return out
	}
	for i := range events {
		if prev := overlapping(events[i], old); len(prev) == 1 && len(overlapping(old[prev[0]], events)) == 1 {
			events[i].ID = old[prev[0]].ID
			continue
		}
		p.nextEvent++
		events[i].ID = fmt.Sprintf("%s#%d", resource, p.nextEvent)
		p.identity.Create(&pb.EntityRef{Type: DowntimeType, Id: events[i].ID}) // fresh opaque IDs never collide
		if len(overlapping(events[i], old)) == 0 {
			started = append(started, events[i])
		}
	}
	for _, o := range old {
		if slices.ContainsFunc(events, func(e Downtime) bool { return e.ID == o.ID }) {
			continue
		}
		var to []*pb.EntityRef
		for _, i := range overlapping(o, events) {
			to = append(to, &pb.EntityRef{Type: DowntimeType, Id: events[i].ID})
		}
		kind := pb.RedirectKind_REDIRECT_KIND_MERGE
		if len(to) > 1 {
			kind = pb.RedirectKind_REDIRECT_KIND_SPLIT
		}
		if len(to) > 0 {
			p.identity.AddRedirect(&pb.Redirect{From: &pb.EntityRef{Type: DowntimeType, Id: o.ID}, To: to, Kind: kind})
		}
	}
	p.downtime[resource] = events
	return started
}

func overlaps(a, b Downtime) bool {
	far := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	aEnd, bEnd := far, far
	if a.End != nil {
		aEnd = *a.End
	}
	if b.End != nil {
		bEnd = *b.End
	}
	return a.Start.Before(bEnd) && b.Start.Before(aEnd)
}

// Downtime lists current events with the reason decided for them: every reason
// decision whose target resolves to exactly this event, the latest winning.
func (p *Plant) Downtime() []Downtime {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Downtime
	for _, events := range p.downtime {
		out = append(out, events...)
	}
	for _, r := range p.ledger.Changes.Records(p.tenant) {
		if r.GetSubmission().GetSchema().GetName() != SchemaReason {
			continue
		}
		var reason reasonPayload
		json.Unmarshal(r.GetSubmission().GetPayload(), &reason)
		refs, _ := p.identity.Resolve(r.GetSubmission().GetTarget())
		for i := range out {
			if slices.ContainsFunc(refs, func(ref kernel.Ref) bool { return ref.ID == out[i].ID }) {
				if len(refs) == 1 {
					out[i].Reason, out[i].NeedsCheck = reason.Reason, false
				} else if out[i].Reason == "" {
					out[i].NeedsCheck = true
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b Downtime) int { return cmp.Or(a.Start.Compare(b.Start), compare(a.ID, b.ID)) }) // deterministic: events can start together
	return out
}

// ERP planned orders arrive by polling (K8 poll): each page is exactly-once by cursor,
// and every planned order is a claim the supervisor may release against (K4 C11).

type PlannedOrder struct {
	ERPID    string `json:"erpId"`
	Product  string `json:"product"`
	Quantity int    `json:"quantity"`
	Due      string `json:"due"`
	FactID   string `json:"factId,omitempty"`
}

type PlannedPage struct {
	CursorFrom string         `json:"cursorFrom"`
	CursorTo   string         `json:"cursorTo"`
	Orders     []PlannedOrder `json:"orders"`
}

func (p *Plant) DeliverPlanned(erp platform.Caller, page PlannedPage, now time.Time) *kernel.Error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if erp.Tenant != p.tenant || roleOf(erp) != ERP && !erp.Replaying {
		return denied
	}
	if err := erp.Deliver(PlannedType, page.CursorFrom, page.CursorTo, now); err != nil {
		return err
	}
	for _, o := range page.Orders {
		raw, _ := json.Marshal(o)
		p.facts.Record(&pb.Fact{TenantId: p.tenant, Kind: pb.FactKind_FACT_KIND_CLAIM,
			Subject: &pb.EntityRef{Type: PlannedType, Id: o.ERPID}, Attribute: "demand",
			Schema: &pb.SchemaRef{Name: schemaPlanned, Version: 1}, IdempotencyKey: erp.ID + ":" + o.ERPID + ":" + page.CursorTo, Payload: raw,
			Provenance: &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: erp.ID}, SourceTime: timestamppb.New(now), Confidence: 1}}, now)
	}
	return nil
}

// Planned lists current ERP claims (latest per planned order) with their fact IDs.
func (p *Plant) Planned() []PlannedOrder {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []PlannedOrder
	seen := map[string]bool{}
	records := p.facts.Records(p.tenant)
	for i := len(records) - 1; i >= 0; i-- {
		f := records[i].GetFact()
		if f.GetSchema().GetName() != schemaPlanned || seen[f.GetSubject().GetId()] {
			continue
		}
		seen[f.GetSubject().GetId()] = true
		var o PlannedOrder
		json.Unmarshal(f.GetPayload(), &o)
		o.FactID = records[i].GetFactId()
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b PlannedOrder) int { return compare(a.ERPID, b.ERPID) })
	return out
}
