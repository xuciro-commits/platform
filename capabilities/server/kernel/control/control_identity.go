package control

import (
	"encoding/json"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (d *Console) currentMember(m *platform.Member) platform.Member {
	out := clone(m)
	if p := d.profiles[m.ID]; p != nil {
		out.Language, out.Timezone = p.Language, p.Timezone
	}
	if out.Timezone == "" {
		out.Timezone = d.tenantDefault(SettingTimezone)
	}
	today := out.Today(time.Now())
	if out.Status == platform.MemberInvited && !d.lastSeen[out.ID].IsZero() {
		out.Status = ""
	}
	if !out.Active() {
		out.Grants, out.Roles = nil, map[string]string{}
		return out
	}
	d.migrateGrants(&out)
	out.Grants = append(slices.Clone(out.Grants), d.ConsolePolicies.TeamGrants(out.ID)...)
	out.Grants = append(out.Grants, d.ConsolePolicies.ProjectGrants(out.ID, out.Agent)...)
	d.deriveRoles(&out, today)
	out.Grants = slices.DeleteFunc(slices.Clone(out.Grants), func(g platform.Grant) bool { return !g.Active(today) })
	return out
}

func (d *Console) migrateGrants(m *platform.Member) {
	if len(m.Grants) > 0 {
		return
	}
	for _, app := range slices.Sorted(maps.Keys(m.Roles)) {
		m.Grants = append(m.Grants, platform.Grant{App: app, Role: m.Roles[app]})
	}
}

func (d *Console) deriveRoles(m *platform.Member, today string) {
	active := slices.DeleteFunc(slices.Clone(m.Grants), func(g platform.Grant) bool { return !g.Active(today) })
	previous := m.Roles
	m.Roles = map[string]string{}
	for _, g := range active {
		if m.Roles[g.App] == "" {
			if role := previous[g.App]; slices.ContainsFunc(active, func(held platform.Grant) bool { return held.App == g.App && held.Role == role }) {
				m.Roles[g.App] = role
			} else {
				m.Roles[g.App] = d.primaryRole(g.App, active)
			}
		}
	}
}

func (d *Console) primaryRole(app string, grants []platform.Grant) string {
	held := func(role string) bool {
		return slices.ContainsFunc(grants, func(g platform.Grant) bool { return g.App == app && g.Role == role })
	}
	if d.host != nil && d.host.App(app) != nil {
		for _, role := range d.host.Manifest(app).AllRoles() {
			if held(role) {
				return role
			}
		}
	}
	for _, g := range grants {
		if g.App == app {
			return g.Role
		}
	}
	return ""
}

func (d *Console) MemberByID(id string) (platform.Member, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	member := d.members[id]
	if member == nil {
		return platform.Member{}, false
	}
	return d.currentMember(member), true
}

func (d *Console) AgentMembers() []platform.Member {
	d.mu.Lock()
	defer d.mu.Unlock()
	var members []platform.Member
	for _, member := range d.members {
		if member.Agent && member.Status != platform.MemberLeft {
			members = append(members, d.currentMember(member))
		}
	}
	slices.SortFunc(members, func(a, b platform.Member) int { return strings.Compare(a.ID, b.ID) })
	return members
}

func (d *Console) Ready() error {
	d.ConsolePolicies.ApplyAccess()
	return nil
}

var _ platform.IdentityDirectory = (*Console)(nil)

var _ platform.CompositionReady = (*Console)(nil)

func (d *Console) AttachConsole(h platform.ConsoleHost) {
	d.host = h
	d.ConsolePackages.Attach(h)
	d.ConsolePolicies.Attach(h)
}

func (d *Console) MemberCount() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.members) }

func (d *Console) NewSimulationIdentity(tenant string, members []platform.Member) platform.App {
	members = append(slices.Clone(members), platform.Member{ID: "app:candidate-test", Tenant: tenant, Roles: map[string]string{PlatformApp: Admin, platform.AIApp: platform.AIAdmin}})
	seats := make([]platform.Seat, 0, len(members))
	for _, member := range members {
		member.Tenant = tenant
		member.Roles = maps.Clone(member.Roles)
		seats = append(seats, platform.Seat{Subjects: []string{member.ID}, Member: member})
	}
	return NewConsole(tenant, d.newOwners, seats...)
}

func (d *Console) UnitGrantUsage(unit, until string) []platform.UnitGrantUsage {
	uses := map[string]bool{}
	d.mu.Lock()
	for _, member := range d.members {
		grants := slices.Clone(member.Grants)
		if member.Active() {
			grants = append(grants, d.currentMember(member).Grants...)
		}
		for _, grant := range grants {
			if grant.Unit == unit {
				uses[member.ID] = uses[member.ID] || member.Active() && (grant.Until == "" || grant.Until > until)
			}
		}
	}
	for _, team := range d.ConsolePolicies.Access().Teams {
		for _, grant := range team.Grants {
			if grant.Unit == unit {
				uses["team:"+team.ID] = uses["team:"+team.ID] || grant.Until == "" || grant.Until > until
			}
		}
	}
	d.mu.Unlock()
	for _, record := range d.ledger.RecordsFor(d.tenant) {
		submission := record.GetSubmission()
		if submission.GetSchema().GetName() == platform.SchemaTeamSave {
			var team platform.Team
			if json.Unmarshal(submission.GetPayload(), &team) == nil && slices.ContainsFunc(team.Grants, func(grant platform.Grant) bool { return grant.Unit == unit }) {
				id := "team:" + submission.GetTarget().GetId()
				if _, exists := uses[id]; !exists {
					uses[id] = false
				}
			}
		}
		if submission.GetSchema().GetName() != SchemaGrant {
			continue
		}
		var grant platform.Grant
		if json.Unmarshal(submission.GetPayload(), &grant) == nil && grant.Unit == unit {
			member := submission.GetTarget().GetId()
			if _, exists := uses[member]; !exists {
				uses[member] = false
			}
		}
	}
	result := make([]platform.UnitGrantUsage, 0, len(uses))
	for member, current := range uses {
		result = append(result, platform.UnitGrantUsage{Member: member, Current: current})
	}
	slices.SortFunc(result, func(a, b platform.UnitGrantUsage) int { return strings.Compare(a.Member, b.Member) })
	return result
}

const (
	ProfileType         = "platform.profile"
	SchemaProfileUpdate = "platform.profile.update"
)

func profileActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaProfileUpdate, Target: ProfileType, Capability: "account", Title: "Update profile",
			Description: "Change how the platform addresses you and behaves for you: name, contact, language, timezone, formats, notifications and where the workspace opens. Only the fields sent change; an empty value returns to the tenant's default. Administrators change anyone's.",
			Payload: []platform.Field{
				{Name: "displayName", Type: "string"}, {Name: "givenName", Type: "string"}, {Name: "familyName", Type: "string"},
				{Name: "title", Type: "string", Description: "Job title"}, {Name: "pronouns", Type: "string"},
				{Name: "email", Type: "string", Description: "Where notifications are mailed"}, {Name: "phone", Type: "string"},
				{Name: "language", Type: "string", Description: "A language the tenant speaks, such as zh-CN"},
				{Name: "timezone", Type: "string", Description: "IANA timezone, such as Asia/Shanghai"},
				{Name: "dateFormat", Type: "string", Description: "ymd, dmy or mdy"}, {Name: "numberFormat", Type: "string", Description: "1,234.56 or 1.234,56 or 1 234,56"},
				{Name: "weekStart", Type: "string", Description: "monday, sunday or saturday"},
				{Name: "inApp", Type: "boolean", Description: "Notify in the workspace"}, {Name: "mail", Type: "boolean", Description: "Notify by mail"},
				{Name: "digest", Type: "string", Description: "instant, hourly or daily"},
				{Name: "quietFrom", Type: "string", Description: "HH:MM: no mail from"}, {Name: "quietTo", Type: "string", Description: "HH:MM: until"},
				{Name: "homePage", Type: "string", Description: "The app the workspace opens on"},
				{Name: "theme", Type: "string", Description: "system, light or dark"}, {Name: "density", Type: "string", Description: "comfortable or compact"},
			}, Roles: []string{platform.AnyMember}},
	}
}

const (
	SettingName         = "name"
	SettingLegalName    = "legalName"
	SettingCountry      = "country"
	SettingTimezone     = "timezone"
	SettingDateFormat   = "dateFormat"
	SettingNumberFormat = "numberFormat"
	SettingWeekStart    = "weekStart"
	SettingFiscalStart  = "fiscalYearStart"
	SettingDomains      = "signInDomains"
	SettingMFA          = "mfaRequired"
	SettingSessionHours = "sessionHours"
	SettingDigest       = "digest"
)

func tenantSettings() []platform.Setting {
	return []platform.Setting{
		{Name: SettingName, Title: "Organisation name", Type: "text", Description: "How the tenant is shown to its members and in documents it sends."},
		{Name: "enterpriseRoot", Title: "Representative organization", Type: "text", Description: "The enterprise organization representing this tenant; no name or role is copied."},
		{Name: SettingLegalName, Title: "Legal name", Type: "text", Description: "The registered name, for documents that need it."},
		{Name: SettingCountry, Title: "Country", Type: "text", Description: "ISO 3166-1 alpha-2, such as CN or DE."},
		{Name: SettingLanguage, Title: "Default language", Type: "text", Default: "",
			Description: "The language members read until they choose their own, such as zh-CN; empty: what each browser asks for, else English."},
		{Name: SettingTimezone, Title: "Timezone", Type: "text", Default: "UTC", Description: "IANA timezone the tenant keeps its days in, such as Asia/Shanghai; members may choose their own."},
		{Name: SettingCurrency, Title: "Currency", Type: "text", Default: "EUR",
			Description: "The tenant's currency (ISO 4217): the default of every amount people enter, and the currency the books are kept in."},
		{Name: SettingDateFormat, Title: "Date format", Type: "choice", Default: "ymd", Choices: []string{"ymd", "dmy", "mdy"}, Description: "How dates are shown: 2026-10-06, 06.10.2026 or 10/06/2026."},
		{Name: SettingNumberFormat, Title: "Number format", Type: "choice", Default: "1,234.56", Choices: []string{"1,234.56", "1.234,56", "1 234,56"}},
		{Name: SettingWeekStart, Title: "Week starts on", Type: "choice", Default: "monday", Choices: []string{"monday", "sunday", "saturday"}},
		{Name: SettingFiscalStart, Title: "Fiscal year starts", Type: "text", Default: "01-01", Description: "MM-DD of the first day of the fiscal year."},
		{Name: SettingDomains, Title: "Sign-in domains", Type: "text", Description: "Comma-separated mail domains whose people become members on first sign-in, holding nothing until granted; empty: invitation only."},
		{Name: SettingMFA, Title: "Require a second factor", Type: "boolean", Default: "false", Description: "Every sign-in must carry the identity provider's word that a second factor was used (amr); a host whose provider does not say admits none."},
		{Name: SettingSessionHours, Title: "Session length (hours)", Type: "integer", Default: "12", Description: "How long a sign-in lasts on this host before the identity provider is asked again; 0: as long as the credential."},
		{Name: SettingDigest, Title: "Mail digest", Type: "choice", Default: "instant", Choices: []string{"instant", "hourly", "daily"}, Description: "How notifications are mailed until a member chooses."},
	}
}

func (d *Console) TenantRecord() platform.TenantRecord {
	h := d.host
	out := platform.TenantRecord{ID: d.tenant, Settings: map[string]string{}, Apps: []string{}, Status: "active"}
	if h == nil {
		return out
	}
	for _, s := range tenantSettings() {
		out.Settings[s.Name] = h.ConsoleSetting(s.Name)
	}
	out.Name = out.Settings[SettingName]
	if out.Name == "" {
		out.Name = d.tenant
	}
	out.Apps = append(out.Apps, h.AppIDs()...)
	d.mu.Lock()
	out.Members = len(d.members)
	d.mu.Unlock()
	if h.TenantSuspended() {
		out.Status = "suspended"
	}
	return out
}

func (d *Console) tenantDefault(name string) string {
	if d.host == nil {
		return ""
	}
	return d.host.ConsoleSetting(name)
}

type defaults map[string]string

func (d *Console) defaults() defaults {
	out := defaults{}
	for _, name := range []string{SettingLanguage, SettingTimezone, SettingDateFormat, SettingNumberFormat, SettingWeekStart, SettingDigest} {
		out[name] = d.tenantDefault(name)
	}
	return out
}

func (d *Console) account(member string, def defaults) platform.Account {
	out := platform.Account{Profile: platform.Profile{Member: member}}
	if m := d.members[member]; m != nil {
		out.Language = m.Language
	}
	if p := d.profiles[member]; p != nil {
		out.Profile = *p
	}
	out.LastSeen = d.lastSeen[member]
	or := func(own, setting string) string {
		if own != "" {
			return own
		}
		return def[setting]
	}
	out.Effective.Language = or(out.Language, SettingLanguage)
	out.Effective.Timezone = or(out.Timezone, SettingTimezone)
	out.Effective.DateFormat = or(out.DateFormat, SettingDateFormat)
	out.Effective.NumberFormat = or(out.NumberFormat, SettingNumberFormat)
	out.Effective.WeekStart = or(out.WeekStart, SettingWeekStart)
	out.Effective.Digest = or(out.Digest, SettingDigest)
	for subject, id := range d.subjects {
		if id == member {
			out.Subjects = append(out.Subjects, subject)
		}
	}
	slices.Sort(out.Subjects)
	if out.Subjects == nil {
		out.Subjects = []string{}
	}
	out.Effective.Email = out.Email
	if out.Effective.Email == "" {
		out.Effective.Email = d.addressLocked(member)
	}
	return out
}

func (d *Console) Account(member string) platform.Account {
	def := d.defaults()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.account(member, def)
}

func (d *Console) Location(member string) *time.Location {
	a := d.Account(member)
	if loc, err := time.LoadLocation(a.Effective.Timezone); err == nil && a.Effective.Timezone != "" {
		return loc
	}
	return time.UTC
}

func (d *Console) decideProfile(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if d.members[id] == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if c.ID != id && !c.Holds(PlatformApp, Admin) && !c.Replaying {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(s.GetPayload(), &fields) != nil {
		return nil, invalid
	}
	var p platform.Profile
	if json.Unmarshal(s.GetPayload(), &p) != nil {
		return nil, invalid
	}
	if !c.Replaying {
		oneOf := func(v string, choices ...string) bool { return v == "" || slices.Contains(choices, v) }
		switch {
		case p.Language != "" && p.Language != "en" && (d.host == nil || !slices.Contains(d.host.Languages(), p.Language)):
			return nil, invalid
		case p.Timezone != "" && !validZone(p.Timezone):
			return nil, invalid
		case !oneOf(p.DateFormat, "ymd", "dmy", "mdy"), !oneOf(p.NumberFormat, "1,234.56", "1.234,56", "1 234,56"),
			!oneOf(p.WeekStart, "monday", "sunday", "saturday"), !oneOf(p.Digest, "instant", "hourly", "daily"),
			!oneOf(p.Theme, "system", "light", "dark"), !oneOf(p.Density, "comfortable", "compact"):
			return nil, invalid
		case !validClock(p.QuietFrom) || !validClock(p.QuietTo):
			return nil, invalid
		case p.Email != "" && !strings.Contains(p.Email, "@"):
			return nil, invalid
		}
	}
	return func(*pb.ChangeRecord) {
		cur := d.profiles[id]
		if cur == nil {
			cur = &platform.Profile{Member: id, Language: d.members[id].Language}
			d.profiles[id] = cur
		}
		set := func(name string, dst *string, v string) {
			if _, ok := fields[name]; ok {
				*dst = v
			}
		}
		set("displayName", &cur.DisplayName, p.DisplayName)
		set("givenName", &cur.GivenName, p.GivenName)
		set("familyName", &cur.FamilyName, p.FamilyName)
		set("title", &cur.Title, p.Title)
		set("pronouns", &cur.Pronouns, p.Pronouns)
		set("email", &cur.Email, p.Email)
		set("phone", &cur.Phone, p.Phone)
		set("language", &cur.Language, p.Language)
		set("timezone", &cur.Timezone, p.Timezone)
		set("dateFormat", &cur.DateFormat, p.DateFormat)
		set("numberFormat", &cur.NumberFormat, p.NumberFormat)
		set("weekStart", &cur.WeekStart, p.WeekStart)
		set("digest", &cur.Digest, p.Digest)
		set("quietFrom", &cur.QuietFrom, p.QuietFrom)
		set("quietTo", &cur.QuietTo, p.QuietTo)
		set("homePage", &cur.HomePage, p.HomePage)
		set("theme", &cur.Theme, p.Theme)
		set("density", &cur.Density, p.Density)
		if _, ok := fields["inApp"]; ok {
			cur.InApp = p.InApp
		}
		if _, ok := fields["mail"]; ok {
			cur.Mail = p.Mail
		}
	}, nil
}

func validZone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

func validClock(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse("15:04", v)
	return err == nil
}

func (d *Console) SeenMember(member string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.members[member] == nil {
		return
	}
	if now.Sub(d.lastSeen[member]) >= time.Minute {
		d.lastSeen[member] = now
	}
}

func (d *Console) Reach(member string) platform.Reach {
	a := d.Account(member)
	out := platform.Reach{InApp: true, Mail: true, Address: a.Effective.Email, Language: d.Language(member), Digest: a.Effective.Digest,
		QuietFrom: a.QuietFrom, QuietTo: a.QuietTo, Location: d.Location(member)}
	if a.InApp != nil {
		out.InApp = *a.InApp
	}
	if a.Mail != nil {
		out.Mail = *a.Mail
	}
	return out
}

func (d *Console) decideDelegate(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct{ App, Until, Reason string }
	to := d.members[s.GetTarget().GetId()]
	if json.Unmarshal(s.GetPayload(), &p) != nil || to == nil || to.ID == c.ID || d.host == nil || d.host.App(p.App) == nil {
		return nil, invalid
	}
	if _, err := time.Parse(time.DateOnly, p.Until); err != nil {
		return nil, invalid
	}
	roles := c.RolesIn(p.App)
	if len(roles) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} holds no role in {app}, so may not {action}", c.ID, p.App, "delegate")
	}
	return func(r *pb.ChangeRecord) {
		d.migrateGrants(to)
		for _, role := range roles {
			to.Grants = slices.DeleteFunc(to.Grants, func(g platform.Grant) bool { return g.App == p.App && g.Role == role && g.By == c.ID })
			to.Grants = append(to.Grants, platform.Grant{App: p.App, Role: role, Until: p.Until, By: c.ID, Reason: strings.TrimSpace("delegated " + p.Reason), At: r.GetRecordedTime().AsTime()})
		}
		d.deriveRoles(to, d.currentMember(to).Today(time.Now()))
	}, nil
}

var _ platform.IdentitySessions = (*Console)(nil)

var _ platform.IdentityProfile = (*Console)(nil)

var _ platform.HostIdentities = (*Console)(nil)

func (d *Console) ProjectAccess(m platform.Member, project string) platform.ProjectAccessView {
	out := platform.ProjectAccessView{Members: []platform.ProjectMember{}, Manage: platform.ProjectManager(m, d.ConsolePolicies.ProjectRole(m.ID, project))}
	for _, p := range d.ConsolePolicies.ProjectViews() {
		if p.ID == project && !p.Archived {
			out.Members = slices.Clone(p.Members)
		}
	}
	listed := map[string]bool{}
	for _, e := range out.Members {
		listed[e.Member] = true
	}
	def := d.defaults()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, member := range d.members {
		if !listed[member.ID] && (!out.Manage || member.Agent || !member.Active()) {
			continue
		}
		profile := d.account(member.ID, def).Profile
		name := profile.DisplayName
		if name == "" {
			name = profile.Email
		}
		if name == "" {
			name = member.ID
		}
		out.People = append(out.People, platform.ProjectPerson{ID: member.ID, Name: name})
	}
	slices.SortFunc(out.People, func(a, b platform.ProjectPerson) int { return strings.Compare(a.Name, b.Name) })
	return out
}
