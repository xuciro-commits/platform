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

	"google.golang.org/protobuf/encoding/protojson"

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
)

type Principal struct {
	ID     string   `json:"id"`
	Tenant string   `json:"tenant"`
	Role   Role     `json:"role"`
	Lines  []string `json:"lines"`
}

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
}

// Plant is one tenant: master data, execution state and its kernel logs.
type Plant struct {
	mu          sync.Mutex
	tenant      string
	master      MasterData
	orders      map[string]*Order
	sfcs        map[string]*SFC
	changes     *kernel.ChangeLog
	facts       *kernel.FactLog
	authorities *kernel.Authorities
	identity    *kernel.Identity
	connectors  *kernel.Connectors
	declaration *pb.AuthorityDeclaration
	downtime    map[string][]Downtime // resource → current derived events
	nextEvent   int
	// Record, when set, makes each accepted input durable before it is answered
	// (docs/ADR/0007); Replay rebuilds a plant from the recorded inputs.
	Record func(platformserver.Entry)
}

func NewPlant(tenant string, master MasterData) *Plant {
	schema := func(names ...string) *kernel.SchemaRegistry {
		var refs []*pb.SchemaRef
		for _, n := range names {
			refs = append(refs, &pb.SchemaRef{Name: n, Version: 1})
		}
		return kernel.NewSchemaRegistry(refs, nil)
	}
	p := &Plant{tenant: tenant, master: master, orders: map[string]*Order{}, sfcs: map[string]*SFC{},
		changes:     kernel.NewChangeLog(schema(SchemaRelease, SchemaStart, SchemaComplete, SchemaNC, SchemaSign, SchemaReason)),
		facts:       kernel.NewFactLog(schema(schemaStates, schemaPlanned)),
		authorities: kernel.NewAuthorities(Authority), identity: kernel.NewIdentity(nil),
		connectors: kernel.NewConnectors(), downtime: map[string][]Downtime{}}
	p.changes.Facts = func(tenant, id string) bool {
		return slices.ContainsFunc(p.facts.Records(tenant), func(r *pb.FactRecord) bool { return r.GetFactId() == id })
	}
	for _, class := range []string{OrderType, SFCType, DowntimeType} {
		d := &pb.AuthorityDeclaration{TenantId: tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: Authority, Epoch: 1}
		p.authorities.Declare(d)
	}
	return p
}

func (p *Plant) Declarations() []*pb.AuthorityDeclaration {
	var out []*pb.AuthorityDeclaration
	for _, class := range []string{OrderType, SFCType, DowntimeType} {
		out = append(out, &pb.AuthorityDeclaration{TenantId: p.tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: Authority, Epoch: 1})
	}
	return out
}

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

// allowed is the policy hook (K6 T3). The plant hierarchy (lines) is context the
// domain reads; the kernel never sees it.
func (p *Plant) allowed(who Principal, s *pb.Submission) bool {
	onLine := func(line string) bool { return line != "" && slices.Contains(who.Lines, line) }
	switch s.GetSchema().GetName() {
	case SchemaRelease:
		var r releasePayload
		json.Unmarshal(s.GetPayload(), &r)
		prod := p.product(r.Product)
		return who.Role == Supervisor && prod != nil && len(prod.Operations) > 0 && onLine(p.workCenter(prod.Operations[0].WorkCenter).Line)
	case SchemaStart, SchemaComplete:
		sfc := p.sfcs[s.GetTarget().GetId()]
		return who.Role == Operator && sfc != nil && onLine(p.lineOf(sfc))
	case SchemaNC:
		sfc := p.sfcs[s.GetTarget().GetId()]
		return who.Role == Quality || who.Role == Operator && sfc != nil && onLine(p.lineOf(sfc))
	case SchemaSign:
		return who.Role == Quality
	case SchemaReason:
		refs, _ := p.identity.Resolve(&pb.EntityRef{Type: DowntimeType, Id: s.GetTarget().GetId()})
		return who.Role == Supervisor || who.Role == Operator && len(refs) > 0 && onLine(p.resourceLine(resourceOfEvent(refs[0].ID)))
	}
	return false
}

// Submit receives a decision in the kernel's order (K6 T2) with the plant's policy and rules.
func (p *Plant) Submit(who Principal, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if who.Tenant != p.tenant {
		return nil, denied
	}
	receiver := kernel.Receiver{Changes: p.changes, Authorities: p.authorities,
		Policy: func(_ kernel.Caller, s *pb.Submission) bool { return p.allowed(who, s) }}
	var apply func()
	before := p.accepted()
	record, err := receiver.Receive(kernel.Caller{Tenant: who.Tenant, Principal: who.ID}, s, now, func() *kernel.Error {
		var err *kernel.Error
		apply, err = p.validate(who, s)
		return err
	})
	if err == nil && apply != nil {
		apply()
		if sfc := p.sfcs[s.GetTarget().GetId()]; sfc != nil && s.GetTarget().GetType() == SFCType {
			sfc.Revision = record.GetRevision()
		}
	}
	if err == nil {
		body, _ := protojson.Marshal(s)
		p.record(p.accepted() > before, "submission", who, body, now)
	}
	return record, err
}

// accepted counts the records of both kernel logs; an input that adds none (an
// idempotent replay) is not recorded again.
func (p *Plant) accepted() int { return len(p.changes.Records(p.tenant)) + len(p.facts.Records(p.tenant)) }

func (p *Plant) record(fresh bool, kind string, who Principal, body []byte, now time.Time) {
	if p.Record == nil || !fresh {
		return
	}
	principal, _ := json.Marshal(who)
	p.Record(platformserver.Entry{Kind: kind, Principal: principal, Body: body, At: now})
}

// Replay feeds recorded inputs through the code that first accepted them, as
// the principals they came from; any refusal means the record and the code
// disagree, and the plant must not serve.
func (p *Plant) Replay(entries []platformserver.Entry) error {
	for i, e := range entries {
		var who Principal
		if err := json.Unmarshal(e.Principal, &who); err != nil {
			return fmt.Errorf("entry %d: %w", i+1, err)
		}
		var err *kernel.Error
		switch e.Kind {
		case "submission":
			s := &pb.Submission{}
			if protojson.Unmarshal(e.Body, s) != nil {
				return fmt.Errorf("entry %d: bad submission", i+1)
			}
			_, err = p.Submit(who, s, e.At)
		case "states":
			var b StateBatch
			if json.Unmarshal(e.Body, &b) != nil {
				return fmt.Errorf("entry %d: bad batch", i+1)
			}
			_, err = p.DeliverStates(who, b, e.At)
		case "planned":
			var page PlannedPage
			if json.Unmarshal(e.Body, &page) != nil {
				return fmt.Errorf("entry %d: bad page", i+1)
			}
			err = p.DeliverPlanned(who, page, e.At)
		default:
			return fmt.Errorf("entry %d: unknown kind %q", i+1, e.Kind)
		}
		if err != nil {
			return fmt.Errorf("entry %d (%s): %v", i+1, e.Kind, err)
		}
	}
	return nil
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
func (p *Plant) validate(who Principal, s *pb.Submission) (func(), *kernel.Error) {
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
