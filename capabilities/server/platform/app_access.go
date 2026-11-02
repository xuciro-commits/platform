package platform

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"slices"
	"strings"
	"time"
)

type CustomRole struct {
	ID      string   `json:"id"`
	App     string   `json:"app"`
	Title   string   `json:"title"`
	Actions []string `json:"actions"`
}

type PolicyRecord struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Effect     string            `json:"effect"`
	Permission string            `json:"permission"`
	Where      map[string]string `json:"where,omitempty"`
	From       string            `json:"from,omitempty"`
	Until      string            `json:"until,omitempty"`
}

type Team struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
	Grants  []Grant  `json:"grants"`
}

type AccessConfig struct {
	Roles    []CustomRole   `json:"roles"`
	Policies []PolicyRecord `json:"policies"`
	Teams    []Team         `json:"teams"`
	Markings []Marking      `json:"markings,omitempty"`
}

type ProjectMember struct {
	Member string `json:"member" field:"required" title:"Member" help:"A member's ID in this tenant"`
	Role   string `json:"role,omitempty" field:"required,choice:owner,editor,viewer,discoverer" title:"Role" help:"owner: edits the project's resources and manages its access; editor: edits its resources; viewer: opens them without editing"`
}

type ProjectAsset struct {
	App  string `json:"app,omitempty" title:"App" help:"The app the asset belongs to; empty: any"`
	Kind string `json:"kind" field:"required,choice:object,page,query,function,process,code,propertytype,linktype" title:"Kind"`
	Name string `json:"name" field:"required" title:"Name" help:"The asset's name inside its owner app"`
}

type BuildProject struct {
	Record
	Approvals int             `json:"approvals,omitempty"`
	Title     string          `json:"title" field:"required,search" title:"What people call it" example:"Spring rollout"`
	Members   []ProjectMember `json:"members,omitempty" title:"Members"`
}

type ProjectPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func ProjectManager(m Member, role string) bool {
	if role == ProjectOwnerRole || m.Holds("platform", "admin") {
		return true
	}
	for _, g := range m.Grants {
		if g.App == "build" && g.Role == "builder" && !strings.HasPrefix(g.By, "project:") {
			return true
		}
	}
	return false
}

type ProjectAccessView struct {
	Approvals int             `json:"approvals"`
	Members   []ProjectMember `json:"members"`
	Manage    bool            `json:"manage"`
	People    []ProjectPerson `json:"people,omitempty"`
}

type Profile struct {
	Member       string    `json:"member"`
	DisplayName  string    `json:"displayName,omitempty"`
	GivenName    string    `json:"givenName,omitempty"`
	FamilyName   string    `json:"familyName,omitempty"`
	Title        string    `json:"title,omitempty"`
	Pronouns     string    `json:"pronouns,omitempty"`
	Email        string    `json:"email,omitempty"`
	Phone        string    `json:"phone,omitempty"`
	Language     string    `json:"language,omitempty"`
	Timezone     string    `json:"timezone,omitempty"`
	DateFormat   string    `json:"dateFormat,omitempty"`
	NumberFormat string    `json:"numberFormat,omitempty"`
	WeekStart    string    `json:"weekStart,omitempty"`
	InApp        *bool     `json:"inApp,omitempty"`
	Mail         *bool     `json:"mail,omitempty"`
	Digest       string    `json:"digest,omitempty"`
	QuietFrom    string    `json:"quietFrom,omitempty"`
	QuietTo      string    `json:"quietTo,omitempty"`
	HomePage     string    `json:"homePage,omitempty"`
	Theme        string    `json:"theme,omitempty"`
	Density      string    `json:"density,omitempty"`
	LastSeen     time.Time `json:"lastSeen,omitzero"`
}

type Account struct {
	Profile
	Effective struct {
		Language     string `json:"language"`
		Timezone     string `json:"timezone"`
		DateFormat   string `json:"dateFormat"`
		NumberFormat string `json:"numberFormat"`
		WeekStart    string `json:"weekStart"`
		Email        string `json:"email"`
		Digest       string `json:"digest"`
	} `json:"effective"`
	Subjects []string `json:"subjects"`
}

type TenantRecord struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Settings map[string]string `json:"settings"`
	Apps     []string          `json:"apps"`
	Members  int               `json:"members"`
	Status   string            `json:"status"`
	Since    time.Time         `json:"since,omitempty"`
}

type Reach struct {
	InApp, Mail bool
	Address     string
	Language    string
	Digest      string
	QuietFrom   string
	QuietTo     string
	Location    *time.Location
}

func (p Reach) Due(now time.Time) time.Time {
	loc := p.Location
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	due := local
	switch p.Digest {
	case "hourly":
		due = local.Truncate(time.Hour).Add(time.Hour)
	case "daily":
		due = time.Date(local.Year(), local.Month(), local.Day(), 8, 0, 0, 0, loc)
		if !due.After(local) {
			due = due.AddDate(0, 0, 1)
		}
	}
	if p.QuietFrom != "" && p.QuietTo != "" {
		from, errFrom := time.Parse("15:04", p.QuietFrom)
		to, errTo := time.Parse("15:04", p.QuietTo)
		if errFrom == nil && errTo == nil {
			at := func(t time.Time, h time.Time) time.Time {
				return time.Date(t.Year(), t.Month(), t.Day(), h.Hour(), h.Minute(), 0, 0, loc)
			}
			start, end := at(due, from), at(due, to)
			if !end.After(start) {
				if due.Before(end) {
					due = end
				} else if !due.Before(start) {
					due = end.AddDate(0, 0, 1)
				}
			} else if !due.Before(start) && due.Before(end) {
				due = end
			}
		}
	}
	return due.UTC()
}

type TokenView struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Scopes   []string  `json:"scopes"`
	Until    string    `json:"until,omitempty"`
	Issued   time.Time `json:"issued"`
	LastUsed time.Time `json:"lastUsed,omitzero"`
	Expired  bool      `json:"expired"`
}

type Session struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Token   string    `json:"token,omitempty"`
	Agent   string    `json:"agent,omitempty"`
	First   time.Time `json:"first"`
	Last    time.Time `json:"last"`
	Current bool      `json:"current"`
	Ended   time.Time `json:"ended,omitzero"`
}

type Seat struct {
	Subjects []string `json:"subjects"`
	Member
	Units []Membership `json:"units,omitempty"`
}

type LoginIdentity struct {
	Token  string            `json:"token"`
	Tenant string            `json:"tenant"`
	Member string            `json:"member"`
	Roles  map[string]string `json:"roles"`
}

type MemberView struct {
	Member
	Subjects []string `json:"subjects"`
	Profile  Profile  `json:"profile"`
}

type IdentitySessions interface {
	App
	MemberBySubject(string) (Member, bool)
	MemberByToken(string, time.Time) (Member, bool)
	NoticeSession(string, string, string, string, time.Time) bool
	SeenMember(string, time.Time)
	JoinsSubject(string) bool
	JoinableSubject(string) bool
	JoinID(string) string
	SecondFactorRequired() bool
	Minted(string, string, time.Time) (string, bool)
	EndOtherSessions(string, string, time.Time) int
	Sessions(string, string, time.Time) []Session
	Tokens(string, time.Time) []TokenView
}

type IdentityProfile interface {
	App
	Account(string) Account
	TenantRecord() TenantRecord
	Location(string) *time.Location
	Language(string) string
	Reach(string) Reach
}

type HostIdentities interface {
	App
	Identities() []LoginIdentity
}

type IdentityRecovery interface{ CloseTokenSecretWindows() }

type IdentityCounts interface{ MemberCount() int }

type ProjectAccess interface {
	ProjectRole(member, project string) string
	ProjectAccess(member Member, project string) ProjectAccessView
}

type AuthorizationState struct {
	Roles    map[string]*CustomRole
	Policies map[string]*PolicyRecord
	Teams    map[string]*Team
	Projects map[string]*BuildProject
	Markings map[string]*Marking `json:",omitempty"`
}

func AccessActions() []Action {
	admin := []string{"admin"}
	return []Action{
		{Schema: SchemaMarkingSave, Target: MarkingType, Capability: "members", Title: "Save marking", Description: "Save a marking and its member or team clearances.", Roles: admin, Payload: []Field{{Name: "title", Type: "string", Required: true}, {Name: "members", Type: "string[]"}, {Name: "teams", Type: "string[]"}, {Name: "declassifiers", Type: "string[]"}, {Name: "declassifierTeams", Type: "string[]"}}},
		{Schema: SchemaMarkingArchive, Target: MarkingType, Capability: "members", Title: "Archive marking", Description: "Stop granting this marking; existing marked data stays protected.", Roles: admin, Payload: []Field{}},
		{Schema: SchemaRoleSave, Target: RoleType, Capability: "members", Title: "Define role",
			Description: "Define or change a role of this tenant in an app: the actions it may call. Members hold it like any role.",
			Payload: []Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "title", Type: "string"},
				{Name: "actions", Type: "json", Required: true, Description: "Action schemas of the app"}}, Roles: admin},
		{Schema: SchemaRoleRemove, Target: RoleType, Capability: "members", Title: "Remove role",
			Description: "Remove a role this tenant defined; grants of it no longer allow anything.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaPolicySave, Target: PolicyType, Capability: "members", Title: "Set policy",
			Description: "Deny or allow a permission under conditions; a deny wins over every role.",
			Payload: []Field{{Name: "title", Type: "string"}, {Name: "effect", Type: "string", Required: true, Choices: []string{"deny", "allow"}},
				{Name: "permission", Type: "string", Required: true, Description: "An action schema, or a prefix ending in *"},
				{Name: "where", Type: "json", Description: "Conditions: member, app, agent, target"},
				{Name: "from", Type: "string", Description: "In force from this day (YYYY-MM-DD)"}, {Name: "until", Type: "string", Description: "Until this day, exclusive"}}, Roles: admin},
		{Schema: SchemaPolicyDrop, Target: PolicyType, Capability: "members", Title: "Remove policy", Description: "Remove a policy.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaTeamSave, Target: TeamType, Capability: "members", Title: "Save team",
			Description: "A team: members who hold the team's grants together, for as long as they belong to it.",
			Payload: []Field{{Name: "name", Type: "string", Required: true}, {Name: "members", Type: "json", Required: true, Description: "Member IDs"},
				{Name: "grants", Type: "json", Description: "Grants every member holds: app, role, unit, structure, until"}}, Roles: admin},
		{Schema: SchemaTeamRemove, Target: TeamType, Capability: "members", Title: "Remove team", Description: "Remove a team; its grants end.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaDelegate, Target: "platform.member", Capability: "members", Title: "Delegate my roles",
			Description: "Give another member the roles you hold in an app, until a day: for an absence or a handover. Only what you hold yourself.",
			Payload: []Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "until", Type: "string", Required: true, Description: "Until this day, exclusive (YYYY-MM-DD)"},
				{Name: "reason", Type: "string"}}, Roles: []string{AnyMember}},
	}
}

func ProjectActions() []Action {
	return []Action{
		{Schema: SchemaProjectSave, Target: ProjectType, Capability: "projects", Title: "Save project access",
			Description: "Set who may edit or open a project's resources. Administrators and builders set it first; afterwards the project's owners manage it.",
			Payload: []Field{
				{Name: "title", Type: "string", Required: true},
				{Name: "members", Type: "json", Description: "member IDs and project roles"},
				{Name: "approvals", Type: "integer", Description: "Required non-author approvals; defaults to one"},
			}, Roles: []string{AnyMember}},
	}
}

const (
	RoleType         = "platform.role"
	SchemaRoleSave   = "platform.role.save"
	SchemaRoleRemove = "platform.role.remove"
	PolicyType       = "platform.policy"
	SchemaPolicySave = "platform.policy.save"
	SchemaPolicyDrop = "platform.policy.remove"
	TeamType         = "platform.team"
	SchemaTeamSave   = "platform.team.save"
	SchemaTeamRemove = "platform.team.remove"
	SchemaDelegate   = "platform.member.delegate"
)

const Auditor = "auditor"

var Markings = []string{"", "internal", "confidential", "restricted"}

func MarkingRank(m string) int { return max(slices.Index(Markings, m), 0) }

func HigherMarking(a, b string) string {
	if MarkingRank(b) > MarkingRank(a) {
		return b
	}
	return a
}

func MarkingCleared(m Member, marking string) bool {
	if MarkingRank(marking) <= MarkingRank("internal") {
		return true
	}
	for _, clearance := range Markings[MarkingRank(marking):] {
		if m.Holds("platform", clearance) || slices.Contains(m.Markings, clearance) {
			return true
		}
	}
	return false
}

const (
	ProjectType           = "platform.project"
	SchemaProjectSave     = ProjectType + ".save"
	ProjectOwnerRole      = "owner"
	ProjectEditorRole     = "editor"
	ProjectViewerRole     = "viewer"
	ProjectDiscovererRole = "discoverer"
)

var auditorReads = []string{"audit", "deliveries", "work", "connectors", "settings",
	"endpoints", "effects", "health", "personal-reads", "permissions", "access", "members", "packages", "notifications"}

func AuditorMayRead(name string) bool {
	for _, r := range auditorReads {
		if r == name {
			return true
		}
	}
	return false
}

type ConsolePolicies interface {
	Attach(AppCatalog)
	SnapshotState() AuthorizationState
	MarkingViews() []Marking
	MarkingClearances(string) ([]string, []string)
	Restore(AuthorizationState)
	PrepareAccess(Caller, *pb.Submission, []string) (func(*pb.ChangeRecord), *kernel.Error)
	PrepareProject(Caller, *pb.Submission, []string) (func(*pb.ChangeRecord), *kernel.Error)
	TeamGrants(string) []Grant
	Access() AccessConfig
	ProjectViews() []BuildProject
	ProjectRole(member, project string) string
	ProjectGrants(string, bool) []Grant
	ApplyAccess()
}
