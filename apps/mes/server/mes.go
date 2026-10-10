// Package mes is the manufacturing execution system (ADR-0025 D1; #83),
// modelled on Opcenter Execution and SAP ME: shop orders release SFCs that move
// through a routing's operations on work centers; nonconformances hold an SFC
// until quality signs a disposition; equipment states arrive from a gateway and
// downtime is derived from them. It may not change the kernel.
package mes

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"production"
)

const (
	ID = "mes"

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

// Parameter is one process setpoint an operation must hold (SAP ME: an operation
// parameter; Opcenter: a characteristic on the specification). Type says how the
// numbers read: a setpoint to hold, a range to stay inside, a limit not to pass,
// or a note the operator reads with no number at all.
type Parameter struct {
	Name   string  `json:"name"`
	Title  string  `json:"title,omitempty"`
	Type   string  `json:"type,omitempty"` // setpoint, range, limit, note
	Unit   string  `json:"unit,omitempty"`
	Target float64 `json:"target,omitempty"`
	Min    float64 `json:"min,omitempty"`
	Max    float64 `json:"max,omitempty"`
}

// Operation is one step of a routing (SAP ME: a router operation). Its number is
// both its identity and its order — 10, 20, 30 — the way a paper router reads, so
// a lot says which operation it stands at by number instead of by counting array
// positions. Requires names the capabilities its equipment must have; Parameters
// are what the machine or the operator must hold while it runs.
type Operation struct {
	Number     int         `json:"number"`
	Name       string      `json:"name"`
	WorkCenter string      `json:"workCenter"`
	Requires   []string    `json:"requires,omitempty"`
	Parameters []Parameter `json:"parameters,omitempty"`
	// Capable is computed, never declared: the resources that can run this
	// operation. Master() fills it in so a floor screen and the refusal in `start`
	// read the same answer from the same rule (canDo).
	Capable []string `json:"capable,omitempty"`
}

// Routing is a reusable, versioned sequence of operations (SAP ME: a router;
// Opcenter: a workflow). Products point at one by ID; a lot is released against a
// specific version, so editing a routing never moves a lot already on the floor.
// MasterData holds one entry per version, keyed by ID and Version.
type Routing struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Version    int         `json:"version"`
	Operations []Operation `json:"operations"`
}

// operation finds a step of this routing by its number.
func (r *Routing) operation(number int) *Operation {
	if r == nil {
		return nil
	}
	i := slices.IndexFunc(r.Operations, func(op Operation) bool { return op.Number == number })
	if i < 0 {
		return nil
	}
	return &r.Operations[i]
}

// first is the operation a lot starts at, and next the one after `number` — nil at
// the end of the routing, which is where a lot is done.
func (r *Routing) first() *Operation {
	if r == nil || len(r.Operations) == 0 {
		return nil
	}
	return &r.Operations[0]
}

func (r *Routing) next(number int) *Operation {
	if r == nil {
		return nil
	}
	i := slices.IndexFunc(r.Operations, func(op Operation) bool { return op.Number == number })
	if i < 0 || i+1 >= len(r.Operations) {
		return nil
	}
	return &r.Operations[i+1]
}

type Product struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Routing string `json:"routing"` // the routing it is made by; which version, see MasterData.Routings
}

// Resource is one machine or station (SAP ME: a resource inside a work center).
// Capabilities are what it can do; matching them against what an operation asks
// for is how the plant answers "which equipment can process this step".
type Resource struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	WorkCenter   string   `json:"workCenter"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type WorkCenter struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Line string `json:"line"`
}

type MasterData struct {
	Products    []Product    `json:"products"`
	Routings    []Routing    `json:"routings"`
	WorkCenters []WorkCenter `json:"workCenters"`
	Resources   []Resource   `json:"resources"`
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

// holds reports whether the caller holds role in this app (any of their roles, ADR-0078).
func holds(c platform.Caller, role Role) bool { return c.Holds(c.App, string(role)) }

// SiteStructure is the organisation structure the plant's rules read (ADR-0012):
// a member works on the lines it belongs to there, directly or through the plant.
const SiteStructure = "site"

// Execution state.

type NC struct {
	Operation int    `json:"operation"` // the operation number it was logged at
	Code      string `json:"code"`
	By        string `json:"by"`
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
	Order    platform.Ref[Order] `json:"order" field:"readonly" inverse:"sfcs"`
	Product  string              `json:"product" field:"readonly,search" help:"The product this lot becomes"`
	Quantity int                 `json:"quantity" field:"readonly" help:"Units in this lot: its order's quantity split over its SFCs"`
	// Released against one routing version, so a later edit of the routing does not
	// move a lot already on the floor; standing at one operation, named by number.
	Routing        string      `json:"routing" field:"readonly" title:"Routing" help:"The routing this lot was released against"`
	RoutingVersion int         `json:"routingVersion" field:"readonly" title:"Routing version" help:"The routing version fixed at release"`
	Operation      int         `json:"operation" field:"readonly" title:"Operation" help:"The number of the operation this lot stands at"`
	State          string      `json:"state" field:"readonly" choices:"queued,active,hold,done,scrapped"`
	Resource       string      `json:"resource,omitempty" field:"readonly" help:"The machine or station working on it now"`
	NCs            []NC        `json:"ncs" field:"readonly" title:"Nonconformances"`
	Signatures     []Signature `json:"signatures" field:"readonly"`
}

// Order is a released shop order; it completes when its last SFC ends.
type Order struct {
	platform.Record
	Product  string              `json:"product" field:"readonly,search"`
	Quantity int                 `json:"quantity" field:"readonly" help:"Units to make"`
	SFCs     []platform.Ref[SFC] `json:"sfcs" field:"readonly" title:"SFCs"`
	Planned  string              `json:"planned,omitempty" field:"readonly" title:"Planned order" help:"The ERP's planned order this order fulfils" synonyms:"PO"`
	Place    string              `json:"place,omitempty" field:"readonly" title:"Place in the model" help:"The enterprise model's place this order runs at" ref:"enterprise.element"`
	Status   string              `json:"status" field:"readonly" choices:"released,completed"`
	// The confirmation to the ERP when the last SFC ends (production.orders/1):
	// sent, confirmed (with the ERP's number), refused or failed.
	ERP              string   `json:"erp,omitempty" field:"readonly" title:"ERP"`
	Confirmation     string   `json:"confirmation,omitempty" field:"readonly"`
	ERPDetail        string   `json:"erpDetail,omitempty" field:"readonly" title:"ERP detail"`
	Resent           int      `json:"resent,omitempty" field:"readonly"` // corrected confirmations sent after a refusal or failure
	Advice           string   `json:"advice,omitempty" field:"readonly" title:"Review advice"`
	AdviceCategory   string   `json:"adviceCategory,omitempty" field:"readonly" title:"Advice category" choices:"routine,review"`
	AdviceReview     bool     `json:"adviceReview,omitempty" field:"readonly" title:"Needs review"`
	AdviceState      string   `json:"adviceState,omitempty" field:"readonly" title:"Advice status" choices:"pending,ready,rejected"`
	AdviceDefinition string   `json:"adviceDefinition,omitempty" field:"readonly" title:"Advice definition"`
	AdviceModel      string   `json:"adviceModel,omitempty" field:"readonly" title:"Advice model"`
	AdviceSources    []string `json:"adviceSources,omitempty" field:"readonly,aside" title:"Advice sources"`
	AdviceWithheld   bool     `json:"adviceWithheld,omitempty" field:"readonly" title:"Advice withheld"`
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

func New(tenant string, master MasterData) *Plant {
	p := &Plant{tenant: tenant, master: master,
		ledger:   platform.NewLedger(tenant, ID, Actions(), OrderType, SFCType, DowntimeType),
		facts:    kernel.NewFactLog(kernel.NewSchemaRegistry([]*pb.SchemaRef{{Name: schemaStates, Version: 1}}, nil)),
		identity: kernel.NewIdentity(nil), downtime: map[string][]Downtime{}}
	p.ledger.SetFacts(func(tenant, id string) bool {
		return slices.ContainsFunc(p.facts.Records(tenant), func(r *pb.FactRecord) bool { return r.GetFactId() == id })
	})
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

// routing finds one version of a routing; version 0 asks for its newest.
func (p *Plant) routing(id string, version int) *Routing {
	var newest *Routing
	for i := range p.master.Routings {
		r := &p.master.Routings[i]
		if r.ID != id || version != 0 && r.Version != version {
			continue
		}
		if newest == nil || r.Version > newest.Version {
			newest = r
		}
	}
	return newest
}

// released is the routing version a lot was fixed to at release.
func (p *Plant) released(sfc *SFC) *Routing { return p.routing(sfc.Routing, sfc.RoutingVersion) }

// routingFor is the newest version of the routing a product is made by.
func (p *Plant) routingFor(product string) *Routing {
	if prod := p.product(product); prod != nil {
		return p.routing(prod.Routing, 0)
	}
	return nil
}

func (p *Plant) resource(id string) *Resource {
	i := slices.IndexFunc(p.master.Resources, func(x Resource) bool { return x.ID == id })
	if i < 0 {
		return nil
	}
	return &p.master.Resources[i]
}

// canDo is the plant's one capability rule: a resource runs an operation when it
// belongs to that operation's work center and holds every capability the operation
// requires. Serving `capable` and refusing `start` both read it, so the answer a
// floor screen shows and the answer the plant enforces cannot drift apart.
func (p *Plant) canDo(resource string, op Operation) bool {
	found := p.resource(resource)
	if found == nil || found.WorkCenter != op.WorkCenter {
		return false
	}
	for _, want := range op.Requires {
		if !slices.Contains(found.Capabilities, want) {
			return false
		}
	}
	return true
}

// capable answers "which equipment can process this step": every resource that can
// run the operation, in master-data order.
func (p *Plant) capable(op Operation) []string {
	var ids []string
	for _, r := range p.master.Resources {
		if p.canDo(r.ID, op) {
			ids = append(ids, r.ID)
		}
	}
	return ids
}

// line of the work center where an SFC's current operation runs.
func (p *Plant) lineOf(sfc SFC) string {
	if r := p.released(&sfc); r != nil {
		if op := r.operation(sfc.Operation); op != nil {
			if wc := p.workCenter(op.WorkCenter); wc != nil {
				return wc.Line
			}
		}
	}
	return ""
}

func (p *Plant) resourceLine(resource string) string {
	if found := p.resource(resource); found != nil {
		if wc := p.workCenter(found.WorkCenter); wc != nil {
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
		routing := p.routingFor(r.Product)
		first := routing.first()
		return first != nil && onLine(p.workCenter(first.WorkCenter).Line)
	case SchemaStart, SchemaComplete:
		return known && onLine(p.lineOf(sfc))
	case SchemaNC:
		return holds(who, Quality) || known && onLine(p.lineOf(sfc))
	case SchemaSign:
		return true
	case SchemaResend, SchemaConfirm, SchemaAnswer:
		o, known := platform.Get[Order](who, s.GetTarget().GetId())
		return known && onLine(p.orderLine(o))
	case SchemaAdvice, SchemaAdviceAnswer:
		return true // host function planning enforces the source's read scope
	case SchemaReason:
		refs, _ := p.identity.Resolve(&pb.EntityRef{Type: DowntimeType, Id: s.GetTarget().GetId()})
		return holds(who, Supervisor) || len(refs) > 0 && onLine(p.resourceLine(resourceOfEvent(refs[0].ID)))
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

// Confirmation and generated lifecycle decisions use only the staged record,
// ledger and intent owners. Release and observation-derived identity changes
// are deliberately not opted in until their own state has a saved result.
func (p *Plant) AcceptedLedger() *platform.Ledger { return p.ledger }
func (*Plant) AcceptedActionSchemas() []string {
	return []string{SchemaConfirm, SchemaAnswer, SchemaResend, SchemaAdvice, SchemaAdviceAnswer}
}

type releasePayload struct {
	Product  string `json:"product"`
	Quantity int    `json:"quantity"`
	SFCs     int    `json:"sfcs"`
	Planned  string `json:"planned,omitempty"` // the ERP planned order it fulfils
	Place    string `json:"place,omitempty"`   // the enterprise model's place this order runs at (ADR-0094)
}

// sfcPayload acts on the SFC's current operation; a stale screen is refused by
// the submission's expected revision (K4 C12).
type sfcPayload struct {
	Resource string `json:"resource,omitempty"`
	Code     string `json:"code,omitempty"`
}

type signPayload struct {
	Action          string `json:"action"`  // rework, scrap, use-as-is
	Meaning         string `json:"meaning"` // reviewed, approved (21 CFR Part 11 signature meaning)
	ReworkOperation int    `json:"reworkOperation,omitempty"`
}

// reworkable is the rule behind the disposition dialog: a lot goes back to an
// operation its own routing declares, at or before where it stands now. Naming an
// operation number, not an array index, is what makes the dialog a list to pick
// from instead of a number to remember.
func reworkable(routing *Routing, at, to int) bool {
	if routing == nil {
		return false
	}
	back := slices.IndexFunc(routing.Operations, func(op Operation) bool { return op.Number == to })
	now := slices.IndexFunc(routing.Operations, func(op Operation) bool { return op.Number == at })
	return back >= 0 && now >= 0 && back <= now
}

type reasonPayload struct {
	Reason string `json:"reason"`
}

// validate checks the rules of the plant's own actions (K4 C10) and returns
// how to apply the decision.
func (p *Plant) validate(who platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	switch s.GetSchema().GetName() {
	case SchemaAdvice, SchemaAdviceAnswer:
		return advice(who, s)
	case SchemaRelease:
		var r releasePayload
		if json.Unmarshal(s.GetPayload(), &r) != nil || p.product(r.Product) == nil {
			return nil, invalid
		}
		routing := p.routingFor(r.Product) // read after the payload: a lot is fixed to the newest version at release
		if routing.first() == nil {
			return nil, invalid
		}
		if _, known := platform.Get[Order](who, id); known {
			return nil, conflict
		}
		o := Order{Record: platform.Record{ID: id}, Product: r.Product, Quantity: r.Quantity, Planned: r.Planned, Place: r.Place}
		if r.Planned != "" && !who.Replaying && p.fits(who, o, r.Planned) != "" {
			return nil, invalid
		}
		return func(record *pb.ChangeRecord) {
			for n := 1; n <= r.SFCs; n++ {
				quantity := r.Quantity / r.SFCs // the remainder goes to the first lots: 10 in 3 is 4, 3, 3
				if n <= r.Quantity%r.SFCs {
					quantity++
				}
				sfc := SFC{Record: platform.Record{ID: fmt.Sprintf("%s-%03d", id, n)}, Order: platform.Ref[Order](id), Product: r.Product, Quantity: quantity,
					Routing: routing.ID, RoutingVersion: routing.Version, Operation: routing.first().Number, NCs: []NC{}, Signatures: []Signature{}}
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
	case SchemaAnswer: // the provider's answer to the confirmation, or, for an ERP outside, the flow once its adapter shows it
		o, known := platform.Get[Order](who, id)
		var a platform.Answer
		if !known {
			return nil, notFound
		}
		if json.Unmarshal(s.GetPayload(), &a) != nil {
			return nil, invalid
		}
		answer, ok := p.answer(who, o, a)
		if !ok && a.Outcome == "accepted" {
			return nil, nil // sent: the ERP outside answers later
		}
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
			Derived: []platform.Derivation{{From: "adviceSources", Fields: []string{"advice", "adviceCategory", "adviceReview", "adviceSources"}}}, Withheld: "adviceWithheld",
			Description: "An order released to the shop floor to make a quantity of a product; it is split into SFCs and confirmed to the ERP when its last SFC ends.",
			Lifecycle: &platform.Lifecycle{Field: "status", Initial: "released",
				States: []platform.State{{Name: "released", Title: "Released", Tone: "info"}, {Name: "completed", Title: "Completed", Tone: "success"}}}},
		{Type: SFCType, Title: "SFC", Model: SFC{}, Synonyms: "lot,batch",
			Description: "A shop floor control: one lot of a shop order moving through the product's routing, operation by operation.", Lifecycle: &platform.Lifecycle{Field: "state", Initial: "queued",
				States: []platform.State{{Name: "queued", Title: "Queued", Tone: "info"}, {Name: "active", Title: "In work", Tone: "warning"},
					{Name: "hold", Title: "On hold", Tone: "danger", Description: "held by a nonconformance until two quality engineers sign one disposition"}, {Name: "done", Title: "Done", Tone: "success"}, {Name: "scrapped", Title: "Scrapped", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "start", Title: "Start operation", Capability: "execution", From: []string{"queued"}, To: []string{"active"}, Roles: roles(Operator),
						Description: "Start the SFC's current operation on a resource that can run it.",
						Payload:     []platform.Field{{Name: "resource", Type: "string", Required: true, Description: "A resource the operation's capable list names"}},
						Do: func(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
							sfc := sfcOf(record)
							var st sfcPayload
							json.Unmarshal(payload, &st)
							routing := p.released(sfc)
							op := routing.operation(sfc.Operation)
							if op == nil || !p.canDo(st.Resource, *op) {
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
							if next := p.released(sfc).next(sfc.Operation); next != nil {
								sfc.Operation, sfc.State = next.Number, "queued"
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
							sfc.NCs = append(sfc.NCs, NC{Operation: sfc.Operation, Code: st.Code, By: c.ID})
							return nil
						}},
					{Name: "sign", Title: "Sign disposition", Capability: "quality", From: []string{"hold"}, To: []string{"hold", "queued", "scrapped"}, Roles: roles(Quality),
						Description: "Sign the disposition of a held SFC (electronic signature with meaning); two people, reviewed and approved, on the same disposition release it.",
						Payload: []platform.Field{{Name: "action", Type: "string", Required: true, Description: "rework, scrap or use-as-is"},
							{Name: "meaning", Type: "string", Required: true, Description: "reviewed or approved"},
							{Name: "reworkOperation", Type: "integer", Description: "Operation number to rework from: one this lot has already reached"}},
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
							if sg.Action == "rework" && !reworkable(p.released(sfc), sfc.Operation, sg.ReworkOperation) {
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
								sfc.Operation, sfc.State = sg.ReworkOperation, "queued"
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

// Master serves the plant's master data with each operation's `capable` list
// computed on the way out (ADR-0088): the floor screens read which equipment can
// process a step instead of re-deriving it, and `start` refuses by the same rule.
func (p *Plant) Master() MasterData {
	served := p.master
	served.Routings = slices.Clone(p.master.Routings)
	for i := range served.Routings {
		served.Routings[i].Operations = slices.Clone(served.Routings[i].Operations)
		for j := range served.Routings[i].Operations {
			op := &served.Routings[i].Operations[j]
			op.Parameters = slices.Clone(op.Parameters)
			op.Capable = p.capable(*op)
		}
	}
	return served
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

// languages translate the app's titles and descriptions (ADR-0023).
//
//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

// Settings of the plant (ADR-0013), set by administrators in Settings.
const (
	SettingNotifyDowntime = "notify-downtime"
	SettingReasonMinutes  = "reason-reminder-minutes"
	JobReasons            = "downtime-reasons"
)

// Snapshot and Restore: the plant's facts (gateway batches), the
// identities and redirects of downtime events, the derived downtime and the
// decisions; orders and SFCs are the host's records (ADR-0019 D6).
type plantState struct {
	Facts     json.RawMessage       `json:"facts"`
	Identity  kernel.IdentityState  `json:"identity"`
	Downtime  map[string][]Downtime `json:"downtime"`
	NextEvent int                   `json:"nextEvent"`
}

func (p *Plant) Snapshot() (json.RawMessage, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	facts, err := platform.SnapshotFacts(p.facts, p.tenant)
	if err != nil {
		return nil, err
	}
	return p.ledger.SnapshotWith(plantState{Facts: facts, Identity: p.identity.State(), Downtime: p.downtime, NextEvent: p.nextEvent})
}

func (p *Plant) Restore(raw json.RawMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	var s plantState
	if err := p.ledger.RestoreWith(raw, &s); err != nil {
		return err
	}
	p.identity.Restore(s.Identity)
	p.downtime, p.nextEvent = s.Downtime, s.NextEvent
	if p.downtime == nil {
		p.downtime = map[string][]Downtime{}
	}
	return platform.RestoreFacts(p.facts, p.tenant, s.Facts)
}

// Manifest declares the plant as the "mes" app (ADR-0010): its actions, its
// reads, its connector input (batches, journaled; the host keeps the
// connectors), its settings, its scheduled job (ADR-0013), its flow (ADR-0020),
// and the protocol it reaches an ERP through (ADR-0024).
func (p *Plant) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: ID, Title: "MES", Version: "1", Actions: p.ledger.Catalog, Queries: queries, Flows: []platform.Flow{p.confirmation()}, Agents: []platform.Agent{p.fixer(), p.planner()},
		Functions: []platform.AIFunction{platform.RecordAdviceFunction(OrderType, []string{"product", "quantity", "status"}, []string{string(Supervisor)})},
		Pages: []platform.Page{{Name: "shop-orders", Title: "Shop orders", Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: OrderType},
			Layout: "list-detail", ListFields: []string{"product", "quantity", "status", "erp"},
			DetailFields: []string{"product", "quantity", "status", "planned", "sfcs", "erp", "confirmation", "erpDetail"},
			Actions:      []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: SchemaRelease}, {App: ID, Kind: platform.AssetAction, Name: SchemaResend}}}},
		Reads: []string{"master", "products", "planned-orders", "downtime"}, Entities: p.entities,
		Consumes: []platform.Consumption{{Protocol: production.ID, Optional: true}},
		Inputs:   map[string]bool{"states": true},
		Jobs:     []platform.Job{{Name: JobReasons, Title: "Remind supervisors of downtime without a reason", Every: 5 * time.Minute}},
		Emits: []platform.EffectKind{{Name: EffectLeadTime, Title: "Lead time from a supplier",
			Description: "A question to a supplier's agent about how soon it can deliver a product; bind it to the supplier's A2A endpoint."}},
		Settings: []platform.Setting{
			{Name: SettingNotifyDowntime, Title: "Tell supervisors about new downtime", Type: "boolean", Default: "true",
				Description: "Each downtime that starts notifies the supervisors of its line."},
			{Name: SettingReasonMinutes, Title: "Remind about missing reasons after (minutes)", Type: "integer", Default: "15",
				Description: "Downtime still without a reason after this long reminds the line's supervisors once; 0 turns reminders off."},
		}}
}

// Run reminds the supervisors of each line about downtime still without a reason.
func (p *Plant) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	minutes, _ := strconv.Atoi(c.Setting(SettingReasonMinutes))
	if minutes <= 0 {
		return nil
	}
	for _, d := range p.Downtime() {
		if d.Reason == "" && !d.Start.After(now.Add(-time.Duration(minutes)*time.Minute)) {
			c.Notify(platform.Notification{Title: "Downtime without a reason on " + d.Resource,
				Body: fmt.Sprintf("Started %s UTC, still no reason after %d minutes.", d.Start.UTC().Format("15:04"), minutes),
				Ref:  DowntimeType + "/" + d.ID, Key: "reason:" + d.ID}, now, p.supervisorsOf(d.Resource))
		}
	}
	return nil
}

func (p *Plant) Read(c platform.Caller, name string) (any, *kernel.Error) {
	switch name {
	case "master":
		return p.Master(), nil
	case "products":
		return p.Master().Products, nil
	case "planned-orders": // the orders of production.orders/1's providers
		return planned(c), nil
	}
	return p.Downtime(), nil
}

func (p *Plant) Input(c platform.Caller, name string, body []byte, now time.Time) (any, *kernel.Error) {
	switch name {
	case "states":
		var b StateBatch
		if json.Unmarshal(body, &b) != nil {
			return nil, invalid
		}
		return p.DeliverStates(c, b, now)
	}
	return nil, fail(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA)
}

// queries are MES's named queries, shared by pages and agents (ADR-0040 21c).
var queries = []platform.NamedQuery{{Name: "released-orders", Title: "Released orders",
	Description: "Shop orders released to the floor and not yet completed.", Object: OrderType,
	Domain: json.RawMessage(`[["status","=","released"]]`), Sort: []string{"id"}}}
