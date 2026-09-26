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
	"platformserver/platform"
)

const (
	Authority = "plant-server"

	OrderType    = "mes.order"
	SFCType      = "mes.sfc"
	DowntimeType = "mes.downtime"
	ResourceType = "mes.resource"

	SchemaRelease  = "mes.order.release"
	SchemaStart    = "mes.sfc.start"
	SchemaComplete = "mes.sfc.complete"
	SchemaNC       = "mes.sfc.nc"
	SchemaSign     = "mes.sfc.sign"
	SchemaReason   = "mes.downtime.reason"
	SchemaResend   = "mes.order.reconfirm"
	SchemaConfirm  = "mes.order.confirm"
	SchemaAnswer   = "mes.order.answer"

	schemaStates = "mes.resource.states"
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
	Assistant  Role = "assistant" // an AI agent acting within the lines it is granted
)

// roleOf is a caller's role in this app.
func roleOf(c platform.Caller) Role { return Role(c.Role()) }

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

// SFC is a lot moving through its product's routing (ADR-0016, ADR-0017): its
// lifecycle is start, complete, nonconformance and signed disposition.
type SFC struct {
	platform.Record
	Order      platform.Ref[Order] `json:"order" field:"readonly"`
	Product    string              `json:"product" field:"readonly,search" help:"The product this lot becomes"`
	Step       int                 `json:"step" field:"readonly" help:"The index of its current operation in the product's routing"` // index into the product's operations
	State      string              `json:"state" field:"readonly" choices:"queued,active,hold,done,scrapped"`
	Resource   string              `json:"resource,omitempty" field:"readonly" help:"The machine or station working on it now"`
	NCs        []NC                `json:"ncs" field:"readonly" title:"Nonconformances"`
	Signatures []Signature         `json:"signatures" field:"readonly"`
}

// Order is a released shop order; it completes when its last SFC ends.
type Order struct {
	platform.Record
	Product  string              `json:"product" field:"readonly,search"`
	Quantity int                 `json:"quantity" field:"readonly" help:"Units to make"`
	SFCs     []platform.Ref[SFC] `json:"sfcs" field:"readonly" title:"SFCs"`
	Planned  string              `json:"planned,omitempty" field:"readonly" title:"Planned order" help:"The ERP's planned order this order fulfils" synonyms:"PO"`
	Status   string              `json:"status" field:"readonly" choices:"released,completed"`
	// The confirmation to the ERP when the last SFC ends (production.orders/1):
	// sent, confirmed (with the ERP's number), refused or failed.
	ERP          string `json:"erp,omitempty" field:"readonly" title:"ERP"`
	Confirmation string `json:"confirmation,omitempty" field:"readonly"`
	ERPDetail    string `json:"erpDetail,omitempty" field:"readonly" title:"ERP detail"`
	Resent       int    `json:"resent,omitempty" field:"readonly"` // corrected confirmations sent after a refusal or failure
}

// Plant is one tenant: master data, execution state and its kernel logs.
type Plant struct {
	mu        sync.Mutex
	tenant    string
	master    MasterData
	entities  []platform.Entity
	ledger    *platform.Ledger
	facts     *kernel.FactLog
	identity  *kernel.Identity
	downtime  map[string][]Downtime // resource → current derived events
	nextEvent int
}

func NewPlant(tenant string, master MasterData) *Plant {
	p := &Plant{tenant: tenant, master: master,
		ledger:   platform.NewLedger(tenant, Authority, Actions(), OrderType, SFCType, DowntimeType),
		facts:    kernel.NewFactLog(kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: schemaStates, Version: 1}}, nil)),
		identity: kernel.NewIdentity(nil), downtime: map[string][]Downtime{}}
	p.ledger.Changes.Facts = func(tenant, id string) bool {
		return slices.ContainsFunc(p.facts.Records(tenant), func(r *pb.FactRecord) bool { return r.GetFactId() == id })
	}
	p.entities = Entities(p)
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
func (p *Plant) lineOf(sfc SFC) string {
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
func (p *Plant) allowed(who platform.Caller, s *pb.Submission, now time.Time) bool {
	scope := who.Units(SiteStructure, now)
	onLine := func(line string) bool { return line != "" && slices.Contains(scope, line) }
	sfc, known := platform.Get[SFC](who, s.GetTarget().GetId())
	switch s.GetSchema().GetName() {
	case SchemaRelease:
		var r releasePayload
		json.Unmarshal(s.GetPayload(), &r)
		prod := p.product(r.Product)
		return prod != nil && len(prod.Operations) > 0 && onLine(p.workCenter(prod.Operations[0].WorkCenter).Line)
	case SchemaStart, SchemaComplete:
		return known && onLine(p.lineOf(sfc))
	case SchemaNC:
		return roleOf(who) == Quality || known && onLine(p.lineOf(sfc))
	case SchemaSign:
		return true
	case SchemaResend, SchemaConfirm, SchemaAnswer:
		o, known := platform.Get[Order](who, s.GetTarget().GetId())
		return known && onLine(p.orderLine(o))
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
// attribute conditions and rules; roles are the catalog's (ADR-0008). The SFC's
// transitions are generated from its lifecycle (ADR-0017).
func (p *Plant) Submit(who platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if who.Tenant != p.tenant {
		return nil, denied
	}
	allowed := func() bool { return p.allowed(who, s, now) }
	if record, err, ok := p.ledger.Generated(who, s, now, allowed, p.entities...); ok {
		return record, err
	}
	return p.ledger.Receive(who, s, now, allowed, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return p.validate(who, s, now)
	})
}

type releasePayload struct {
	Product  string `json:"product"`
	Quantity int    `json:"quantity"`
	SFCs     int    `json:"sfcs"`
	Planned  string `json:"planned,omitempty"` // the ERP planned order it fulfils
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

// validate checks the rules of the plant's own actions (K4 C10) and returns
// how to apply the decision.
func (p *Plant) validate(who platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	switch s.GetSchema().GetName() {
	case SchemaRelease:
		var r releasePayload
		if json.Unmarshal(s.GetPayload(), &r) != nil || p.product(r.Product) == nil || r.Quantity < 1 || r.SFCs < 1 || r.SFCs > r.Quantity {
			return nil, invalid
		}
		if _, known := platform.Get[Order](who, id); known {
			return nil, conflict
		}
		o := Order{Record: platform.Record{ID: id}, Product: r.Product, Quantity: r.Quantity, Planned: r.Planned}
		if r.Planned != "" && !who.Replaying && p.fits(who, o, r.Planned) != "" {
			return nil, invalid
		}
		return func(record *pb.ChangeRecord) {
			for n := 1; n <= r.SFCs; n++ {
				sfc := SFC{Record: platform.Record{ID: fmt.Sprintf("%s-%03d", id, n)}, Order: platform.Ref[Order](id), Product: r.Product, NCs: []NC{}, Signatures: []Signature{}}
				o.SFCs = append(o.SFCs, platform.Ref[SFC](sfc.ID))
				p.identity.Create(&pb.EntityRef{Type: SFCType, Id: sfc.ID})
				who.Put(record, sfc)
			}
			who.Put(record, o)
			p.identity.Create(&pb.EntityRef{Type: OrderType, Id: id})
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
	case SchemaConfirm:
		o, known := platform.Get[Order](who, id)
		if !known {
			return nil, notFound
		}
		if o.Status != "completed" || o.ERP != "" {
			return nil, conflict // a completed order, confirmed once; corrections are resent
		}
		return p.confirm(who, s, o, now), nil
	case SchemaAnswer: // the flow records the answer of an ERP outside once its provider shows it
		o, known := platform.Get[Order](who, id)
		if !known {
			return nil, notFound
		}
		answer, ok := awaited(who, o)
		if !ok {
			return nil, conflict
		}
		return func(record *pb.ChangeRecord) { p.answered(who, record, o, answer, s.GetIdempotencyKey(), now) }, nil
	case SchemaResend:
		var r struct {
			Planned string `json:"planned"`
		}
		o, known := platform.Get[Order](who, id)
		if json.Unmarshal(s.GetPayload(), &r) != nil {
			return nil, invalid
		}
		if !known {
			return nil, notFound
		}
		if o.ERP != "refused" && o.ERP != "failed" {
			return nil, conflict // only a confirmation the ERP refused, or that never arrived, is corrected
		}
		// Rules added later judge new decisions only; the record stands on replay.
		if r.Planned != "" && !who.Replaying && p.fits(who, o, r.Planned) != "" {
			return nil, invalid
		}
		if r.Planned != "" {
			o.Planned = r.Planned
		}
		o.Resent++
		o.ERP, o.Confirmation, o.ERPDetail = "", "", ""
		return p.confirm(who, s, o, now), nil
	}
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}

// Entities declares the plant's types (ADR-0016) and the SFC's lifecycle
// (ADR-0017): its transitions are the operator's and quality's actions, with
// the plant's rules in Do and what follows (the order's completion, its
// confirmation to the ERP) in After. p may be nil for the catalog alone.
func Entities(p *Plant) []platform.Entity {
	roles := func(r ...Role) []string {
		out := make([]string, len(r))
		for i, x := range r {
			out[i] = string(x)
		}
		return out
	}
	sfcOf := func(record any) *SFC { return record.(*SFC) }
	finished := func(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
		p.finishOrder(c, r, string(sfcOf(record).Order), now)
	}
	return []platform.Entity{
		{Type: OrderType, Title: "Shop order", Model: Order{}, Synonyms: "work order,production order",
			Description: "An order released to the shop floor to make a quantity of a product; it is split into SFCs and confirmed to the ERP when its last SFC ends.",
			Lifecycle: &platform.Lifecycle{Field: "status", Initial: "released",
				States: []platform.State{{Name: "released", Title: "Released", Tone: "info"}, {Name: "completed", Title: "Completed", Tone: "success"}}}},
		{Type: SFCType, Title: "SFC", Model: SFC{}, Synonyms: "lot,batch",
			Description: "A shop floor control: one lot of a shop order moving through the product's routing, operation by operation.", Lifecycle: &platform.Lifecycle{Field: "state", Initial: "queued",
				States: []platform.State{{Name: "queued", Title: "Queued", Tone: "info"}, {Name: "active", Title: "In work", Tone: "warning"},
					{Name: "hold", Title: "On hold", Tone: "danger", Description: "held by a nonconformance until two quality engineers sign one disposition"}, {Name: "done", Title: "Done", Tone: "success"}, {Name: "scrapped", Title: "Scrapped", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "start", Title: "Start operation", Capability: "execution", From: []string{"queued"}, To: []string{"active"}, Roles: roles(Operator),
						Description: "Start the SFC's current operation on a resource of its work center.",
						Payload:     []platform.Field{{Name: "resource", Type: "string", Required: true, Description: "Resource of the operation's work center"}},
						Do: func(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
							sfc := sfcOf(record)
							var st sfcPayload
							json.Unmarshal(payload, &st)
							prod := p.product(sfc.Product)
							if wc := p.workCenter(prod.Operations[sfc.Step].WorkCenter); wc == nil || !slices.Contains(wc.Resources, st.Resource) {
								return invalid
							}
							sfc.Resource = st.Resource
							return nil
						}},
					{Name: "complete", Title: "Complete operation", Capability: "execution", From: []string{"active"}, To: []string{"queued", "done"}, Roles: roles(Operator),
						Description: "Complete the SFC's active operation; it moves to the next operation or is done.",
						Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
							sfc := sfcOf(record)
							sfc.Resource = ""
							if sfc.Step+1 < len(p.product(sfc.Product).Operations) {
								sfc.Step, sfc.State = sfc.Step+1, "queued"
							} else {
								sfc.State = "done"
							}
							return nil
						}, After: finished},
					{Name: "nc", Title: "Log nonconformance", Capability: "quality", From: []string{"queued", "active"}, To: []string{"hold"}, Roles: roles(Operator, Quality),
						Description: "Log a nonconformance at the current operation; the SFC is held until quality signs a disposition.",
						Payload:     []platform.Field{{Name: "code", Type: "string", Required: true, Description: "NC code: POROSITY, DIMENSION, SURFACE, LEAK"}},
						Do: func(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
							sfc := sfcOf(record)
							var st sfcPayload
							if json.Unmarshal(payload, &st) != nil || st.Code == "" {
								return invalid
							}
							sfc.Resource = ""
							sfc.NCs = append(sfc.NCs, NC{Step: sfc.Step, Code: st.Code, By: c.ID})
							return nil
						}},
					{Name: "sign", Title: "Sign disposition", Capability: "quality", From: []string{"hold"}, To: []string{"hold", "queued", "scrapped"}, Roles: roles(Quality),
						Description: "Sign the disposition of a held SFC (electronic signature with meaning); two people, reviewed and approved, on the same disposition release it.",
						Payload: []platform.Field{{Name: "action", Type: "string", Required: true, Description: "rework, scrap or use-as-is"},
							{Name: "meaning", Type: "string", Required: true, Description: "reviewed or approved"},
							{Name: "reworkStep", Type: "integer", Description: "Operation step to rework from"}},
						Do: func(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
							sfc := sfcOf(record)
							var sg signPayload
							if json.Unmarshal(payload, &sg) != nil || !slices.Contains([]string{"rework", "scrap", "use-as-is"}, sg.Action) ||
								!slices.Contains([]string{"reviewed", "approved"}, sg.Meaning) {
								return invalid
							}
							if slices.ContainsFunc(sfc.Signatures, func(x Signature) bool { return x.By == c.ID || x.Meaning == sg.Meaning && x.Action == sg.Action }) {
								return conflict // one signature per person, one per meaning
							}
							if sg.Action == "rework" && (sg.ReworkStep < 0 || sg.ReworkStep > sfc.Step) {
								return invalid
							}
							sfc.Signatures = append(sfc.Signatures, Signature{Action: sg.Action, Meaning: sg.Meaning, By: c.ID})
							agreed := 0
							for _, x := range sfc.Signatures {
								if x.Action == sg.Action {
									agreed++
								}
							}
							if agreed < 2 { // two people, reviewed and approved, on the same disposition
								return nil
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
							return nil
						}, After: finished},
				}}},
	}
}

// Reads.

func (p *Plant) Master() MasterData { return p.master }

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
