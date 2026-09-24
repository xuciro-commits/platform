package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// The platform app (ADR-0010): the tenant's directory of members, their role in
// each app, and what administrators see of the tenant.
// Every change is one of its decisions, so the directory has history, survives
// restarts through the journal, and takes effect on the next request.
const (
	PlatformApp  = "platform"
	MemberType   = "platform.member"
	SchemaAdd    = "platform.member.add"
	SchemaGrant  = "platform.member.grant"
	SchemaRevoke = "platform.member.revoke"
	Admin        = "admin"
)

// Seat is a directory entry as configured: the subjects that sign in as the member.
type Seat struct {
	Subjects []string `json:"subjects"` // "user:<email>", "client:<id>", or a development token's subject
	Member
	Units []Membership `json:"units,omitempty"` // the member's starting memberships (ADR-0012); Party is filled in
}

// Memberships are the seats' starting memberships, for the org app's seed.
func Memberships(seats []Seat) []Membership {
	var out []Membership
	for _, s := range seats {
		for _, m := range s.Units {
			m.Party = "member:" + s.ID
			out = append(out, m)
		}
	}
	return out
}

type Directory struct {
	mu       sync.Mutex
	tenant   string
	members  map[string]*Member
	subjects map[string]string // subject → member ID
	ledger   *Ledger
}

func DirectoryActions() *Catalog {
	admin := []string{Admin}
	app := Field{Name: "app", Type: "string", Required: true, Description: "App ID"}
	return NewCatalog(append([]Action{
		Action{Schema: SchemaAdd, Target: MemberType, Capability: "members", Title: "Add member",
			Description: "Add a member who signs in as a subject: user:<email> for a person, client:<id> for a service or AI agent.",
			Payload:     []Field{{Name: "subject", Type: "string", Required: true, Description: "user:<email> or client:<id>"}}, Roles: admin},
		Action{Schema: SchemaGrant, Target: MemberType, Capability: "members", Title: "Grant role",
			Description: "Give a member a role in an app, replacing the role held there.",
			Payload:     []Field{app, {Name: "role", Type: "string", Required: true, Description: "A role the app defines"}}, Roles: admin},
		Action{Schema: SchemaRevoke, Target: MemberType, Capability: "members", Title: "Revoke role",
			Description: "Remove a member's role in an app.", Payload: []Field{app}, Roles: admin},
	}, append(operationsActions(), effectActions()...)...)...)
}

// NewDirectory seeds a tenant's directory; changes recorded later replay on top.
func NewDirectory(tenant string, seats ...Seat) *Directory {
	d := &Directory{tenant: tenant, members: map[string]*Member{}, subjects: map[string]string{},
		ledger: NewLedger(tenant, PlatformApp, DirectoryActions(), MemberType, ConnectorType, SettingType, WorkType, NotificationType, ProtocolType, EndpointType, EffectType)}
	for _, s := range seats {
		m := s.Member
		m.Tenant, m.Roles = tenant, maps.Clone(m.Roles)
		if m.Roles == nil {
			m.Roles = map[string]string{}
		}
		d.members[m.ID] = &m
		for _, subject := range s.Subjects {
			d.subjects[subject] = m.ID
		}
	}
	return d
}

// Member is the member a subject signs in as, with its current roles.
func (d *Directory) Member(subject string) (Member, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[d.subjects[subject]]
	if m == nil {
		return Member{}, false
	}
	return clone(m), true
}

// holding are the members with role in app, sorted.
func (d *Directory) holding(app, role string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for id, m := range d.members {
		if m.Roles[app] == role {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func clone(m *Member) Member {
	out := *m
	out.Roles = maps.Clone(m.Roles)
	return out
}

func (d *Directory) Manifest() Manifest {
	return Manifest{ID: PlatformApp, Version: "1", Actions: d.ledger.Catalog,
		Reads:    []string{"members", "audit", "deliveries", "work", "connectors", "settings", "notifications", "endpoints", "effects"},
		Everyone: []string{"notifications"}, Inputs: map[string]bool{"heartbeat": false}}
}

func (d *Directory) Declarations() []*pb.AuthorityDeclaration { return d.ledger.Declarations() }

func (d *Directory) Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		if c.tenant == nil && !strings.HasPrefix(s.GetSchema().GetName(), "platform.member.") {
			return nil, invalid
		}
		if apply, err, ok := operate(c, s, now); ok {
			return apply, err
		}
		if apply, err, ok := decideEffects(c, s, now); ok {
			return apply, err
		}
		var p struct {
			Subject, App, Role string
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		id := s.GetTarget().GetId()
		m := d.members[id]
		if s.GetSchema().GetName() == SchemaAdd {
			if !strings.HasPrefix(p.Subject, "user:") && !strings.HasPrefix(p.Subject, "client:") {
				return nil, invalid
			}
			if m != nil || d.subjects[p.Subject] != "" {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			return func(*pb.ChangeRecord) {
				d.members[id] = &Member{ID: id, Tenant: d.tenant, Roles: map[string]string{}}
				d.subjects[p.Subject] = id
			}, nil
		}
		if m == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		switch s.GetSchema().GetName() {
		case SchemaGrant:
			// Only a role the app defines, in an app the tenant runs.
			if c.tenant == nil {
				return nil, invalid
			}
			if app := c.tenant.app(p.App); app == nil || !slices.Contains(app.Manifest().Actions.Roles(), p.Role) {
				return nil, invalid
			}
			return func(*pb.ChangeRecord) { m.Roles[p.App] = p.Role }, nil
		case SchemaRevoke:
			if p.App == "" {
				return nil, invalid
			}
			return func(*pb.ChangeRecord) { delete(m.Roles, p.App) }, nil
		}
		return nil, invalid
	})
}

// MemberView is a member with the subjects that sign in as it.
type MemberView struct {
	Member
	Subjects []string `json:"subjects"`
}

// Read "notifications": the caller's own. Every other read is for the tenant's
// administrators only.
func (d *Directory) Read(c Caller, name string) (any, *kernel.Error) {
	if c.tenant != nil && name == "notifications" {
		return c.tenant.notificationsFor(c.ID), nil
	}
	if c.Role() != Admin || c.tenant == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	switch name {
	case "audit":
		return c.tenant.Audit(), nil
	case "deliveries":
		return c.tenant.Deliveries(), nil
	case "work":
		return c.tenant.Tasks(), nil
	case "connectors":
		return c.tenant.Connectors(time.Now()), nil
	case "settings":
		return c.tenant.Settings(), nil
	case "endpoints":
		return c.tenant.Endpoints(), nil
	case "effects":
		return c.tenant.Effects(time.Now()), nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []MemberView{}
	for _, m := range d.members {
		v := MemberView{Member: clone(m), Subjects: []string{}}
		for subject, id := range d.subjects {
			if id == m.ID {
				v.Subjects = append(v.Subjects, subject)
			}
		}
		slices.Sort(v.Subjects)
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b MemberView) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// Input "heartbeat": a connector reports it is alive (not journaled; health
// reads stale after a restart until the next one).
func (d *Directory) Input(c Caller, name string, _ []byte, now time.Time) (any, *kernel.Error) {
	if name != "heartbeat" || c.tenant == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	c.tenant.opsMu.Lock()
	defer c.tenant.opsMu.Unlock()
	return nil, c.tenant.connectors.Heartbeat(c.Tenant, c.ID, now)
}
