// Package mes is the manufacturing reference slice (docs/WorkQueue.md #83),
// modelled on Opcenter Execution and SAP ME: shop orders release SFCs that move
// through a routing's operations on work centers; nonconformances hold an SFC
// until quality signs a disposition; equipment states arrive from a gateway and
// downtime is derived from them. It may not change the kernel.
package mes

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver"
)

const (
	Authority = "plant-server"

	OrderType    = "mes.order"
	SFCType      = "mes.sfc"
	DowntimeType = "mes.downtime"
	ResourceType = "mes.resource"
	PlannedType  = "mes.planned-order"

	SchemaRelease  = "mes.order.release"
	SchemaStart    = "mes.sfc.start"
	SchemaComplete = "mes.sfc.complete"
	SchemaNC       = "mes.sfc.nc"
	SchemaSign     = "mes.sfc.sign"
	SchemaReason   = "mes.downtime.reason"

	schemaStates  = "mes.resource.states"
	schemaPlanned = "mes.erp.planned-order"
	schemaAnswer  = "mes.erp.confirmation" // the ERP's answer to an order confirmation (ADR-0014 D4)
)

// Master data (Opcenter: product, workflow/spec, resource; SAP ME: material, router/operation, work center/resource).

type Operation struct {
	Step       int    `json:"step"`
	Name       string `json:"name"`
	WorkCenter string `json:"workCenter"`
}

type Product struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Routing    string      `json:"routing"`
	Operations []Operation `json:"operations"`
}

type WorkCenter struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Line      string   `json:"line"`
	Resources []string `json:"resources"`
}

type MasterData struct {
	Products    []Product    `json:"products"`
	WorkCenters []WorkCenter `json:"workCenters"`
}

// Organisation is domain data (K6); policy reads it as context.

type Role string

const (
	Operator   Role = "operator"
	Quality    Role = "quality"
	Supervisor Role = "supervisor"
	Gateway    Role = "gateway"
	ERP        Role = "erp"
	Assistant  Role = "assistant" // an AI agent acting within the lines it is granted
)

// roleOf is a caller's role in this app.
func roleOf(c platformserver.Caller) Role { return Role(c.Role()) }

// SiteStructure is the organisation structure the plant's rules read (ADR-0012):
// a member works on the lines it belongs to there, directly or through the plant.
const SiteStructure = "site"

// Execution state.

type NC struct {
	Step int    `json:"step"`
	Code string `json:"code"`
	By   string `json:"by"`
}

type Signature struct {
	Action  string `json:"action"`
	Meaning string `json:"meaning"`
	By      string `json:"by"`
}

type SFC struct {
	ID         string      `json:"id"`
	Order      string      `json:"order"`
	Product    string      `json:"product"`
	Step       int         `json:"step"`  // index into the product's operations
	State      string      `json:"state"` // queued, active, hold, done, scrapped
	Resource   string      `json:"resource,omitempty"`
	Revision   uint32      `json:"revision"` // K4 C12: accepted changes naming this SFC
	NCs        []NC        `json:"ncs"`
	Signatures []Signature `json:"signatures"`
}

type Order struct {
	ID       string   `json:"id"`
	Product  string   `json:"product"`
	Quantity int      `json:"quantity"`
	SFCs     []string `json:"sfcs"`
	Planned  string   `json:"planned,omitempty"`
	// The confirmation written back to the ERP when the last SFC ends: sent,
	// confirmed (with the ERP's number), refused or failed, read from its answer.
	ERP          string `json:"erp,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
	ERPDetail    string `json:"erpDetail,omitempty"`
}

// Plant is one tenant: master data, execution state and its kernel logs.
type Plant struct {
	mu        sync.Mutex
	tenant    string
	master    MasterData
	orders    map[string]*Order
	sfcs      map[string]*SFC
	ledger    *platformserver.Ledger
	facts     *kernel.FactLog
	identity  *kernel.Identity
	downtime  map[string][]Downtime // resource → current derived events
	nextEvent int
}

func NewPlant(tenant string, master MasterData) *Plant {
	p := &Plant{tenant: tenant, master: master, orders: map[string]*Order{}, sfcs: map[string]*SFC{},
		ledger: platformserver.NewLedger(tenant, Authority, Actions(), OrderType, SFCType, DowntimeType),
		facts: kernel.NewFactLog(kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: schemaStates, Version: 1}, {Name: schemaPlanned, Version: 1},
			{Name: schemaAnswer, Version: 1}}, nil)),
		identity: kernel.NewIdentity(nil), downtime: map[string][]Downtime{}}
	p.ledger.Changes.Facts = func(tenant, id string) bool {
		return slices.ContainsFunc(p.facts.Records(tenant), func(r *pb.FactRecord) bool { return r.GetFactId() == id })
	}
	return p
}

func (p *Plant) Declarations() []*pb.AuthorityDeclaration { return p.ledger.Declarations() }

func fail(code pb.ErrorCode) *kernel.Error { return &kernel.Error{Code: code} }

var (
	invalid  = fail(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	conflict = fail(pb.ErrorCode_ERROR_CODE_CONFLICT)
	notFound = fail(pb.ErrorCode_ERROR_CODE_NOT_FOUND)
	denied   = fail(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
)

func (p *Plant) product(id string) *Product {
	i := slices.IndexFunc(p.master.Products, func(x Product) bool { return x.ID == id })
	if i < 0 {
		return nil
	}
	return &p.master.Products[i]
}

func (p *Plant) workCenter(id string) *WorkCenter {
	i := slices.IndexFunc(p.master.WorkCenters, func(x WorkCenter) bool { return x.ID == id })
	if i < 0 {
		return nil
	}
	return &p.master.WorkCenters[i]
}

// line of the work center where an SFC's current operation runs.
func (p *Plant) lineOf(sfc *SFC) string {
	if prod := p.product(sfc.Product); prod != nil && sfc.Step < len(prod.Operations) {
		if wc := p.workCenter(prod.Operations[sfc.Step].WorkCenter); wc != nil {
			return wc.Line
		}
	}
	return ""
}

func (p *Plant) resourceLine(resource string) string {
	for _, wc := range p.master.WorkCenters {
		if slices.Contains(wc.Resources, resource) {
			return wc.Line
		}
	}
	return ""
}

// allowed is the policy hook (K6 T3): the catalog decides which roles may call an
// action (ADR-0008); the plant hierarchy (lines) is context the domain reads, and
// the kernel never sees it.
func (p *Plant) allowed(who platformserver.Caller, s *pb.Submission, now time.Time) bool {
	scope := who.Units(SiteStructure, now)
	onLine := func(line string) bool { return line != "" && slices.Contains(scope, line) }
	sfc := p.sfcs[s.GetTarget().GetId()]
	switch s.GetSchema().GetName() {
	case SchemaRelease:
		var r releasePayload
		json.Unmarshal(s.GetPayload(), &r)
		prod := p.product(r.Product)
		return prod != nil && len(prod.Operations) > 0 && onLine(p.workCenter(prod.Operations[0].WorkCenter).Line)
	case SchemaStart, SchemaComplete:
		return sfc != nil && onLine(p.lineOf(sfc))
	case SchemaNC:
		return roleOf(who) == Quality || sfc != nil && onLine(p.lineOf(sfc))
	case SchemaSign:
		return true
	case SchemaReason:
		refs, _ := p.identity.Resolve(&pb.EntityRef{Type: DowntimeType, Id: s.GetTarget().GetId()})
		return roleOf(who) == Supervisor || len(refs) > 0 && onLine(p.resourceLine(resourceOfEvent(refs[0].ID)))
	}
	return false
}

// Disable deactivates a capability for this plant (start-up configuration): its
// actions leave the catalog and are refused; its recorded history still replays.
func (p *Plant) Disable(capability string) bool { return p.ledger.Catalog.Disable(capability) }

// Submit receives a decision in the kernel's order (K6 T2) with the plant's
// attribute conditions and rules; roles are the catalog's (ADR-0008).
func (p *Plant) Submit(who platformserver.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if who.Tenant != p.tenant {
		return nil, denied
	}
	return p.ledger.Receive(who, s, now, func() bool { return p.allowed(who, s, now) }, func() (func(*pb.ChangeRecord), *kernel.Error) {
		apply, err := p.validate(who, s)
		if err != nil {
			return nil, err
		}
		return func(record *pb.ChangeRecord) {
			if apply != nil {
				apply()
			}
			if sfc := p.sfcs[s.GetTarget().GetId()]; sfc != nil && s.GetTarget().GetType() == SFCType {
				sfc.Revision = record.GetRevision()
				p.confirmIfFinished(who, sfc.Order, now)
			}
		}, nil
	})
}

type releasePayload struct {
	Product  string `json:"product"`
	Quantity int    `json:"quantity"`
	SFCs     int    `json:"sfcs"`
	Planned  string `json:"planned,omitempty"` // the ERP planned order it fulfils (its claim is the evidence)
}

// sfcPayload acts on the SFC's current operation; a stale screen is refused by
// the submission's expected revision (K4 C12).
type sfcPayload struct {
	Resource string `json:"resource,omitempty"`
	Code     string `json:"code,omitempty"`
}

type signPayload struct {
	Action     string `json:"action"`  // rework, scrap, use-as-is
	Meaning    string `json:"meaning"` // reviewed, approved (21 CFR Part 11 signature meaning)
	ReworkStep int    `json:"reworkStep,omitempty"`
}

type reasonPayload struct {
	Reason string `json:"reason"`
}

// validate checks the plant's rules (K4 C10) and returns how to apply the decision.
func (p *Plant) validate(who platformserver.Caller, s *pb.Submission) (func(), *kernel.Error) {
	id := s.GetTarget().GetId()
	switch s.GetSchema().GetName() {
	case SchemaRelease:
		var r releasePayload
		if json.Unmarshal(s.GetPayload(), &r) != nil || p.product(r.Product) == nil || r.Quantity < 1 || r.SFCs < 1 || r.SFCs > r.Quantity {
			return nil, invalid
		}
		prod := p.product(r.Product)
		if p.orders[id] != nil {
			return nil, conflict
		}
		return func() {
			o := &Order{ID: id, Product: prod.ID, Quantity: r.Quantity, Planned: r.Planned}
			for n := 1; n <= r.SFCs; n++ {
				sfc := &SFC{ID: fmt.Sprintf("%s-%03d", id, n), Order: id, Product: prod.ID, State: "queued", NCs: []NC{}, Signatures: []Signature{}}
				p.sfcs[sfc.ID] = sfc
				o.SFCs = append(o.SFCs, sfc.ID)
				p.identity.Create(&pb.EntityRef{Type: SFCType, Id: sfc.ID})
			}
			p.orders[id] = o
			p.identity.Create(&pb.EntityRef{Type: OrderType, Id: id})
		}, nil
	case SchemaStart, SchemaComplete, SchemaNC:
		var st sfcPayload
		sfc := p.sfcs[id]
		if json.Unmarshal(s.GetPayload(), &st) != nil {
			return nil, invalid
		}
		if sfc == nil {
			return nil, notFound
		}
		prod := p.product(sfc.Product)
		switch s.GetSchema().GetName() {
		case SchemaStart:
			wc := p.workCenter(prod.Operations[sfc.Step].WorkCenter)
			if !slices.Contains(wc.Resources, st.Resource) {
				return nil, invalid
			}
			if sfc.State != "queued" {
				return nil, conflict
			}
			return func() { sfc.State, sfc.Resource = "active", st.Resource }, nil
		case SchemaComplete:
			if sfc.State != "active" {
				return nil, conflict
			}
			return func() {
				sfc.Resource = ""
				if sfc.Step+1 < len(prod.Operations) {
					sfc.Step, sfc.State = sfc.Step+1, "queued"
				} else {
					sfc.State = "done"
				}
			}, nil
		default:
			if st.Code == "" {
				return nil, invalid
			}
			if sfc.State != "queued" && sfc.State != "active" {
				return nil, conflict
			}
			return func() {
				sfc.State, sfc.Resource = "hold", ""
				sfc.NCs = append(sfc.NCs, NC{Step: sfc.Step, Code: st.Code, By: who.ID})
			}, nil
		}
	case SchemaSign:
		var sg signPayload
		sfc := p.sfcs[id]
		if json.Unmarshal(s.GetPayload(), &sg) != nil || !slices.Contains([]string{"rework", "scrap", "use-as-is"}, sg.Action) ||
			!slices.Contains([]string{"reviewed", "approved"}, sg.Meaning) {
			return nil, invalid
		}
		if sfc == nil {
			return nil, notFound
		}
		if sfc.State != "hold" || slices.ContainsFunc(sfc.Signatures, func(x Signature) bool { return x.By == who.ID || x.Meaning == sg.Meaning && x.Action == sg.Action }) {
			return nil, conflict // one signature per person, one per meaning
		}
		if sg.Action == "rework" && (sg.ReworkStep < 0 || sg.ReworkStep > sfc.Step) {
			return nil, invalid
		}
		return func() {
			sfc.Signatures = append(sfc.Signatures, Signature{Action: sg.Action, Meaning: sg.Meaning, By: who.ID})
			agreed := 0
			for _, x := range sfc.Signatures {
				if x.Action == sg.Action {
					agreed++
				}
			}
			if agreed < 2 { // two people, reviewed and approved, on the same disposition
				return
			}
			switch sg.Action {
			case "rework":
				sfc.Step, sfc.State = sg.ReworkStep, "queued"
			case "scrap":
				sfc.State = "scrapped"
			default:
				sfc.State = "queued"
			}
			sfc.Signatures = []Signature{}
		}, nil
	case SchemaReason:
		var r reasonPayload
		if json.Unmarshal(s.GetPayload(), &r) != nil || r.Reason == "" {
			return nil, invalid
		}
		refs, err := p.identity.Resolve(&pb.EntityRef{Type: DowntimeType, Id: id})
		if err != nil {
			return nil, notFound
		}
		if len(refs) != 1 {
			return nil, conflict // the event was split: the reason must be given for each part
		}
		return nil, nil // reasons are read from the log through identity (see Downtime)
	}
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}

// Reads.

func (p *Plant) Master() MasterData { return p.master }

func (p *Plant) Orders() []Order {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Order, 0, len(p.orders))
	for _, o := range p.orders {
		out = append(out, *o)
	}
	slices.SortFunc(out, func(a, b Order) int { return compare(a.ID, b.ID) })
	return out
}

func (p *Plant) SFCs() []SFC {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]SFC, 0, len(p.sfcs))
	for _, s := range p.sfcs {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b SFC) int { return compare(a.ID, b.ID) })
	return out
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
