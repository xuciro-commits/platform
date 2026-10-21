// Package enterprise is the platform's enterprise modeling layer (ADR-0022):
// each tenant's model of its own enterprise — organisations, people, posts,
// capabilities, locations, resources, projects, goals and how they relate —
// typed by the OMG UAF profile the host embeds, drawn in views, seeded by
// scale, and answered to every app through host.Directory. It took over the
// org app of ADR-0022; the "organization" read and the OrgSeed format remain
// as its Personnel-domain projection.
package enterprise

import (
	"encoding/json"
	enterprisemodel "platformserver/internal/enterprise"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/enterprise/uaf"
	"platformserver/platform"
)

const (
	ID               = enterprisemodel.ID
	ElementType      = "enterprise.element"
	RelationshipType = "enterprise.relationship"
	ViewType         = "enterprise.view"
	ModelType        = enterprisemodel.ModelType

	SchemaElementAdd         = "enterprise.element.add"
	SchemaElementEdit        = "enterprise.element.edit"
	SchemaElementClose       = "enterprise.element.close"
	SchemaElementDelete      = "enterprise.element.delete"
	SchemaElementReopen      = "enterprise.element.reopen"
	SchemaElementReinstate   = "enterprise.element.reinstate"
	SchemaRelationshipAdd    = "enterprise.relationship.add"
	SchemaRelationshipEnd    = "enterprise.relationship.end"
	SchemaRelationshipChange = "enterprise.relationship.change"
	SchemaKindAdd            = "enterprise.kind.add"
	SchemaKindEdit           = "enterprise.kind.edit"
	SchemaTenantLinkSave     = "enterprise.tenant-link.save"
	SchemaTenantLinkRemove   = "enterprise.tenant-link.remove"
	TenantLinkType           = "enterprise.tenant-link"
	SchemaViewSave           = "enterprise.view.save"
	SchemaViewDelete         = "enterprise.view.delete"
	SchemaSeed               = "enterprise.model.seed"

	ReadOrganization   = "organization"
	ReadModel          = "enterprise"
	ReadMetamodel      = "enterprise-metamodel"
	ReadPublished      = "enterprise-published" // the slice this tenant shares with the tenants it federates with
	ReadPatterns       = "enterprise-patterns"  // the pattern catalogue with previews
	SchemaPatternApply = "enterprise.pattern.apply"
	SchemaSliceImport  = "enterprise.slice.import"
	SchemaSliceSync    = enterprisemodel.SchemaSliceSync // ADR-0022: a pipeline lands an external system's slice, diffed

	// Admin stewards the model (the org app's role, kept so seats carry over).
	Admin = "admin"
)

// Enterprise is one tenant's enterprise model app.
type Enterprise struct {
	mu     sync.Mutex
	tenant string
	model  enterprisemodel.Model
	ledger *platform.Ledger
}

// declarations is the enterprise action catalog in full — the write contract
// the host enforces and the generated SDK types (ADR-0022 D3): one source for
// the ledger and for every consumer outside this package.
func declarations() []platform.Action {
	admin := []string{Admin}
	f := func(name, typ, description string, required bool) platform.Field {
		return platform.Field{Name: name, Type: typ, Required: required, Description: description}
	}
	from, until := f("from", "date", "Valid from (YYYY-MM-DD; empty: today)", false), f("until", "date", "Valid until, exclusive (empty: open)", false)
	actions := []platform.Action{
		platform.Action{Schema: SchemaElementAdd, Target: ElementType, Capability: "elements", Title: "Add element", Roles: admin,
			Description: "Add an element of the enterprise: an organisation, post, person, capability, location, resource, project or goal, typed by a UAF stereotype.",
			Payload: []platform.Field{f("stereotype", "string", "UAF stereotype, e.g. ActualOrganization, ActualPost, Capability, ActualLocation", true), f("name", "string", "Name", true),
				f("kind", "string", "Kind within the stereotype, e.g. factory, line, team; plant, bin; machine", false), f("shortName", "string", "Short name", false),
				f("legal", "boolean", "A legal entity", false), f("external", "boolean", "Outside the tenant's own enterprise", false), f("properties", "json", "Tagged values of the stereotype", false), from, until}},
		platform.Action{Schema: SchemaElementEdit, Target: ElementType, Capability: "elements", Title: "Edit element", Roles: admin,
			Description: "Rename an element or change its kind, short name or tagged values.",
			Payload: []platform.Field{f("name", "string", "Name", false), f("kind", "string", "Kind", false), f("shortName", "string", "Short name", false), f("properties", "json", "Tagged values", false), f("calendar", "string", "Working calendar id", false),
				f("published", "boolean", "Share it with the tenants this one federates with (group, subsidiaries, partners)", false)}},
		platform.Action{Schema: SchemaElementClose, Target: ElementType, Capability: "elements", Title: "Close element", Roles: admin,
			Description: "End an element (dissolved, merged into another, decommissioned); its history stays.",
			Payload:     []platform.Field{f("until", "date", "Last day + 1", true), f("reason", "string", "e.g. merged into <element>", true)}},
		{Schema: SchemaElementDelete, Target: ElementType, Capability: "elements", Title: "Delete unused element", Roles: admin, Description: "Delete a local element only when no model or business history uses it. Its identity cannot be reused.", Payload: []platform.Field{f("reason", "string", "Reason", true)}},
		{Schema: SchemaElementReopen, Target: ElementType, Capability: "elements", Title: "Reopen element", Roles: admin, Description: "Start a new effective period without changing the closed period.", Payload: []platform.Field{from, f("reason", "string", "Reason", true)}},
		{Schema: SchemaElementReinstate, Target: ElementType, Capability: "elements", Title: "Reinstate element", Roles: admin, Description: "Correct an accidental closure while recording why it was undone.", Payload: []platform.Field{f("reason", "string", "Reason", true)}},
		{Schema: SchemaKindEdit, Target: ModelType, Capability: "kinds", Title: "Rename relationship kind", Roles: admin, Description: "Change a relationship kind's display name without changing its identity or structure.", Payload: []platform.Field{f("name", "string", "Name", true)}},
		{Schema: SchemaTenantLinkSave, Target: TenantLinkType, Capability: "federation", Title: "Save tenant link", Roles: admin, Description: "Associate a local organisation with a shared organisation in another tenant.", Payload: []platform.Field{f("organization", "string", "Local organisation", true), f("tenant", "string", "Source tenant", true), f("root", "string", "Shared root organisation", true), f("connection", "string", "Connection record ID", true)}},
		{Schema: SchemaTenantLinkRemove, Target: TenantLinkType, Capability: "federation", Title: "Remove tenant link", Roles: admin, Description: "Remove the tenant link while retaining imported model history.", Payload: []platform.Field{}},
		platform.Action{Schema: SchemaKindAdd, Target: ModelType, Capability: "kinds", Title: "Add relationship kind", Roles: admin,
			Description: "Add a way organisations relate: legal, management, finance, site, project, governance, community or custom.",
			Payload:     []platform.Field{f("name", "string", "Name", true), f("kind", "string", "Kind", true), f("matrix", "boolean", "An organisation may have several parents", false)}},
		platform.Action{Schema: SchemaRelationshipAdd, Target: RelationshipType, Capability: "relationships", Title: "Relate", Roles: admin,
			Description: "Relate two elements: place an organisation under a parent in a kind, make a party a member, fill a post, give a capability, assign responsibility.",
			Payload: []platform.Field{f("stereotype", "string", "UAF relationship stereotype, e.g. ActualResourceRelationship, ActualOrganizationRole, FillsPost, IsCapableToPerform, ResponsibleFor", true),
				f("source", "string", "Source element id, or member:<id> for a membership", true), f("target", "string", "Target element id", true),
				f("kind", "string", "For a placement: the relationship kind id", false), f("role", "string", "For a membership: employee, chair, volunteer …", false),
				f("relation", "string", "e.g. part of, owned by, reports to", false), f("share", "number", "Ownership share", false), f("primary", "boolean", "The party's primary organisation", false), from, until}},
		platform.Action{Schema: SchemaRelationshipEnd, Target: RelationshipType, Capability: "relationships", Title: "End relationship", Roles: admin,
			Description: "End a relationship on a day; it stays in history.",
			Payload:     []platform.Field{until}},
		platform.Action{Schema: SchemaViewSave, Target: ViewType, Capability: "views", Title: "Save view", Roles: admin,
			Description: "Write a drawing over the model under its own id: the name, the elements it shows, where, the records pinned beside them, and the day it shows. Saving an id writes that view; it never writes another one.",
			Payload: []platform.Field{f("name", "string", "Name", true), f("viewpoint", "string", "The viewpoint the drawing starts from: organization, data, function, output, control", true),
				f("kind", "string", "Relationship kind shown by the view", false), f("context", "json", "Elements explicitly added from other viewpoints", false),
				f("elements", "json", "Element ids shown", false), f("layout", "json", "Positions by element id", false),
				f("pins", "json", "Records pinned on the drawing: {ref, anchor, at}", false), f("asOf", "date", "The day the view shows", false)}},
		platform.Action{Schema: SchemaViewDelete, Target: ViewType, Capability: "views", Title: "Delete view", Roles: admin,
			Description: "Discard a drawing. The model, its elements, their relationships and the records that name them are untouched: a view is a picture, not a fact.",
			Payload:     []platform.Field{f("id", "string", "The view's id", true)}},
		platform.Action{Schema: SchemaSliceImport, Target: ModelType, Capability: "federation", Title: "Import a published slice", Roles: admin,
			Description: "Mirror what another tenant publishes — its organisations, capabilities, sites — read-only, owned there; relate your own elements to them. A connector delivers the slice.",
			Payload:     []platform.Field{f("slice", "json", "The other tenant's enterprise-published answer", true), f("link", "string", "Tenant link", false), f("content", "string", "Shared slice content", false), f("base", "string", "Model version from preview", false), f("connectionRevision", "integer", "Connection revision from preview", false)}},
		platform.Action{Schema: SchemaSliceSync, Target: ModelType, Capability: "federation", Title: "Sync a system's slice", Roles: admin, Automation: true,
			Description: "Land what an external system holds about the enterprise - organisational units, posts, locations, equipment - as elements and relationships owned on its behalf. Repeated syncs diff: new ids start today, missing ids close today, history stays.",
			Payload:     []platform.Field{f("source", "string", "The system's name, e.g. sap-hr", true), f("elements", "json", "Elements with ids stable across syncs", true), f("relationships", "json", "Relationships between them, or to the tenant's own elements", false)}},
		platform.Action{Schema: SchemaPatternApply, Target: ModelType, Capability: "seed", Title: "Add from a pattern", Roles: admin,
			Description: "Graft a ready-made piece of enterprise — a company, plant, hotel, warehouse, department, line — under an organisation, or at the top. Rename and reshape it afterwards like anything else.",
			Payload: []platform.Field{f("pattern", "string", "Pattern id: group, company, plant, hotel, warehouse, office, department, shared-services, line, team", true), f("name", "string", "The new root's name", true),
				f("under", "string", "The organisation it sits under; empty: the top", false), f("params", "json", "Integer knobs by name, e.g. {\"workshops\": 3}", false)}},
		platform.Action{Schema: SchemaSeed, Target: ModelType, Capability: "seed", Title: "Seed from a template", Roles: admin,
			Description: "Give an empty model its first shape for the enterprise's scale: S (≤100 people), M (≤1,000: a plant), L (≤10,000: divisions), XL (≤100,000: a group).",
			Payload: []platform.Field{f("scale", "string", "S, M, L or XL; empty: from headcount", false), f("name", "string", "The enterprise's name", true), f("headcount", "number", "People, roughly", false),
				f("sites", "number", "Sites or plants", false), f("legalEntities", "number", "Legal entities", false), f("industry", "string", "manufacturing, hospitality, services …", false)}},
	}
	// relationship.change is relationship.add that replaces atomically: the
	// same payload plus the new id, declared last as the catalog always did.
	change := platform.Action{}
	for _, a := range actions {
		if a.Schema == SchemaRelationshipAdd {
			change = a
			break
		}
	}
	change.Schema, change.Title, change.Description = SchemaRelationshipChange, "Change relationship", "Replace a relationship atomically; a refusal leaves the existing relationship unchanged."
	change.Payload = append(slices.Clone(change.Payload), f("replacement", "string", "New relationship id", true))
	return append(actions, change)
}

// New is a tenant's enterprise app, starting from an ADR-0022 seed (which an
// industry package or the development seats give) lifted into the model. Its
// ledger is declared from declarations(), the single source of the write
// contract the generated SDK also reads (ADR-0022).
func New(tenant string, seed platform.OrgSeed) *Enterprise {
	catalog := platform.NewCatalog(declarations()...)
	return &Enterprise{tenant: tenant, model: enterprisemodel.FromOrgSeed(seed), ledger: platform.NewLedger(tenant, ID, catalog, ElementType, RelationshipType, ViewType, ModelType, TenantLinkType)}
}

// Snapshot and Restore: the model as decisions left it (ADR-0024).
func (e *Enterprise) SnapshotFormat() string { return "enterprise.model.v1" }

func (e *Enterprise) Snapshot() (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ledger.SnapshotWith(e.model)
}

func (e *Enterprise) Restore(raw json.RawMessage) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.model = enterprisemodel.Model{}
	return e.ledger.RestoreWith(raw, &e.model)
}

func (e *Enterprise) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Enterprise", Version: "1", Actions: e.ledger.Catalog,
		Reads: []string{ReadOrganization, ReadModel, ReadMetamodel, ReadPublished, ReadPatterns}, Everyone: []string{ReadModel, ReadMetamodel, ReadPatterns},
		ReadSources: map[string]string{ReadOrganization: "record", ReadModel: "record", ReadMetamodel: "record", ReadPublished: "record", ReadPatterns: "record"}}
}

// memberOffboard is the console's decision that a member left (ADR-0028 §4):
// the model answers by ending their memberships that day, as its own
// decisions, so the person's tenure reads Until and nothing is deleted.
const memberOffboard = "platform.member.offboard"

// Interested hears the console's offboarding (host.Listener: a platform app
// given another app's events as owned work).
func (e *Enterprise) Interested(names []string, _ platform.Event) bool {
	return slices.Contains(names, memberOffboard)
}

// Listen ends the memberships of a member who left.
func (e *Enterprise) Listen(c platform.Caller, ev platform.Event, _ []string, _ time.Time) *kernel.Error {
	sub := ev.Record.GetSubmission()
	if sub.GetSchema().GetName() != memberOffboard {
		return nil
	}
	party, now := "member:"+sub.GetTarget().GetId(), ev.Record.GetRecordedTime().AsTime()
	day := now.UTC().Format(time.DateOnly)
	e.mu.Lock()
	var open []string
	for _, r := range e.model.Relationships {
		if r.Stereotype == Membership && r.Source == party && r.ActiveOn(day) {
			open = append(open, r.ID)
		}
	}
	e.mu.Unlock()
	for _, id := range open {
		payload, _ := json.Marshal(map[string]string{"until": day})
		if _, err := e.Submit(c, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "offboard:" + party + ":" + id,
			Target: &pb.EntityRef{Type: RelationshipType, Id: id}, Schema: &pb.SchemaRef{Name: SchemaRelationshipEnd, Version: 1}, Payload: payload}, now); err != nil {
			return err
		}
	}
	return nil
}

func (e *Enterprise) Declarations() []*pb.AuthorityDeclaration { return e.ledger.Declarations() }

// Model is a copy of the model, for seeds and tests.
func (e *Enterprise) Model() enterprisemodel.Model {
	e.mu.Lock()
	defer e.mu.Unlock()
	return copyModel(e.model)
}

func copyModel(m enterprisemodel.Model) enterprisemodel.Model {
	raw, _ := json.Marshal(m)
	out := enterprisemodel.Model{Kinds: []enterprisemodel.Kind{}, Elements: []enterprisemodel.Element{}, Relationships: []enterprisemodel.Relationship{}, Views: []enterprisemodel.View{}}
	json.Unmarshal(raw, &out)
	return out
}

type payload struct {
	Stereotype, Name, Kind, ShortName, Reason, Source, Target, Role, Relation, Viewpoint, Scale, Industry string
	Legal, External, Matrix, Primary                                                                      bool
	Share                                                                                                 float64
	Headcount, Sites, LegalEntities                                                                       int
	From, Until, AsOf                                                                                     platform.Date
	Properties                                                                                            map[string]any
	Elements                                                                                              []string
	Layout                                                                                                map[string][2]float64
	Pins                                                                                                  []enterprisemodel.Pin
	Calendar                                                                                              string
	Published                                                                                             *bool
	Slice                                                                                                 *enterprisemodel.Slice
	Pattern, Under                                                                                        string
	Replacement                                                                                           string
	Context                                                                                               []string
	Params                                                                                                enterprisemodel.Params
	Link, Content, Base, Organization, Tenant, Root, Connection                                           string
	ConnectionRevision                                                                                    uint32
}

func (e *Enterprise) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if !c.Replaying {
		var p struct {
			Until, Connection, Link string
			ConnectionRevision      uint32
		}
		if err := json.Unmarshal(s.GetPayload(), &p); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "the payload is not readable")
		}
		switch s.GetSchema().GetName() {
		case SchemaElementClose, SchemaElementDelete:
			if err := c.Enterprise().CheckUsage(s.GetTarget().GetId(), p.Until, s.GetSchema().GetName() == SchemaElementDelete, now); err != nil {
				return nil, err
			}
		case SchemaTenantLinkSave:
			if err := c.Enterprise().CheckTenantConnection(p.Connection, now); err != nil {
				return nil, err
			}
		case SchemaSliceImport:
			if p.Link != "" {
				if err := c.Enterprise().CheckTenantLink(p.Link, p.ConnectionRevision, now); err != nil {
					return nil, err
				}
			}
		}
	}
	if !c.Replaying && s.GetSchema().GetName() == SchemaViewSave {
		var v payload
		if err := json.Unmarshal(s.GetPayload(), &v); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid view")
		}
		pins := v.Pins
		for _, p := range pins {
			if _, err := pinRecord(c, p.Ref, now); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This view contains a record pin you may not read")
			}
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := func(msg string, args ...any) *kernel.Error {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, msg, args...)
		}
		notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		var p payload
		if s.GetSchema().GetName() == SchemaSliceSync { // its elements are objects, not the view's id list
			var head struct {
				Source string `json:"source"`
			}
			json.Unmarshal(s.GetPayload(), &head)
			p.Source = head.Source
		} else if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid("the payload is not readable")
		}
		if c.Replaying && s.GetSchema().GetName() == SchemaViewSave && p.Viewpoint == "" {
			var historical struct{ Grid string }
			json.Unmarshal(s.GetPayload(), &historical)
			p.Viewpoint = enterprisemodel.LegacyViewpoint(historical.Grid)
		}
		today := now.UTC().Format(time.DateOnly)
		if p.From == "" {
			p.From = today
		}
		id := s.GetTarget().GetId()
		mm := uaf.Current()
		m := &e.model
		var supplied map[string]json.RawMessage
		json.Unmarshal(s.GetPayload(), &supplied)
		switch s.GetSchema().GetName() {
		case SchemaElementAdd:
			st := mm.Stereotypes[p.Stereotype]
			switch {
			case p.Name == "":
				return nil, invalid("an element needs a name")
			case st == nil:
				return nil, invalid("{stereotype} is not a UAF {version} stereotype", p.Stereotype, mm.Version)
			case mm.Relationship(p.Stereotype):
				return nil, invalid("{stereotype} relates elements; add it with Relate", p.Stereotype)
			case st.Abstract:
				return nil, invalid("{stereotype} is abstract; choose one of its specialisations", p.Stereotype)
			case p.Until != "" && p.Until <= p.From:
				return nil, invalid("the element would end before it starts")
			case m.ElementOf(id) != nil:
				return nil, conflict
			}
			if err := checkProperties(mm, p.Stereotype, p.Properties, !c.Replaying); err != nil {
				return nil, err
			}
			return func(*pb.ChangeRecord) {
				m.Elements = append(m.Elements, enterprisemodel.Element{ID: id, Stereotype: p.Stereotype, Name: p.Name, ShortName: p.ShortName, Kind: p.Kind, Legal: p.Legal, External: p.External, From: p.From, Until: p.Until, Properties: p.Properties})
			}, nil
		case SchemaKindAdd:
			if p.Name == "" || p.Kind == "" {
				return nil, invalid("a relationship kind needs a name and a kind")
			}
			if m.KindOf(id) != nil {
				return nil, conflict
			}
			return func(*pb.ChangeRecord) {
				m.Kinds = append(m.Kinds, enterprisemodel.Kind{ID: id, Name: p.Name, Kind: p.Kind, Matrix: p.Matrix})
			}, nil
		case SchemaKindEdit:
			kind := m.KindOf(id)
			if kind == nil {
				return nil, notFound
			}
			if kind.Owner != "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The relationship kind is maintained by its source")
			}
			if strings.TrimSpace(p.Name) == "" {
				return nil, invalid("a relationship kind needs a name and a kind")
			}
			return func(*pb.ChangeRecord) { kind.Name = p.Name }, nil
		case SchemaTenantLinkSave:
			organization := m.ElementOf(p.Organization)
			if organization == nil || organization.Deleted || organization.Owner != "" || organization.Stereotype != Organization || !organization.ActiveOn(today) {
				return nil, invalid("Choose an active local organisation for the tenant link")
			}
			if p.Tenant == "" || p.Tenant == e.tenant || p.Root == "" || p.Connection == "" {
				return nil, invalid("A tenant link needs a source tenant, shared root and connection")
			}
			link := enterprisemodel.TenantLink{ID: id, Organization: p.Organization, Tenant: p.Tenant, Root: p.Root, Connection: p.Connection}
			if old := m.TenantLinkOf(id); old != nil {
				if old.Tenant == p.Tenant && old.Root == p.Root && old.Connection == p.Connection {
					link.UpdatedAt, link.Content = old.UpdatedAt, old.Content
				}
				return func(*pb.ChangeRecord) { *old = link }, nil
			}
			return func(*pb.ChangeRecord) { m.TenantLinks = append(m.TenantLinks, link) }, nil
		case SchemaTenantLinkRemove:
			if m.TenantLinkOf(id) == nil {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) {
				m.TenantLinks = slices.DeleteFunc(m.TenantLinks, func(link enterprisemodel.TenantLink) bool { return link.ID == id })
			}, nil
		case SchemaSeed:
			if len(m.Elements) > 0 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "the model already has {n} elements; a template seeds only an empty model", len(m.Elements))
			}
			if p.Name == "" {
				return nil, invalid("the enterprise needs a name")
			}
			seeded, err := enterprisemodel.Template(enterprisemodel.SeedParams{Scale: p.Scale, Name: p.Name, Headcount: p.Headcount, Sites: p.Sites, LegalEntities: p.LegalEntities, Industry: p.Industry, Day: today})
			if err != nil {
				return nil, invalid(err.Error())
			}
			return func(*pb.ChangeRecord) {
				seeded.Calendars = m.Calendars
				*m = seeded
			}, nil
		case SchemaPatternApply:
			probe := copyModel(*m)
			if _, err := probe.Apply(p.Pattern, p.Under, p.Name, p.Params, today); err != nil {
				return nil, invalid(err.Error())
			}
			return func(*pb.ChangeRecord) { m.Apply(p.Pattern, p.Under, p.Name, p.Params, today) }, nil
		case SchemaSliceSync:
			if p.Source == "" || strings.ContainsAny(p.Source, " :/") {
				return nil, invalid("a sync names its source system")
			}
			var sync struct {
				Elements      []enterprisemodel.Element      `json:"elements"`
				Relationships []enterprisemodel.Relationship `json:"relationships"`
			}
			if json.Unmarshal(s.GetPayload(), &sync) != nil || sync.Elements == nil {
				return nil, invalid("a sync carries elements")
			}
			owner, ids := "source:"+p.Source, map[string]bool{}
			stereotypes := map[string]string{}
			for i := range sync.Elements {
				el := &sync.Elements[i]
				switch {
				case el.ID == "" || el.Name == "":
					return nil, invalid("every synced element has an id and a name")
				case mm.Stereotypes[el.Stereotype] == nil || mm.Relationship(el.Stereotype) || mm.Stereotypes[el.Stereotype].Abstract:
					return nil, invalid("{stereotype} is not a concrete UAF {version} element stereotype", el.Stereotype, mm.Version)
				case ids[el.ID]:
					return nil, invalid("{element} is sent twice", el.ID)
				}
				if own := m.ElementOf(el.ID); own != nil && own.Owner != owner {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{element} is not {source}'s to change", el.ID, p.Source)
				}
				if !c.Replaying {
					if el.Deleted {
						return nil, invalid("A source element keeps its identity and type")
					}
					if own := m.ElementOf(el.ID); own != nil && (own.Deleted || own.Stereotype != el.Stereotype) {
						return nil, invalid("A source element keeps its identity and type")
					}
					if err := enterprisemodel.CheckPeriods(el.PeriodsFromLegacy()); err != nil {
						return nil, invalid(err.Error())
					}
				}
				if err := checkProperties(mm, el.Stereotype, el.Properties, !c.Replaying); err != nil {
					return nil, err
				}
				ids[el.ID] = true
				stereotypes[el.ID] = el.Stereotype
			}
			relationshipIDs := map[string]bool{}
			for i := range sync.Relationships {
				r := &sync.Relationships[i]
				if !c.Replaying {
					if relationshipIDs[r.ID] {
						return nil, invalid("The shared slice repeats a relationship")
					}
					relationshipIDs[r.ID] = true
					prefixed := "sync:" + p.Source + ":"
					relID := r.ID
					if !strings.HasPrefix(relID, prefixed) {
						relID = prefixed + relID
					}
					if old := m.RelationshipOf(relID); old != nil && (old.Source != r.Source || old.Target != r.Target || old.Stereotype != r.Stereotype) {
						return nil, invalid("A source relationship keeps its endpoints; use a new relationship ID")
					}
					if err := enterprisemodel.CheckPeriods(r.PeriodsFromLegacy()); err != nil {
						return nil, invalid(err.Error())
					}
				}
				switch {
				case r.ID == "" || r.Source == "" || r.Target == "":
					return nil, invalid("every synced relationship has an id, a source and a target")
				case !mm.Relationship(r.Stereotype):
					return nil, invalid("{stereotype} is not a UAF {version} relationship stereotype", r.Stereotype, mm.Version)
				case !ids[r.Source] && m.ElementOf(r.Source) == nil && !strings.HasPrefix(r.Source, "member:"):
					return nil, invalid("{element} is not in the model nor in the sync", r.Source)
				case !ids[r.Target] && m.ElementOf(r.Target) == nil:
					return nil, invalid("{element} is not in the model nor in the sync", r.Target)
				case r.Stereotype == Placement && r.Kind != "" && m.KindOf(r.Kind) == nil:
					return nil, invalid("{kind} is not a relationship kind of this model", r.Kind)
				}
				// The same contract as Relate (ADR-0022 D2): a slice or a sync
				// cannot write a pair the profile refuses.
				src, dst := m.ElementOf(r.Source), m.ElementOf(r.Target)
				if st, ok := stereotypes[r.Source]; ok {
					src = &enterprisemodel.Element{Stereotype: st}
				}
				if st, ok := stereotypes[r.Target]; ok {
					dst = &enterprisemodel.Element{Stereotype: st}
				}
				if src != nil && dst != nil {
					if ok, why := enterprisemodel.Allowed(enterprisemodel.Contracts(mm), mm, r.Stereotype, src.Stereotype, dst.Stereotype); !ok {
						return nil, invalid(why)
					}
				}
			}
			source := p.Source
			if !c.Replaying {
				candidate := copyModel(*m)
				candidate.Sync(source, sync.Elements, sync.Relationships, today)
				if err := checkSourceRelationships(candidate, mm, "sync:"+source+":", today); err != nil {
					return nil, err
				}
				return func(*pb.ChangeRecord) { *m = candidate }, nil
			}
			return func(*pb.ChangeRecord) { m.ReplaySync(source, sync.Elements, sync.Relationships, today) }, nil
		case SchemaSliceImport:
			if p.Slice == nil || p.Slice.Tenant == "" || p.Slice.Tenant == e.tenant {
				return nil, invalid("a slice names the tenant it comes from")
			}
			if p.Link != "" {
				next, err := m.PreviewImport(*p.Slice, p.Link, p.Content, p.Base, today)
				if err != nil {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
				}
				for _, el := range next.Elements {
					if el.Owner == "tenant:"+p.Slice.Tenant {
						if mm.Stereotypes[el.Stereotype] == nil || mm.Relationship(el.Stereotype) {
							return nil, invalid("Choose a concrete enterprise element type")
						}
					}
				}
				if err := checkSourceRelationships(next, mm, "fed:"+p.Slice.Tenant+":", today); err != nil {
					return nil, err
				}
				return func(*pb.ChangeRecord) { *m = next }, nil
			}
			if !c.Replaying {
				normalized, err := m.NamespaceSlice(*p.Slice)
				if err != nil {
					return nil, invalid(err.Error())
				}
				p.Slice = &normalized
			}
			owner := "tenant:" + p.Slice.Tenant
			stereotypes := map[string]string{}
			for i := range p.Slice.Elements {
				el := &p.Slice.Elements[i]
				if mm.Stereotypes[el.Stereotype] == nil {
					return nil, invalid("{stereotype} is not in UAF {version}", el.Stereotype, mm.Version)
				}
				if own := m.ElementOf(el.ID); own != nil && own.Owner != owner {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{element} is this tenant's own; the slice may not replace it", el.ID)
				}
				stereotypes[el.ID] = el.Stereotype
				el.Owner, el.Published = owner, false
			}
			for _, r := range p.Slice.Relationships {
				src, dst := m.ElementOf(r.Source), m.ElementOf(r.Target)
				if st, ok := stereotypes[r.Source]; ok {
					src = &enterprisemodel.Element{Stereotype: st}
				}
				if st, ok := stereotypes[r.Target]; ok {
					dst = &enterprisemodel.Element{Stereotype: st}
				}
				if src == nil || dst == nil {
					return nil, invalid("{element} is not in the model nor in the slice", r.Source)
				}
				if ok, why := enterprisemodel.Allowed(enterprisemodel.Contracts(mm), mm, r.Stereotype, src.Stereotype, dst.Stereotype); !ok {
					return nil, invalid(why)
				}
			}
			slice := *p.Slice
			if !c.Replaying {
				next := copyModel(*m)
				next.ImportOn(slice, today)
				if err := checkSourceRelationships(next, mm, "fed:"+slice.Tenant+":", today); err != nil {
					return nil, err
				}
				return func(*pb.ChangeRecord) { *m = next }, nil
			}
			return func(*pb.ChangeRecord) { m.Import(slice) }, nil
		case SchemaViewSave:
			if p.Name == "" || p.Viewpoint == "" {
				return nil, invalid("a view needs a name and a viewpoint")
			}
			if !slices.ContainsFunc(enterprisemodel.Views(mm), func(v enterprisemodel.Viewpoint) bool { return v.ID == p.Viewpoint }) {
				return nil, invalid("{viewpoint} is not a view of this model", p.Viewpoint)
			}
			for _, el := range p.Elements {
				if m.ElementOf(el) == nil || !c.Replaying && m.ElementOf(el).Deleted {
					return nil, invalid("the view shows {element}, which is not in the model", el)
				}
			}
			for _, pin := range p.Pins {
				if m.ElementOf(pin.Anchor) == nil || !c.Replaying && m.ElementOf(pin.Anchor).Deleted {
					return nil, invalid("a pinned record sits beside {element}, which is not in the model", pin.Anchor)
				}
				if !strings.HasPrefix(pin.Ref, "record:") || enterprisemodel.RefType(pin.Ref) == "" || enterprisemodel.RefID(pin.Ref) == "" {
					return nil, invalid("a pin is a record reference: record:<entity type>/<id>, not {pin}", pin.Ref)
				}
			}
			kind := p.Kind
			if kind == "" {
				if prev := m.ViewOf(id); prev != nil {
					kind = prev.Kind
				}
			}
			if kind != "" && m.KindOf(kind) == nil {
				return nil, invalid("{kind} is not a relationship kind of this model", kind)
			}
			if p.AsOf != "" {
				if _, err := time.Parse(time.DateOnly, p.AsOf); err != nil {
					return nil, invalid("Invalid date {day}", p.AsOf)
				}
			}
			for _, context := range p.Context {
				if !slices.Contains(p.Elements, context) {
					return nil, invalid("A context element must be on the view")
				}
			}
			return func(*pb.ChangeRecord) {
				v := enterprisemodel.View{ID: id, Name: p.Name, Viewpoint: p.Viewpoint, Kind: kind, Context: p.Context, Elements: p.Elements, Layout: p.Layout, Pins: p.Pins, AsOf: p.AsOf}
				if v.Elements == nil {
					v.Elements = []string{}
				}
				if i := slices.IndexFunc(m.Views, func(x enterprisemodel.View) bool { return x.ID == id }); i >= 0 {
					m.Views[i] = v
				} else {
					m.Views = append(m.Views, v)
				}
			}, nil
		case SchemaViewDelete:
			if m.ViewOf(id) == nil {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) {
				m.Views = slices.DeleteFunc(m.Views, func(x enterprisemodel.View) bool { return x.ID == id })
			}, nil
		case SchemaRelationshipAdd:
			return decideRelationship(m, mm, id, p, !c.Replaying, false)
		case SchemaRelationshipChange:
			previous := m.RelationshipOf(id)
			if previous == nil {
				return nil, notFound
			}
			if !c.Replaying && sourceRelationship(previous.ID) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The relationship is maintained by its source")
			}
			if p.From < previous.From || !previous.ActiveOn(p.From) {
				return nil, invalid("The relationship is not active on {day}", p.From)
			}
			if p.Replacement == "" || p.Replacement == id {
				return nil, invalid("A relationship change needs a new relationship id")
			}
			candidate := copyModel(*m)
			if c.Replaying {
				candidate.RelationshipOf(id).Until = p.From
			} else {
				if err := candidate.RelationshipOf(id).EndOn(p.From); err != nil {
					return nil, invalid(err.Error())
				}
			}
			apply, err := decideRelationship(&candidate, mm, p.Replacement, p, !c.Replaying, false)
			if err != nil {
				return nil, err
			}
			apply(nil)
			return func(*pb.ChangeRecord) { *m = candidate }, nil
		case SchemaRelationshipEnd:
			r := m.RelationshipOf(id)
			if r == nil {
				return nil, notFound
			}
			if !c.Replaying && sourceRelationship(r.ID) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The relationship is maintained by its source")
			}
			day := p.Until
			if day == "" {
				day = today
			}
			if c.Replaying {
				return func(*pb.ChangeRecord) { r.Until = day }, nil
			}
			next := *r
			if err := next.EndOn(day); err != nil {
				return nil, invalid(err.Error())
			}
			return func(*pb.ChangeRecord) { *r = next }, nil
		}
		el := m.ElementOf(id)
		if el == nil {
			return nil, notFound
		}
		if el.Owner != "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{element} belongs to {owner}; it is changed there", el.Name, el.Owner)
		}
		if el.Deleted {
			return nil, notFound
		}
		switch s.GetSchema().GetName() {
		case SchemaElementEdit:
			if !c.Replaying && supplied["name"] != nil && strings.TrimSpace(p.Name) == "" {
				return nil, invalid("an element needs a name")
			}
			if p.Properties != nil {
				if err := checkProperties(mm, el.Stereotype, p.Properties, !c.Replaying); err != nil {
					return nil, err
				}
			}
			return func(*pb.ChangeRecord) {
				if p.Name != "" {
					el.Name = p.Name
				}
				if p.Kind != "" || !c.Replaying && supplied["kind"] != nil {
					el.Kind = p.Kind
				}
				if p.Published != nil {
					el.Published = *p.Published
				}
				if p.ShortName != "" || !c.Replaying && supplied["shortName"] != nil {
					el.ShortName = p.ShortName
				}
				if p.Calendar != "" || !c.Replaying && supplied["calendar"] != nil {
					el.Calendar = p.Calendar
				}
				if p.Properties != nil {
					el.Properties = p.Properties
				}
			}, nil
		case SchemaElementClose:
			if !c.Replaying {
				if strings.TrimSpace(p.Reason) == "" {
					return nil, invalid("Give a reason for changing the element's lifecycle")
				}
				if why := closingUsage(*m, id, p.Until); why != "" {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "End the relationship or tenant link before closing the element: {usage}", why)
				}
				next := *el
				next.Periods = slices.Clone(el.Periods)
				if err := next.CloseOn(p.Until, p.Reason); err != nil {
					return nil, invalid(err.Error())
				}
				return func(*pb.ChangeRecord) { *el = next }, nil
			}
			if p.Until == "" || p.Until <= el.From {
				return nil, invalid("the element would end before it starts")
			}
			return func(*pb.ChangeRecord) { el.Until, el.Closed = p.Until, p.Reason }, nil
		case SchemaElementDelete:
			if why := m.InternalUsage(id); why != "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The element is still used by {usage}", why)
			}
			if strings.TrimSpace(p.Reason) == "" {
				return nil, invalid("Give a reason for changing the element's lifecycle")
			}
			return func(*pb.ChangeRecord) { el.Deleted = true; el.Published = false; el.Closed = p.Reason }, nil
		case SchemaElementReopen, SchemaElementReinstate:
			if strings.TrimSpace(p.Reason) == "" {
				return nil, invalid("Give a reason for changing the element's lifecycle")
			}
			next := *el
			next.Periods = slices.Clone(el.Periods)
			var err error
			if s.GetSchema().GetName() == SchemaElementReopen {
				err = next.ReopenOn(p.From)
			} else {
				err = next.Reinstate()
			}
			if err != nil {
				return nil, invalid(err.Error())
			}
			return func(*pb.ChangeRecord) { *el = next }, nil
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	})
}

// checkProperties: tagged values name properties of the stereotype, and an
// enumeration-typed one holds one of its literals.
func checkProperties(mm *uaf.Metamodel, stereotype string, values map[string]any, strictDates bool) *kernel.Error {
	if len(values) == 0 {
		return nil
	}
	props := mm.Properties(stereotype)
	for name, v := range values {
		if _, bag := v.(map[string]any); bag && strings.HasPrefix(name, "source:") {
			continue // an external system's own attributes, kept under its name (ADR-0022)
		}
		i := slices.IndexFunc(props, func(p uaf.Property) bool { return p.Name == name })
		if i < 0 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{stereotype} has no property {name}", stereotype, name)
		}
		if en := mm.Enumerations[props[i].Type]; en != nil {
			if s, _ := v.(string); s != "" && !slices.Contains(en.Literals, s) {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{name} takes one of {literals}", name, strings.Join(en.Literals, ", "))
			}
		}
		if strictDates && props[i].Type == "ISO8601DateTime" {
			text, ok := v.(string)
			if _, err := time.Parse(time.RFC3339Nano, text); !ok || err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{name} needs a date and time with a timezone", name)
			}
		}
	}
	start, startOK := values["startDate"].(string)
	end, endOK := values["endDate"].(string)
	if strictDates && startOK && endOK {
		a, _ := time.Parse(time.RFC3339Nano, start)
		b, _ := time.Parse(time.RFC3339Nano, end)
		if b.Before(a) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The end date cannot be before the start date")
		}
	}
	return nil
}

// --- host.Directory ---

// Units are the organisations party belongs to on day and those below them
// in kind ("" for the organisations themselves).
func (e *Enterprise) Units(party, kind string, day platform.Date) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.model.UnitsIn(party, kind, day)
}

// Below expands units through active placements in one structure.
func (e *Enterprise) Below(units []string, kind string, day platform.Date) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.model.Descendants(units, kind, day)
}

// Holders are the members holding a membership (with role, when given) in
// unit or in an organisation above it in kind on day.
func (e *Enterprise) Holders(kind, unit, role string, day platform.Date) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	if !e.model.LiveOn(unit, day) {
		return out
	}
	for _, r := range e.model.Relationships {
		id, person := strings.CutPrefix(r.Source, "member:")
		if r.Stereotype != Membership || !person || role != "" && r.Role != role || !r.ActiveOn(day) || !e.model.LiveOn(r.Target, day) || slices.Contains(out, id) {
			continue
		}
		if r.Target == unit || e.model.BelowIn(kind, r.Target, unit, day) {
			out = append(out, id)
		}
	}
	return out
}

// Calendar is the working calendar of party on day: that of its primary
// organisation, else of any of its organisations, found up any kind; else the
// tenant's first; else Monday to Friday (ADR-0038).
func (e *Enterprise) Calendar(party string, day platform.Date) platform.Calendar {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := &e.model
	byID := func(id string) (platform.Calendar, bool) {
		if i := slices.IndexFunc(m.Calendars, func(c platform.Calendar) bool { return c.ID == id }); i >= 0 {
			return m.Calendars[i], true
		}
		return platform.Calendar{}, false
	}
	var units []string
	for _, r := range m.Relationships {
		if r.Stereotype == Membership && r.Source == party && r.ActiveOn(day) && m.LiveOn(r.Target, day) {
			if r.Primary {
				units = append([]string{r.Target}, units...)
			} else {
				units = append(units, r.Target)
			}
		}
	}
	seen := map[string]bool{}
	for len(units) > 0 {
		u := units[0]
		units = units[1:]
		if seen[u] {
			continue
		}
		seen[u] = true
		if x := m.ElementOf(u); x != nil && x.ActiveOn(day) && x.Calendar != "" {
			if c, ok := byID(x.Calendar); ok {
				return c
			}
		}
		for _, r := range m.Relationships {
			if r.Stereotype == Placement && r.Source == u && r.ActiveOn(day) && m.LiveOn(r.Target, day) {
				units = append(units, r.Target)
			}
		}
	}
	if len(m.Calendars) > 0 {
		return m.Calendars[0]
	}
	return platform.Calendar{}
}

// --- reads ---

// Metamodel is what the client draws palettes from: the current release's
// stereotypes in the Enterprise Core enterprisemodel.Profile, with the whole registry behind.
// PatternInfo is a pattern with what it adds by default.
type PatternInfo = enterprisemodel.PatternInfo

type Metamodel = enterprisemodel.Metamodel

// Element and Related serve host.Directory for the apps (ADR-0022 D3).
func (e *Enterprise) Element(id string, day platform.Date) (platform.ElementInfo, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	el := e.model.ElementOf(id)
	if el == nil || !e.model.LiveOn(id, day) {
		return platform.ElementInfo{}, false
	}
	return platform.ElementInfo{ID: el.ID, Stereotype: el.Stereotype, Name: el.Name, Kind: el.Kind, Owner: el.Owner}, true
}

func (e *Enterprise) Related(element, stereotype string, outgoing bool, day platform.Date) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.model.Of(element, stereotype, outgoing, day)
}

func (e *Enterprise) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name == ReadModel {
		return readableModel(c, e.Model(), time.Now()), nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	switch name {
	case ReadOrganization:
		return e.model.OrgSeed(), nil
	case ReadPublished:
		return e.model.Published(e.tenant), nil
	case ReadPatterns:
		out := []enterprisemodel.PatternInfo{}
		for _, p := range enterprisemodel.Patterns() {
			pv, _ := enterprisemodel.PatternPreview(p.ID, "", nil)
			out = append(out, enterprisemodel.PatternInfo{Pattern: p, Preview: pv, LevelName: enterprisemodel.LevelNames[p.Level]})
		}
		return out, nil
	case ReadMetamodel:
		mm := uaf.Current()
		return enterprisemodel.Metamodel{Version: mm.Version, URI: mm.URI, Domains: mm.Domains, Profile: enterprisemodel.Profile(), Stereotypes: mm.Stereotypes, Enumerations: mm.Enumerations,
			Views: enterprisemodel.Views(mm), Contracts: enterprisemodel.Contracts(mm)}, nil
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
