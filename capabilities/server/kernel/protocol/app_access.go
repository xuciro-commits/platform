package protocol

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"strings"
	"time"
)

type ProjectAsset struct {
	App  string `json:"app,omitempty" title:"App" help:"The app the asset belongs to; empty: any"`
	Kind string `json:"kind" field:"required,choice:object,page,query,function,process,code,propertytype,linktype" title:"Kind"`
	Name string `json:"name" field:"required" title:"Name" help:"The asset's name inside its owner app"`
}

type ProjectPerson struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func ProjectManager(m platform.Member, role string) bool {
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
	Approvals int                      `json:"approvals"`
	Members   []platform.ProjectMember `json:"members"`
	Manage    bool                     `json:"manage"`
	People    []ProjectPerson          `json:"people,omitempty"`
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
	platform.Member
	Units []platform.Membership `json:"units,omitempty"`
}

type LoginIdentity struct {
	Token  string            `json:"token"`
	Tenant string            `json:"tenant"`
	Member string            `json:"member"`
	Roles  map[string]string `json:"roles"`
}

type MemberView struct {
	platform.Member
	Subjects []string `json:"subjects"`
	Profile  Profile  `json:"profile"`
}

type IdentitySessions interface {
	platform.Service
	MemberBySubject(string) (platform.Member, bool)
	MemberByToken(string, time.Time) (platform.Member, bool)
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
	platform.Service
	Account(string) Account
	MemberDisplayName(string) string
	TenantRecord() TenantRecord
	Location(string) *time.Location
	Language(string) string
	Reach(string) Reach
}

type MemberOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type HostIdentities interface {
	platform.Service
	Identities() []LoginIdentity
}

type IdentityRecovery interface{ CloseTokenSecretWindows() }

type IdentityCounts interface{ MemberCount() int }

type ProjectAccess interface {
	ProjectRole(member, project string) string
	ProjectAccess(member platform.Member, project string) ProjectAccessView
}

func AccessActions() []platform.Action {
	admin := []string{"admin"}
	return []platform.Action{
		{Schema: platform.SchemaMarkingSave, Target: platform.MarkingType, Capability: "members", Title: "Save marking", Description: "Save a marking and its member or team clearances.", Roles: admin, Payload: []platform.Field{{Name: "title", Type: "string", Required: true}, {Name: "members", Type: "string[]"}, {Name: "teams", Type: "string[]"}, {Name: "declassifiers", Type: "string[]"}, {Name: "declassifierTeams", Type: "string[]"}}},
		{Schema: platform.SchemaMarkingArchive, Target: platform.MarkingType, Capability: "members", Title: "Archive marking", Description: "Stop granting this marking; existing marked data stays protected.", Roles: admin, Payload: []platform.Field{}},
		{Schema: SchemaRoleSave, Target: RoleType, Capability: "members", Title: "Define role",
			Description: "Define or change a role of this tenant in an app: the actions it may call. Members hold it like any role.",
			Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "title", Type: "string"},
				{Name: "actions", Type: "json", Required: true, Description: "Action schemas of the app"}}, Roles: admin},
		{Schema: SchemaRoleRemove, Target: RoleType, Capability: "members", Title: "Remove role",
			Description: "Remove a role this tenant defined; grants of it no longer allow anything.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaPolicySave, Target: PolicyType, Capability: "members", Title: "Set policy",
			Description: "Deny or allow a permission under conditions; a deny wins over every role.",
			Payload: []platform.Field{{Name: "title", Type: "string"}, {Name: "effect", Type: "string", Required: true, Choices: []string{"deny", "allow"}},
				{Name: "permission", Type: "string", Required: true, Description: "An action schema, or a prefix ending in *"},
				{Name: "where", Type: "json", Description: "Conditions: member, app, agent, target"},
				{Name: "from", Type: "string", Description: "In force from this day (YYYY-MM-DD)"}, {Name: "until", Type: "string", Description: "Until this day, exclusive"}}, Roles: admin},
		{Schema: SchemaPolicyDrop, Target: PolicyType, Capability: "members", Title: "Remove policy", Description: "Remove a policy.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaTeamSave, Target: TeamType, Capability: "members", Title: "Save team",
			Description: "A team: members who hold the team's grants together, for as long as they belong to it.",
			Payload: []platform.Field{{Name: "name", Type: "string", Required: true}, {Name: "members", Type: "json", Required: true, Description: "Member IDs"},
				{Name: "grants", Type: "json", Description: "Grants every member holds: app, role, unit, structure, until"}}, Roles: admin},
		{Schema: SchemaTeamRemove, Target: TeamType, Capability: "members", Title: "Remove team", Description: "Remove a team; its grants end.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaDelegate, Target: "platform.member", Capability: "members", Title: "Delegate my roles",
			Description: "Give another member the roles you hold in an app, until a day: for an absence or a handover. Only what you hold yourself.",
			Payload: []platform.Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}, {Name: "until", Type: "string", Required: true, Description: "Until this day, exclusive (YYYY-MM-DD)"},
				{Name: "reason", Type: "string"}}, Roles: []string{platform.AnyMember}},
	}
}

func ProjectActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaProjectSave, Target: ProjectType, Capability: "projects", Title: "Save project access",
			Description: "Set who may edit or open a project's resources. Administrators and builders set it first; afterwards the project's owners manage it.",
			Payload: []platform.Field{
				{Name: "title", Type: "string", Required: true},
				{Name: "members", Type: "json", Description: "member IDs and project roles"},
				{Name: "approvals", Type: "integer", Description: "Required non-author approvals; defaults to one"},
			}, Roles: []string{platform.AnyMember}},
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

func HigherMarking(a, b string) string {
	if platform.MarkingRank(b) > platform.MarkingRank(a) {
		return b
	}
	return a
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
	"endpoints", "effects", "health", "personal-reads", "permissions", "access", "access-requests", "members", "packages", "notifications"}

func AuditorMayRead(name string) bool {
	for _, r := range auditorReads {
		if r == name {
			return true
		}
	}
	return false
}

type ConsolePolicies interface {
	Attach(platform.AppCatalog)
	SnapshotState() platform.AuthorizationState
	MarkingViews() []platform.Marking
	MarkingClearances(string) ([]string, []string)
	Restore(platform.AuthorizationState)
	PrepareAccess(platform.Caller, *pb.Submission, []string) (func(*pb.ChangeRecord), *platform.Error)
	PrepareProject(platform.Caller, *pb.Submission, []string) (func(*pb.ChangeRecord), *platform.Error)
	TeamGrants(string) []platform.Grant
	Access() platform.AccessConfig
	ProjectViews() []platform.BuildProject
	ProjectRole(member, project string) string
	ProjectGrants(string, bool) []platform.Grant
	ApplyAccess()
}
