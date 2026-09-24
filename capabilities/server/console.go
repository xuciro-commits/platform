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
	"platformserver/platform"
)

// The platform app (ADR-0010) is the tenant's console: its directory of
// members and their role in each app, and every administrator's decision about
// the host's operations — connectors, app settings, owned work, protocol
// bindings, notifications, endpoints and effects. Every change is one of its
// decisions, so it has history, survives restarts through the journal, and
// takes effect on the next request. Each area decides its own target type,
// next to the state it changes (operations.go, effects.go).
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
	platform.Member
	Units []platform.Membership `json:"units,omitempty"` // the member's starting memberships (ADR-0012); Party is filled in
}

// Memberships are the seats' starting memberships, for the org app's seed.
func Memberships(seats []Seat) []platform.Membership {
	var out []platform.Membership
	for _, s := range seats {
		for _, m := range s.Units {
			m.Party = "member:" + s.ID
			out = append(out, m)
		}
	}
	return out
}

type Console struct {
	mu       sync.Mutex
	tenant   string
	members  map[string]*platform.Member
	subjects map[string]string // subject → member ID
	ledger   *platform.Ledger
	t        *Tenant // the tenant running it, once composed (NewTenant)
}

func ConsoleActions() *platform.Catalog {
	admin := []string{Admin}
	app := platform.Field{Name: "app", Type: "string", Required: true, Description: "App ID"}
	return platform.NewCatalog(append([]platform.Action{
		platform.Action{Schema: SchemaAdd, Target: MemberType, Capability: "members", Title: "Add member",
			Description: "Add a member who signs in as a subject: user:<email> for a person, client:<id> for a service or AI agent.",
			Payload: []platform.Field{{Name: "subject", Type: "string", Required: true, Description: "user:<email> or client:<id>"},
				{Name: "agent", Type: "boolean", Description: "An AI agent: its irreversible effects wait for a person's approval"}}, Roles: admin},
		platform.Action{Schema: SchemaGrant, Target: MemberType, Capability: "members", Title: "Grant role",
			Description: "Give a member a role in an app, replacing the role held there.",
			Payload:     []platform.Field{app, {Name: "role", Type: "string", Required: true, Description: "A role the app defines"}}, Roles: admin},
		platform.Action{Schema: SchemaRevoke, Target: MemberType, Capability: "members", Title: "Revoke role",
			Description: "Remove a member's role in an app.", Payload: []platform.Field{app}, Roles: admin},
	}, append(operationsActions(), effectActions()...)...)...)
}

// NewConsole seeds a tenant's directory of members; changes recorded later replay on top.
func NewConsole(tenant string, seats ...Seat) *Console {
	d := &Console{tenant: tenant, members: map[string]*platform.Member{}, subjects: map[string]string{},
		ledger: platform.NewLedger(tenant, PlatformApp, ConsoleActions(), MemberType, ConnectorType, SettingType, WorkType, NotificationType, ProtocolType, EndpointType, EffectType)}
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
func (d *Console) Member(subject string) (platform.Member, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[d.subjects[subject]]
	if m == nil {
		return platform.Member{}, false
	}
	return clone(m), true
}

// holding are the members with role in app, sorted.
func (d *Console) holding(app, role string) []string {
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

// address is the email a member signs in with ("" for services and agents).
func (d *Console) address(member string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for subject, id := range d.subjects {
		if email, ok := strings.CutPrefix(subject, "user:"); ok && id == member {
			out = append(out, email)
		}
	}
	slices.Sort(out)
	if len(out) == 0 {
		return ""
	}
	return out[0]
}

func clone(m *platform.Member) platform.Member {
	out := *m
	out.Roles = maps.Clone(m.Roles)
	return out
}

func (d *Console) Manifest() platform.Manifest {
	return platform.Manifest{ID: PlatformApp, Version: "1", Actions: d.ledger.Catalog,
		Reads:    []string{"members", "audit", "deliveries", "work", "connectors", "settings", "notifications", "endpoints", "effects"},
		Everyone: []string{"notifications"}, Inputs: map[string]bool{"heartbeat": false}}
}

func (d *Console) Declarations() []*pb.AuthorityDeclaration { return d.ledger.Declarations() }

func (d *Console) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		declared, _ := d.ledger.Catalog.Action(s.GetSchema().GetName())
		if declared.Target == MemberType {
			return d.decideMember(s)
		}
		if d.t == nil { // not composed: a tenant's operations need one
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		areas := map[string]func(platform.Caller, *pb.Submission, time.Time) (func(*pb.ChangeRecord), *kernel.Error){
			ConnectorType: d.t.decideConnector, SettingType: d.t.decideSetting, WorkType: d.t.decideWork, ProtocolType: d.t.decideBinding,
			NotificationType: d.t.decideNotification, EndpointType: d.t.decideEndpoint, EffectType: d.t.decideEffect,
		}
		return areas[declared.Target](c, s, now)
	})
}

// decideMember decides the directory's actions: add a member, grant or revoke a role.
func (d *Console) decideMember(s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		Subject, App, Role string
		Agent              bool
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
			d.members[id] = &platform.Member{ID: id, Tenant: d.tenant, Roles: map[string]string{}, Agent: p.Agent}
			d.subjects[p.Subject] = id
		}, nil
	}
	if m == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if s.GetSchema().GetName() == SchemaRevoke {
		if p.App == "" {
			return nil, invalid
		}
		return func(*pb.ChangeRecord) { delete(m.Roles, p.App) }, nil
	}
	// SchemaGrant: only a role the app defines, in an app the tenant runs.
	if d.t == nil {
		return nil, invalid
	}
	if app := d.t.app(p.App); app == nil || !slices.Contains(app.Manifest().Actions.Roles(), p.Role) {
		return nil, invalid
	}
	return func(*pb.ChangeRecord) { m.Roles[p.App] = p.Role }, nil
}

// MemberView is a member with the subjects that sign in as it.
type MemberView struct {
	platform.Member
	Subjects []string `json:"subjects"`
}

// Read "notifications": the caller's own. Every other read is for the tenant's
// administrators only.
func (d *Console) Read(c platform.Caller, name string) (any, *kernel.Error) {
	t := d.t
	if t != nil && name == "notifications" {
		return t.notificationsFor(c.ID), nil
	}
	if c.Role() != Admin || t == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	switch name {
	case "audit":
		return t.Audit(), nil
	case "deliveries":
		return t.Deliveries(), nil
	case "work":
		return t.Tasks(), nil
	case "connectors":
		return t.Connectors(time.Now()), nil
	case "settings":
		return t.Settings(), nil
	case "endpoints":
		return t.Endpoints(), nil
	case "effects":
		return t.Effects(time.Now()), nil
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
func (d *Console) Input(c platform.Caller, name string, _ []byte, now time.Time) (any, *kernel.Error) {
	if name != "heartbeat" || d.t == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	d.t.opsMu.Lock()
	defer d.t.opsMu.Unlock()
	return nil, d.t.connectors.Heartbeat(c.Tenant, c.ID, now)
}
