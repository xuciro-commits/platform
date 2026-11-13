package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"sync"
	"time"
)

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

func Memberships(seats []platform.Seat) []platform.Membership {
	var out []platform.Membership
	for _, s := range seats {
		for _, m := range s.Units {
			m.Party = "member:" + s.ID
			out = append(out, m)
		}
	}
	return out
}

type Owners struct {
	Policies platform.ConsolePolicies
	Packages platform.ConsolePackages
	Actions  []platform.Action
}

type IdentityOwner interface {
	platform.IdentityDirectory
	platform.IdentityProfile
	platform.IdentitySessions
	platform.HostIdentities
	platform.IdentityRecovery
	platform.IdentityCounts
	platform.Credentials
	platform.ProjectAccess
}

type Console struct {
	newOwners func(string) Owners
	mu        sync.Mutex
	*Credentials
	tenant          string
	members         map[string]*platform.Member
	subjects        map[string]string
	directoryShared bool
	then            func()
	profiles        map[string]*platform.Profile
	tokens          map[string]*Token
	minted          map[string]bool
	accessRequests  map[string]*AccessRequest
	lastSeen        map[string]time.Time
	lastSeenShared  bool
	acceptedState   json.RawMessage
	acceptedDigest  string
	acceptedValid   bool
	sessions        sessionTable
	platform.ConsolePolicies
	platform.ConsolePackages
	ledger *platform.Ledger
	host   platform.ConsoleHost
}

func ConsoleActions(ownerActions []platform.Action) *platform.Catalog {
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
	actions = append(actions, lifecycleActions()...)
	actions = append(actions, operationsActions()...)
	actions = append(actions, wordsActions()...)
	actions = append(actions, effectActions()...)
	actions = append(actions, operationActions()...)
	actions = append(actions, ownerActions...)
	actions = append(actions, AccessRequestActions()...)
	actions = append(actions, CredentialActions()...)
	return platform.NewCatalog(actions...)
}

func NewConsole(tenant string, newOwners func(string) Owners, seats ...platform.Seat) *Console {
	owners := newOwners(tenant)
	d := &Console{tenant: tenant, newOwners: newOwners, members: map[string]*platform.Member{}, subjects: map[string]string{}, profiles: map[string]*platform.Profile{},
		ConsolePolicies: owners.Policies,
		tokens:          map[string]*Token{}, minted: map[string]bool{}, accessRequests: map[string]*AccessRequest{}, lastSeen: map[string]time.Time{}, sessions: sessionTable{seen: map[string]map[string]*platform.Session{}, revoked: map[string]bool{}},
		ConsolePackages: owners.Packages,
		Credentials:     NewCredentials(tenant),
		ledger:          platform.NewLedger(tenant, PlatformApp, ConsoleActions(owners.Actions), MemberType, ProfileType, platform.MarkingType, platform.RoleType, platform.PolicyType, platform.TeamType, TokenType, platform.ProjectType, AccessRequestType, platform.PackageType, CredentialType, ConnectorType, SettingType, WorkType, NotificationType, ProtocolType, EndpointType, EffectType, OperationType, TranslationType)}
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

func (d *Console) Identities() []platform.LoginIdentity {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []platform.LoginIdentity{}
	for subject, id := range d.subjects {
		if m := d.members[id]; m != nil && m.Status != platform.MemberLeft {
			out = append(out, platform.LoginIdentity{Token: subject, Tenant: d.tenant, Member: id, Roles: d.currentMember(m).Roles})
		}
	}
	slices.SortFunc(out, func(a, b platform.LoginIdentity) int { return strings.Compare(a.Token, b.Token) })
	return out
}

type consoleState struct {
	PackageSources map[string]platform.PackageSource     `json:"packageSources,omitempty"`
	Credentials    map[string]platform.CredentialState   `json:"credentials,omitempty"`
	Members        map[string]*platform.Member           `json:"members"`
	Subjects       map[string]string                     `json:"subjects"`
	Profiles       map[string]*platform.Profile          `json:"profiles,omitempty"`
	Projects       map[string]*platform.BuildProject     `json:"projects,omitempty"`
	Packages       map[string]*platform.InstalledPackage `json:"packages,omitempty"`
	Markings       map[string]*platform.Marking          `json:"markings,omitempty"`
	Roles          map[string]*platform.CustomRole       `json:"roles,omitempty"`
	Policies       map[string]*platform.PolicyRecord     `json:"policies,omitempty"`
	Teams          map[string]*platform.Team             `json:"teams,omitempty"`
	Tokens         map[string]*Token                     `json:"tokens,omitempty"`
	AccessRequests map[string]*AccessRequest             `json:"accessRequests,omitempty"`
}

func (d *Console) state() consoleState {
	policies, packages := d.ConsolePolicies.SnapshotState(), d.ConsolePackages.SnapshotState()
	return consoleState{Credentials: d.Credentials.SnapshotState(), Members: d.members, Subjects: d.subjects, Profiles: d.profiles, Projects: policies.Projects, Packages: packages.Packages, PackageSources: packages.Sources, Markings: policies.Markings, Roles: policies.Roles, Policies: policies.Policies, Teams: policies.Teams, Tokens: d.tokens, AccessRequests: d.accessRequests}
}

func (d *Console) SnapshotFormat() string { return "platform.console.v1" }

func (d *Console) Snapshot() (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ledger.SnapshotWith(d.state())
}

func (d *Console) Restore(raw json.RawMessage) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	var s consoleState
	if err := d.ledger.RestoreWith(raw, &s); err != nil {
		return err
	}
	d.members, d.subjects, d.profiles = s.Members, s.Subjects, s.Profiles
	d.acceptedState = nil
	d.acceptedDigest = ""
	d.acceptedValid = false
	if d.profiles == nil {
		d.profiles = map[string]*platform.Profile{}
	}
	d.restoreAccess(s)
	return nil
}

func (d *Console) restoreAccess(s consoleState) {
	d.ConsolePackages.Restore(platform.PackageState{Packages: s.Packages, Sources: s.PackageSources})
	d.Credentials.Restore(s.Credentials)
	d.ConsolePolicies.Restore(platform.AuthorizationState{Markings: s.Markings, Projects: s.Projects, Roles: s.Roles, Policies: s.Policies, Teams: s.Teams})
	d.tokens = s.Tokens
	if d.tokens == nil {
		d.tokens = map[string]*Token{}
	}
	d.accessRequests = s.AccessRequests
	if d.accessRequests == nil {
		d.accessRequests = map[string]*AccessRequest{}
	}
}

func (d *Console) MemberBySubject(subject string) (platform.Member, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[d.subjects[subject]]
	if m == nil || m.Status == platform.MemberLeft {
		return platform.Member{}, false
	}
	return d.currentMember(m), true
}

func (d *Console) Holding(app, role string) []string {
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

func (d *Console) addressLocked(member string) string {
	if m := d.members[member]; m != nil && m.Status == platform.MemberLeft {
		return ""
	}
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
	out.Grants = slices.Clone(m.Grants)
	out.Scopes = slices.Clone(m.Scopes)
	return out
}

func (d *Console) Manifest() platform.Manifest {
	return platform.Manifest{ID: PlatformApp, Title: platform.BuiltinManifestTitle(PlatformApp), Version: "1", Actions: d.ledger.Catalog, Roles: []string{Admin, platform.Auditor, "confidential", "restricted"},
		Reads:       []string{"members", "tenant", "account", "audit", "deliveries", "work", "connectors", "settings", "notifications", "endpoints", "effects", "health", "personal-reads", "permissions", "access", "access-requests", "packages", "contributions", "credentials", "markings"},
		ReadSources: map[string]string{"members": "record", "tenant": "record", "account": "record", "audit": "record", "deliveries": "record", "connectors": "record", "settings": "record", "endpoints": "record", "health": "record", "personal-reads": "record", "permissions": "record", "access": "record", "access-requests": "record", "packages": "record", "credentials": "record", "contributions": "record", "markings": "record"},
		Everyone:    []string{"notifications", "tenant", "account", "markings", "access-requests"}, Inputs: map[string]bool{"heartbeat": false},
		Settings: tenantSettings()}
}

func (d *Console) Declarations() []*pb.AuthorityDeclaration { return d.ledger.Declarations() }

func (d *Console) AcceptedLedger() *platform.Ledger { return d.ledger }

func (d *Console) Fork(ledger *platform.Ledger) (platform.App, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	state := d.state()
	fork := NewConsole(d.tenant, d.newOwners)
	fork.ledger, fork.host = ledger, d.host
	fork.ConsolePackages.Attach(d.host)
	fork.ConsolePackages.BindPackageIndex(d.ConsolePackages.BoundIndex())
	fork.members, fork.subjects = state.Members, state.Subjects
	fork.ConsolePolicies.Restore(platform.AuthorizationState{Markings: state.Markings, Projects: state.Projects, Roles: state.Roles, Policies: state.Policies, Teams: state.Teams})
	fork.ConsolePolicies.Attach(d.host)
	fork.ConsolePackages.Restore(platform.PackageState{Packages: state.Packages, Sources: state.PackageSources})
	if state.Credentials != nil {
		fork.Credentials.Restore(state.Credentials)
	}
	if state.Profiles != nil {
		fork.profiles = state.Profiles
	}
	if state.Tokens != nil {
		fork.tokens = state.Tokens
	}
	fork.accessRequests = maps.Clone(state.AccessRequests)
	if fork.accessRequests == nil {
		fork.accessRequests = map[string]*AccessRequest{}
	}
	fork.minted, fork.lastSeen = maps.Clone(d.minted), d.lastSeen
	d.directoryShared, fork.directoryShared = true, true
	d.lastSeenShared, fork.lastSeenShared = true, true
	return fork, nil
}

func (d *Console) AcceptedState() (json.RawMessage, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.acceptedState == nil {
		raw, err := json.Marshal(d.state())
		if err != nil {
			return nil, err
		}
		d.acceptedState = raw
	}
	return d.acceptedState, nil
}

func (d *Console) AcceptedStateDigest(raw json.RawMessage) (string, error) {
	d.mu.Lock()
	current := bytes.Equal(raw, d.acceptedState)
	digest := d.acceptedDigest
	d.mu.Unlock()
	if current && digest != "" {
		return digest, nil
	}
	digest, err := platform.CanonicalDigest(raw)
	if err != nil || !current {
		return digest, err
	}
	d.mu.Lock()
	if bytes.Equal(raw, d.acceptedState) {
		d.acceptedDigest = digest
	}
	d.mu.Unlock()
	return digest, nil
}

func (d *Console) ValidateAcceptedState(raw json.RawMessage) error {
	d.mu.Lock()
	valid := bytes.Equal(raw, d.acceptedState) && d.acceptedValid
	d.mu.Unlock()
	if valid {
		return nil
	}
	var state consoleState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if err := d.validateAcceptedState(state); err != nil {
		return err
	}
	d.mu.Lock()
	if bytes.Equal(raw, d.acceptedState) {
		d.acceptedValid = true
	}
	d.mu.Unlock()
	return nil
}

func (d *Console) validateAcceptedState(state consoleState) error {
	if state.Members == nil || state.Subjects == nil {
		return fmt.Errorf("accepted directory is incomplete")
	}
	for digest, source := range state.PackageSources {
		if digest != source.Digest || !platform.ValidDigest(digest) || source.ID == "" || source.Version == "" || source.Origin == "" {
			return fmt.Errorf("accepted package source is incomplete")
		}
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
	for id, request := range state.AccessRequests {
		if id == "" || request == nil || request.ID != id || request.Requester == "" || request.State == "" {
			return fmt.Errorf("accepted access request %q is incomplete", id)
		}
	}
	return nil
}

func (d *Console) ApplyAcceptedState(raw json.RawMessage) error {
	var state consoleState
	if err := json.Unmarshal(raw, &state); err != nil {
		return err
	}
	if err := d.validateAcceptedState(state); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	for id, member := range state.Members {
		if old := d.members[id]; old != nil && old.Status != member.Status && !member.Active() {
			d.endSessions(id, "", now)
		}
	}
	for id, token := range d.tokens {
		if state.Tokens[id] == nil {
			d.endSessions(token.Member, id, now)
		}
	}
	d.members, d.subjects, d.profiles = state.Members, state.Subjects, state.Profiles
	d.acceptedState = slices.Clone(raw)
	d.acceptedDigest = ""
	d.acceptedValid = true
	if d.profiles == nil {
		d.profiles = map[string]*platform.Profile{}
	}
	d.restoreAccess(state)
	return nil
}

func (d *Console) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return d.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		declared, _ := d.ledger.Catalog.Action(s.GetSchema().GetName())
		if declared.Target == OperationType {
			return d.decideOperation(c, s, now)
		}
		if declared.Target == MemberType || declared.Target == ProfileType || declared.Target == platform.MarkingType || declared.Target == platform.RoleType || declared.Target == platform.PolicyType || declared.Target == platform.TeamType || declared.Target == TokenType {
			d.mu.Lock()
			if d.directoryShared && (declared.Target == MemberType || declared.Target == ProfileType || declared.Target == TokenType) {
				d.acceptedState = nil
				d.acceptedDigest = ""
				d.acceptedValid = false
				d.members, d.subjects = maps.Clone(d.members), maps.Clone(d.subjects)
				d.profiles, d.tokens = maps.Clone(d.profiles), maps.Clone(d.tokens)
				d.directoryShared = false
			}
			id := s.GetTarget().GetId()
			if declared.Target == MemberType {
				ids := []string{id}
				if s.GetSchema().GetName() == SchemaOffboard {
					ids = slices.Collect(maps.Keys(d.members))
				}
				for _, memberID := range ids {
					if member := d.members[memberID]; member != nil {
						copy := clone(member)
						d.members[memberID] = &copy
					}
				}
			}
			if declared.Target == ProfileType {
				if profile := d.profiles[id]; profile != nil {
					copy := *profile
					d.profiles[id] = &copy
				}
			}
			var apply func(*pb.ChangeRecord)
			var err *kernel.Error
			switch {
			case declared.Target == ProfileType:
				apply, err = d.decideProfile(c, s)
			case declared.Target == TokenType:
				apply, err = d.decideToken(c, s)
			case declared.Target != MemberType:
				apply, err = d.ConsolePolicies.PrepareAccess(c, s, slices.Collect(maps.Keys(d.members)))
			case s.GetSchema().GetName() == platform.SchemaDelegate:
				apply, err = d.decideDelegate(c, s)
			case slices.Contains([]string{SchemaInvite, SchemaJoin, SchemaMemberSuspend, SchemaMemberResume, SchemaOffboard}, s.GetSchema().GetName()):
				apply, err = d.decideLifecycle(c, s)
			default:
				apply, err = d.decideMember(c, s)
			}
			d.mu.Unlock()
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				d.acceptedState = nil
				d.acceptedDigest = ""
				d.acceptedValid = false
				apply(r)
				then := d.then
				d.then = nil
				d.mu.Unlock()
				if then != nil {
					then()
				}
			}, err
		}
		if declared.Target == CredentialType {
			apply, err := d.Credentials.Prepare(c, s, now)
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				defer d.mu.Unlock()
				d.acceptedState, d.acceptedDigest, d.acceptedValid = nil, "", false
				apply(r)
			}, err
		}
		if declared.Target == platform.PackageType {
			apply, err := d.ConsolePackages.Prepare(c, s)
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				defer d.mu.Unlock()
				d.acceptedState, d.acceptedDigest, d.acceptedValid = nil, "", false
				apply(r)
			}, err
		}
		if declared.Target == platform.ProjectType {
			d.mu.Lock()
			members := slices.Collect(maps.Keys(d.members))
			d.mu.Unlock()
			apply, err := d.ConsolePolicies.PrepareProject(c, s, members)
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				defer d.mu.Unlock()
				d.acceptedState, d.acceptedDigest, d.acceptedValid = nil, "", false
				apply(r)
			}, err
		}
		if declared.Target == AccessRequestType {
			d.mu.Lock()
			apply, after, err := d.decideAccessRequest(c, s, now)
			d.mu.Unlock()
			if apply == nil {
				return nil, err
			}
			return func(r *pb.ChangeRecord) {
				d.mu.Lock()
				d.acceptedState, d.acceptedDigest, d.acceptedValid = nil, "", false
				apply(r)
				d.mu.Unlock()
				if after != nil {
					if failure := after(r); failure != nil {
						c.FailDecision(failure)
					}
				}
			}, err
		}
		if d.host == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		return platform.ControlDecision(c, s, now)
	})
}

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
		bound := d.members[d.subjects[p.Subject]]
		if m != nil || bound != nil && bound.Status != platform.MemberLeft {
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
			d.migrateGrants(m)
			m.Grants = slices.DeleteFunc(m.Grants, func(g platform.Grant) bool {
				return g.App == p.App && (p.Role == "" || g.Role == p.Role) && (p.Unit == "" || g.Unit == p.Unit)
			})
			d.deriveRoles(m, today(m))
		}, nil
	}
	if d.host == nil {
		return nil, invalid
	}
	if app := d.host.App(p.App); app == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Application {app} is not active in this tenant.", p.App)
	} else if !slices.Contains(app.Manifest().AllRoles(), p.Role) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Role {role} is not declared by application {app}. Publish its definitions before granting business roles.", p.Role, p.App)
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
		d.migrateGrants(m)
		m.Grants = slices.DeleteFunc(m.Grants, func(g platform.Grant) bool { return g.App == p.App && g.Role == p.Role && g.Unit == p.Unit })
		m.Grants = append(m.Grants, platform.Grant{App: p.App, Role: p.Role, Unit: p.Unit, Structure: p.Structure,
			From: p.From, Until: p.Until, By: c.ID, Reason: p.Reason, At: r.GetRecordedTime().AsTime()})
		d.deriveRoles(m, today(m))
	}, nil
}

func (d *Console) Read(c platform.Caller, name string) (any, *kernel.Error) {
	h := d.host
	if h != nil && name == "notifications" {
		return h.GovernanceRead(c, name)
	}
	if name == "contributions" {
		return d.Contributions(), nil
	}
	if name == "tenant" {
		return d.TenantRecord(), nil
	}
	if name == "account" {
		return d.Account(c.ID), nil
	}
	if name == "credentials" {
		if !c.Holds(PlatformApp, Admin) && !c.Holds("ai", "admin") {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Credential metadata requires platform or AI administration")
		}
		return d.Credentials.Catalog(), nil
	}
	if name == "packages" && !c.Holds(PlatformApp, Admin) && !c.Holds(PlatformApp, platform.Auditor) && c.Member.InScope("build.app.create") && c.May("build", "build.app.create", []string{"builder"}).Allow {
		return d.ConsolePackages.BlueprintViews(), nil
	}
	if name == "markings" {
		markings := d.ConsolePolicies.MarkingViews()
		for i := range markings {
			markings[i].Cleared = slices.Contains(c.Member.Markings, markings[i].ID)
			if markings[i].Classification {
				markings[i].Cleared = platform.MarkingCleared(c.Member, markings[i].ID)
			}
			markings[i].CanDeclassify = c.Member.CanDeclassify([]string{markings[i].ID})
			markings[i].RID = platform.ResourceID(c.Tenant, platform.AssetRef{App: PlatformApp, Kind: "marking", Name: markings[i].ID})
			if !c.Holds(PlatformApp, Admin) && !c.Holds(PlatformApp, platform.Auditor) {
				markings[i].Members, markings[i].Teams, markings[i].Declassifiers, markings[i].DeclassifierTeams = nil, nil, nil, nil
			}
		}
		return markings, nil
	}
	if name == "access-requests" {
		return d.AccessRequests(c), nil
	}
	if h == nil || !c.Holds(PlatformApp, Admin) && (!c.Holds(PlatformApp, platform.Auditor) || !platform.AuditorMayRead(name)) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	switch name {
	case "audit", "deliveries", "work", "connectors", "settings", "endpoints", "effects", "health", "personal-reads", "permissions":
		return h.GovernanceRead(c, name)
	case "access":
		return d.Access(), nil
	case "packages":
		return d.PackageViews(), nil
	}
	def := d.defaults()
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []platform.MemberView{}
	for _, m := range d.members {
		v := platform.MemberView{Member: d.currentMember(m), Subjects: []string{}, Profile: d.account(m.ID, def).Profile}
		for subject, id := range d.subjects {
			if id == m.ID {
				v.Subjects = append(v.Subjects, subject)
			}
		}
		slices.Sort(v.Subjects)
		out = append(out, v)
	}
	slices.SortFunc(out, func(a, b platform.MemberView) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (d *Console) Input(c platform.Caller, name string, _ []byte, now time.Time) (any, *kernel.Error) {
	if name != "heartbeat" || d.host == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	return nil, d.host.ConnectorHeartbeat(c, now)
}

func (d *Console) Language(member string) string {
	d.mu.Lock()
	preferred := ""
	if p := d.profiles[member]; p != nil {
		preferred = p.Language
	} else if m := d.members[member]; m != nil {
		preferred = m.Language
	}
	d.mu.Unlock()
	if preferred != "" {
		return preferred
	}
	return d.tenantDefault(SettingLanguage)
}

const OperationType = "platform.operation"

const SchemaOperationCall = OperationType + ".call"

func operationActions() []platform.Action {
	return []platform.Action{{Schema: SchemaOperationCall, Target: OperationType, Capability: "compute", Title: "Call operation", Description: "Invoke a registered typed operation through its owner and accepted work.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "The operation owner"}, {Name: "name", Type: "string", Required: true, Description: "The registered operation name"}, {Name: "version", Type: "integer", Description: "The retained owner version"}, {Name: "key", Type: "string", Required: true, Description: "The stable invocation key"}, {Name: "inputs", Type: "json", Required: true, Description: "The typed operation input"}, {Name: "sources", Type: "string[]", Description: "Protected source references"}}}}
}

func (d *Console) decideOperation(c platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var q platform.OperationRequest
	if json.Unmarshal(s.GetPayload(), &q) != nil || q.Key != s.GetIdempotencyKey() || s.GetTarget().GetId() != q.Key || q.App == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an operation and stable input key")
	}
	return func(r *pb.ChangeRecord) { _, _ = c.RequestOperation(r, q) }, nil
}

const (
	ConnectorType          = "platform.connector"
	SettingType            = "platform.setting"
	WorkType               = "platform.work"
	NotificationType       = "platform.notification"
	SchemaConnectorOn      = "platform.connector.enable"
	SchemaConnectorOff     = "platform.connector.disable"
	SchemaSettingSet       = "platform.setting.set"
	SchemaWorkRetry        = "platform.work.retry"
	SchemaNotificationRead = "platform.notification.read"
	ProtocolType           = "platform.protocol"
	SchemaProtocolBind     = "platform.protocol.bind"
)

func operationsActions() []platform.Action {
	admin := []string{Admin}
	return []platform.Action{
		{Schema: SchemaConnectorOn, Target: ConnectorType, Capability: "integrations", Title: "Enable connector",
			Description: "Accept the connector's deliveries again.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaConnectorOff, Target: ConnectorType, Capability: "integrations", Title: "Disable connector",
			Description: "Refuse the connector's deliveries until it is enabled; its cursor is kept.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaSettingSet, Target: SettingType, Capability: "settings", Title: "Change app setting",
			Description: "Set an app's setting (target <app>/<name>) to a value of its type.",
			Payload:     []platform.Field{{Name: "value", Type: "string", Description: "true/false, a whole number, one of the choices, or text; empty clears a text setting"}}, Roles: admin},
		{Schema: SchemaWorkRetry, Target: WorkType, Capability: "automation", Title: "Retry work",
			Description: "Queue a failed event delivery again, or run a scheduled job now.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaProtocolBind, Target: ProtocolType, Capability: "apps", Title: "Choose protocol provider",
			Description: "Send the tenant's new calls of a protocol (target <name>/<version>) to another app that provides it; what every provider holds stays readable.",
			Payload:     []platform.Field{{Name: "provider", Type: "string", Required: true, Description: "App ID of a provider"}}, Roles: admin},
		{Schema: SchemaNotificationRead, Target: NotificationType, Capability: "notifications", Title: "Mark notification read",
			Description: "Mark one of your notifications as read.", Payload: []platform.Field{}, Roles: []string{platform.AnyMember}},
	}
}

const (
	BuildApp             = "build"
	TranslationType      = "platform.translation"
	SchemaTranslationSet = "platform.translation.set"
)

func wordsActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaTranslationSet, Target: TranslationType, Capability: "settings", Title: "Translate a text",
			Description: "Say a declaration text (an object type, field, state, action or application name the tenant defined, or any shipped one) in a language (target: the language tag, such as zh-CN). An empty translation removes the tenant's entry.",
			Payload: []platform.Field{
				{Name: "text", Type: "string", Required: true, Description: "The text as declared, in its source language"},
				{Name: "translation", Type: "string", Description: "What it reads as in the target language; empty removes"},
			}, Roles: []string{Admin}},
	}
}

const (
	EndpointType         = "platform.endpoint"
	EffectType           = "platform.effect"
	SchemaEndpointAdd    = "platform.endpoint.add"
	SchemaEndpointRemove = "platform.endpoint.remove"
	SchemaEffectRetry    = "platform.effect.retry"
	SchemaEffectDiscard  = "platform.effect.discard"
	SchemaEffectApprove  = "platform.effect.approve"
	SchemaEffectResolve  = "platform.effect.resolve"
)

func effectActions() []platform.Action {
	admin := []string{Admin}
	return []platform.Action{
		{Schema: SchemaEndpointAdd, Target: EndpointType, Capability: "integrations", Title: "Add endpoint",
			Description: "Send the chosen events and effects to a URL, signed with a named secret (Standard Webhooks); or mail the chosen apps' notifications to members through an SMTP server.",
			Payload: []platform.Field{{Name: "url", Type: "string", Required: true, Description: "https URL of the receiver"},
				{Name: "secret", Type: "string", Description: "Name of the signing secret in the secret store"},
				{Name: "events", Type: "string[]", Description: "Action schemas or <protocol id>#<event> to send as webhooks"},
				{Name: "effects", Type: "string[]", Description: "Effect kinds apps emit, <app>/<kind>, to send here"},
				{Name: "kind", Type: "string", Description: "webhook (default), email, or a2a (an external agent's A2A 1.0 JSON-RPC URL; the secret is its bearer token)"},
				{Name: "irreversible", Type: "boolean", Description: "What the receiver does cannot be recalled: effects agents cause wait for a person"},
				{Name: "from", Type: "string", Description: "email: the sender address"},
				{Name: "notifications", Type: "string[]", Description: "email: apps whose notifications are mailed to members"},
				{Name: "allowPrivate", Type: "boolean", Description: "Allow private addresses and plain http (inside the deployment only)"}}, Roles: admin},
		{Schema: SchemaEndpointRemove, Target: EndpointType, Capability: "integrations", Title: "Remove endpoint",
			Description: "Stop sending to the endpoint; what it has not received is discarded.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaEffectRetry, Target: EffectType, Capability: "integrations", Title: "Retry effect",
			Description: "Send a failed or rejected effect again, with the same key.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaEffectResolve, Target: EffectType, Capability: "integrations", Title: "Resolve unknown delivery",
			Description: "Record checked external evidence before resolving an unknown outcome; the original effect is not sent again.", Roles: admin,
			Payload: []platform.Field{{Name: "outcome", Type: "string", Required: true, Choices: []string{"delivered", "rejected"}, Description: "Confirmed external result"}, {Name: "reason", Type: "string", Required: true, Description: "Evidence supporting the decision"}, {Name: "answer", Type: "json", Description: "Confirmed external answer"}}},
		{Schema: SchemaEffectDiscard, Target: EffectType, Capability: "integrations", Title: "Discard effect",
			Description: "Give up on an effect not yet delivered.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaEffectApprove, Target: EffectType, Capability: "integrations", Title: "Approve effect",
			Description: "Send an irreversible effect an AI agent caused; only a person may approve it.", Payload: []platform.Field{}, Roles: admin},
	}
}
