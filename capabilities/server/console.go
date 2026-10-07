package platformserver

import (
	"encoding/json"
	"fmt"
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
	PlatformApp     = "platform"
	MemberType      = "platform.member"
	SchemaAdd       = "platform.member.add"
	SchemaGrant     = "platform.member.grant"
	SchemaRevoke    = "platform.member.revoke"
	SettingLanguage = "language"
	SettingCurrency = "currency"
	Admin           = "admin"
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
	subjects map[string]string   // subject → member ID
	profiles map[string]*Profile // member ID → profile (ADR-0079)
	projects map[string]*BuildProject
	packages map[string]*InstalledPackage
	index    *PackageIndex
	ledger   *platform.Ledger
	t        *Tenant // the tenant running it, once composed (NewTenant)
}

func ConsoleActions() *platform.Catalog {
	admin := []string{Admin}
	app := platform.Field{Name: "app", Type: "string", Required: true, Description: "App ID"}
	actions := []platform.Action{
		{Schema: SchemaAdd, Target: MemberType, Capability: "members", Title: "Add member",
			Description: "Add a member who signs in as a subject: user:<email> for a person, client:<id> for a service or AI agent.",
			Payload: []platform.Field{{Name: "subject", Type: "string", Required: true, Description: "user:<email> or client:<id>"},
				{Name: "agent", Type: "boolean", Description: "An AI agent: its irreversible effects wait for a person's approval"}}, Roles: admin},
		{Schema: SchemaGrant, Target: MemberType, Capability: "members", Title: "Grant role",
			Description: "Give a member a role in an app, alongside the roles held there; optionally within a unit and until a day.",
			Payload: []platform.Field{app, {Name: "role", Type: "string", Required: true, Description: "A role the app defines"},
				{Name: "unit", Type: "string", Description: "Only within this unit of the enterprise"},
				{Name: "structure", Type: "string", Description: "The structure the unit belongs to"},
				{Name: "from", Type: "string", Description: "Holds from this day (YYYY-MM-DD)"},
				{Name: "until", Type: "string", Description: "Holds until this day, exclusive (YYYY-MM-DD)"},
				{Name: "reason", Type: "string", Description: "Why"}}, Roles: admin},
		{Schema: SchemaRevoke, Target: MemberType, Capability: "members", Title: "Revoke role",
			Description: "Remove a member's role in an app: one role, or every role held there.",
			Payload: []platform.Field{app, {Name: "role", Type: "string", Description: "Only this role; every role when empty"},
				{Name: "unit", Type: "string", Description: "Only the grant within this unit"}}, Roles: admin},
	}
	actions = append(actions, profileActions()...)
	actions = append(actions, operationsActions()...)
	actions = append(actions, effectActions()...)
	actions = append(actions, operationActions()...)
	actions = append(actions, ProjectActions()...)
	actions = append(actions, PackageActions()...)
	return platform.NewCatalog(actions...)
}

// NewConsole seeds a tenant's directory of members; changes recorded later replay on top.
func NewConsole(tenant string, seats ...Seat) *Console {
	d := &Console{tenant: tenant, members: map[string]*platform.Member{}, subjects: map[string]string{}, profiles: map[string]*Profile{},
		projects: map[string]*BuildProject{},
		packages: map[string]*InstalledPackage{},
		index:    &PackageIndex{},
		ledger:   platform.NewLedger(tenant, PlatformApp, ConsoleActions(), MemberType, ProfileType, ProjectType, PackageType, ConnectorType, SettingType, WorkType, NotificationType, ProtocolType, EndpointType, EffectType, OperationType)}
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

// Identity is a development identity: the token that signs in as a member.
type Identity struct {
	Token  string            `json:"token"`
	Tenant string            `json:"tenant"`
	Member string            `json:"member"`
	Roles  map[string]string `json:"roles"`
}

// Identities are the tenant's subjects with the members they sign in as, for
// a host on development tokens (the token is the subject).
func (d *Console) Identities() []Identity {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []Identity{}
	for subject, id := range d.subjects {
		if m := d.members[id]; m != nil {
			out = append(out, Identity{Token: subject, Tenant: d.tenant, Member: id, Roles: d.currentMember(m).Roles})
		}
	}
	slices.SortFunc(out, func(a, b Identity) int { return strings.Compare(a.Token, b.Token) })
	return out
}

// Snapshot and Restore: the members, how they sign in, and the decisions (ADR-0019 D6).
type consoleState struct {
	Members  map[string]*platform.Member  `json:"members"`
	Subjects map[string]string            `json:"subjects"`
	Profiles map[string]*Profile          `json:"profiles,omitempty"`
	Projects map[string]*BuildProject     `json:"projects,omitempty"`
	Packages map[string]*InstalledPackage `json:"packages,omitempty"`
}

func (d *Console) Snapshot() (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ledger.SnapshotWith(consoleState{Members: d.members, Subjects: d.subjects, Profiles: d.profiles, Projects: d.projects, Packages: d.packages})
}

func (d *Console) Restore(raw json.RawMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var s consoleState
	if err := d.ledger.RestoreWith(raw, &s); err != nil {
		return err
	}
	d.members, d.subjects, d.profiles = s.Members, s.Subjects, s.Profiles
	if d.profiles == nil {
		d.profiles = map[string]*Profile{}
	}
	if s.Projects != nil {
		d.projects = s.Projects
	}
	if s.Packages != nil {
		d.packages = s.Packages
	}
	return nil
}

// Member is the member a subject signs in as, with its current roles.
func (d *Console) Member(subject string) (platform.Member, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[d.subjects[subject]]
	if m == nil {
		return platform.Member{}, false
	}
	return d.currentMember(m), true
}

// holding are the members with role in app, sorted.
func (d *Console) holding(app, role string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []string
	for id, m := range d.members {
		if d.currentMember(m).Holds(app, role) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// addressLocked is the email a member signs in with ("" for services and agents). Call with d.mu held.
func (d *Console) addressLocked(member string) string {
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
	return platform.Manifest{ID: PlatformApp, Title: "Settings", Version: "1", Actions: d.ledger.Catalog, Roles: []string{Admin, Auditor},
		Reads:    []string{"members", "tenant", "account", "audit", "deliveries", "work", "connectors", "settings", "notifications", "endpoints", "effects", "health", "personal-reads", "permissions", "projects", "packages", "contributions"},
		Everyone: []string{"notifications", "tenant", "account"}, Inputs: map[string]bool{"heartbeat": false},
		Settings: tenantSettings()}
}

func (d *Console) Declarations() []*pb.AuthorityDeclaration { return d.ledger.Declarations() }

// Member decisions are the Console's bounded, app-owned state. Host operations
// remain outside this path until their respective owners can be staged too.
func (d *Console) AcceptedLedger() *platform.Ledger { return d.ledger }
func (*Console) AcceptedActionSchemas() []string {
	return []string{SchemaAdd, SchemaGrant, SchemaRevoke, SchemaProfileUpdate, SchemaOperationCall, SchemaProjectSave, SchemaProjectArchive,
		SchemaPackageInstall, SchemaPackageUpgrade, SchemaPackageDrain, SchemaPackageRetire}
}

func (d *Console) ForkAcceptedState() (platform.App, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	members := make(map[string]*platform.Member, len(d.members))
	for id, member := range d.members {
		copy := clone(member)
		members[id] = &copy
	}
	projects := make(map[string]*BuildProject, len(d.projects))
	for id, project := range d.projects {
		copy := *project
		projects[id] = &copy
	}
	packages := make(map[string]*InstalledPackage, len(d.packages))
	for id, p := range d.packages {
		copy := *p
		packages[id] = &copy
	}
	profiles := make(map[string]*Profile, len(d.profiles))
	for id, p := range d.profiles {
		copy := *p
		profiles[id] = &copy
	}
	return &Console{tenant: d.tenant, members: members, subjects: maps.Clone(d.subjects), profiles: profiles, projects: projects,
		packages: packages, index: d.index, ledger: d.ledger, t: d.t}, nil
}

func (d *Console) AcceptedState() (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return json.Marshal(consoleState{Members: d.members, Subjects: d.subjects, Profiles: d.profiles, Projects: d.projects, Packages: d.packages})
}

func (d *Console) ValidateAcceptedState(raw json.RawMessage) error {
	var state consoleState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if state.Members == nil || state.Subjects == nil {
		return fmt.Errorf("accepted directory is incomplete")
	}
	for id, member := range state.Members {
		if id == "" || member == nil || member.ID != id || member.Tenant != d.tenant || member.Roles == nil {
			return fmt.Errorf("accepted directory has invalid member %q", id)
		}
	}
	for subject, id := range state.Subjects {
		if subject == "" || state.Members[id] == nil {
			return fmt.Errorf("accepted directory has invalid subject %q", subject)
		}
	}
	return nil
}

func (d *Console) ApplyAcceptedState(raw json.RawMessage) error {
	if err := d.ValidateAcceptedState(raw); err != nil {
		return err
	}
	var state consoleState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.members, d.subjects, d.profiles = state.Members, state.Subjects, state.Profiles
	if d.profiles == nil {
		d.profiles = map[string]*Profile{}
	}
	return nil
}

// Submit decides the console's actions under the tenant's lock, as every
// decision; its own lock guards only the directory, so a tenant's operations
// (effects, endpoints, settings) may notify members by role as they apply.
func (d *Console) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return d.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		declared, _ := d.ledger.Catalog.Action(s.GetSchema().GetName())
		if declared.Target == OperationType {
			return d.decideOperation(c, s, now)
		}
		if declared.Target == MemberType || declared.Target == ProfileType {
			d.mu.Lock()
			var apply func(*pb.ChangeRecord)
			var err *kernel.Error
			if declared.Target == ProfileType {
				apply, err = d.decideProfile(c, s)
			} else {
				apply, err = d.decideMember(c, s)
			}
			d.mu.Unlock()
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				defer d.mu.Unlock()
				apply(r)
			}, err
		}
		if declared.Target == PackageType {
			apply, err := d.decidePackage(c, s)
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				defer d.mu.Unlock()
				apply(r)
			}, err
		}
		if declared.Target == ProjectType {
			apply, err := d.decideProject(c, s)
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				defer d.mu.Unlock()
				apply(r)
			}, err
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
func (d *Console) decideMember(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		Subject, App, Role, Unit, Structure, From, Until, Reason string
		Agent                                                    bool
	}
	if json.Unmarshal(s.GetPayload(), &p) != nil {
		return nil, invalid
	}
	today := func(m *platform.Member) string { return d.currentMember(m).Today(time.Now()) }
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
		return func(*pb.ChangeRecord) {
			d.deriveRoles(m, today(m))
			m.Grants = slices.DeleteFunc(m.Grants, func(g platform.Grant) bool {
				return g.App == p.App && (p.Role == "" || g.Role == p.Role) && (p.Unit == "" || g.Unit == p.Unit)
			})
			if p.App == "enterprise" && d.enterpriseRoles() && !c.Replaying {
				m.Grants = slices.DeleteFunc(m.Grants, func(g platform.Grant) bool { return g.App == "org" })
			}
			d.deriveRoles(m, today(m))
		}, nil
	}
	// SchemaGrant: only a role the app defines, in an app the tenant runs.
	if d.t == nil {
		return nil, invalid
	}
	if app := d.t.app(p.App); app == nil || !slices.Contains(app.Manifest().AllRoles(), p.Role) {
		return nil, invalid
	}
	if p.Unit != "" && p.Structure == "" {
		return nil, invalid
	}
	for _, day := range []string{p.From, p.Until} {
		if _, err := time.Parse(time.DateOnly, day); day != "" && err != nil {
			return nil, invalid
		}
	}
	return func(r *pb.ChangeRecord) {
		d.deriveRoles(m, today(m))
		m.Grants = slices.DeleteFunc(m.Grants, func(g platform.Grant) bool { return g.App == p.App && g.Role == p.Role && g.Unit == p.Unit })
		if p.App == "enterprise" && d.enterpriseRoles() && !c.Replaying {
			m.Grants = slices.DeleteFunc(m.Grants, func(g platform.Grant) bool { return g.App == "org" })
		}
		m.Grants = append(m.Grants, platform.Grant{App: p.App, Role: p.Role, Unit: p.Unit, Structure: p.Structure,
			From: p.From, Until: p.Until, By: c.ID, Reason: p.Reason, At: r.GetRecordedTime().AsTime()})
		d.deriveRoles(m, today(m))
	}, nil
}

// MemberView is a member with the subjects that sign in as it and its profile.
type MemberView struct {
	platform.Member
	Subjects []string `json:"subjects"`
	Profile  Profile  `json:"profile"`
}

// Read "notifications": the caller's own. Every other read is for the tenant's
// administrators only.
func (d *Console) Read(c platform.Caller, name string) (any, *kernel.Error) {
	t := d.t
	if t != nil && name == "notifications" {
		return t.notificationsFor(c.ID), nil
	}
	if name == "contributions" {
		return d.Contributions(), nil
	}
	if name == "tenant" {
		return d.tenantRecord(), nil
	}
	if name == "account" {
		return d.Account(c.ID), nil
	}
	if t == nil || c.Role() != Admin && (c.Role() != Auditor || !auditorMayRead(name)) {
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
	case "health":
		return t.Health(time.Now()), nil
	case "personal-reads":
		return t.PersonalReads(), nil
	case "permissions":
		return t.Permissions(), nil
	case "projects":
		return d.ProjectViews(), nil
	case "packages":
		return d.PackageViews(), nil
	}
	def := d.defaults()
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []MemberView{}
	for _, m := range d.members {
		v := MemberView{Member: d.currentMember(m), Subjects: []string{}, Profile: d.account(m.ID, def).Profile}
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

// language is the language a member reads (ADR-0023 6b): their own choice,
// else the tenant's default; "" is English, or what the browser asks for.
func (d *Console) language(member string) string {
	d.mu.Lock()
	p := d.profiles[member]
	d.mu.Unlock()
	if p != nil && p.Language != "" {
		return p.Language
	}
	return d.tenantDefault(SettingLanguage)
}

// member is a member of the tenant by ID, with its current roles.
func (t *Tenant) member(id string) (platform.Member, bool) {
	d, ok := t.app(PlatformApp).(*Console)
	if !ok {
		return platform.Member{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[id]
	if m == nil {
		return platform.Member{}, false
	}
	return d.currentMember(m), true
}
