package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// A member's profile (ADR-0079 §2) is how the platform addresses them and
// how they want it to behave: name and contact, language, timezone and
// formats, notification preferences, and where the workspace opens. Members
// change their own; administrators change anyone's. What is empty inherits
// the tenant's defaults (platform.tenant settings), so a profile only holds
// what the member chose.
const (
	ProfileType         = "platform.profile"
	SchemaProfileUpdate = "platform.profile.update"
)

type Profile struct {
	Member      string `json:"member"`
	DisplayName string `json:"displayName,omitempty"`
	GivenName   string `json:"givenName,omitempty"`
	FamilyName  string `json:"familyName,omitempty"`
	Title       string `json:"title,omitempty"` // job title
	Pronouns    string `json:"pronouns,omitempty"`
	Email       string `json:"email,omitempty"` // where notifications are mailed; empty: the user: subject's address
	Phone       string `json:"phone,omitempty"`
	// Localisation; empty inherits the tenant's.
	Language     string `json:"language,omitempty"`     // such as zh-CN
	Timezone     string `json:"timezone,omitempty"`     // IANA, such as Asia/Shanghai
	DateFormat   string `json:"dateFormat,omitempty"`   // ymd | dmy | mdy
	NumberFormat string `json:"numberFormat,omitempty"` // "1,234.56" | "1.234,56" | "1 234,56"
	WeekStart    string `json:"weekStart,omitempty"`    // monday | sunday | saturday
	// Notifications.
	InApp     *bool  `json:"inApp,omitempty"`
	Mail      *bool  `json:"mail,omitempty"`
	Digest    string `json:"digest,omitempty"`    // instant | hourly | daily
	QuietFrom string `json:"quietFrom,omitempty"` // HH:MM in the member's timezone
	QuietTo   string `json:"quietTo,omitempty"`
	// Workspace.
	HomePage string `json:"homePage,omitempty"` // an app ID or app/view the workspace opens on
	Theme    string `json:"theme,omitempty"`    // system | light | dark
	Density  string `json:"density,omitempty"`  // comfortable | compact
	// Read-only, maintained by the host.
	LastSeen time.Time `json:"lastSeen,omitempty"`
}

// Account is the effective profile a member works under: their own choices
// over the tenant's defaults, with the defaults named.
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
}

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

// Tenant-wide defaults a profile inherits (ADR-0078 §2.1), as settings of the
// platform app so they are decided, journaled and read like any setting.
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
		{Name: SettingDomains, Title: "Sign-in domains", Type: "text", Description: "Comma-separated mail domains whose users may join as members without an invitation; empty: invitation only."},
		{Name: SettingMFA, Title: "Require a second factor", Type: "boolean", Default: "false", Description: "Asked of the identity provider for every sign-in."},
		{Name: SettingSessionHours, Title: "Session length (hours)", Type: "integer", Default: "12", Description: "How long a sign-in lasts before the identity provider is asked again."},
		{Name: SettingDigest, Title: "Mail digest", Type: "choice", Default: "instant", Choices: []string{"instant", "hourly", "daily"}, Description: "How notifications are mailed until a member chooses."},
	}
}

// TenantRecord is the tenant as its members see it (read "tenant", everyone):
// its settings, the apps it runs, and its standing on the host.
type TenantRecord struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Settings map[string]string `json:"settings"`
	Apps     []string          `json:"apps"`
	Members  int               `json:"members"`
	Status   string            `json:"status"` // active | suspended | frozen (host lifecycle)
	Since    time.Time         `json:"since,omitempty"`
}

func (d *Console) tenantRecord() TenantRecord {
	t := d.t
	out := TenantRecord{ID: d.tenant, Settings: map[string]string{}, Apps: []string{}, Status: "active"}
	if t == nil {
		return out
	}
	auto := t.automation(PlatformApp, false)
	for _, s := range tenantSettings() {
		out.Settings[s.Name] = t.setting(auto, s.Name)
	}
	out.Name = out.Settings[SettingName]
	if out.Name == "" {
		out.Name = d.tenant
	}
	for _, a := range t.apps {
		out.Apps = append(out.Apps, a.Manifest().ID)
	}
	d.mu.Lock()
	out.Members = len(d.members)
	d.mu.Unlock()
	if t.hostSuspended() {
		out.Status = "suspended"
	}
	return out
}

// tenantDefault is a tenant-wide setting, "" without a composed tenant.
func (d *Console) tenantDefault(name string) string {
	if d.t == nil {
		return ""
	}
	return d.t.setting(d.t.automation(PlatformApp, false), name)
}

// defaults are the tenant-wide settings a profile inherits. Read without d.mu.
type defaults map[string]string

func (d *Console) defaults() defaults {
	out := defaults{}
	for _, name := range []string{SettingLanguage, SettingTimezone, SettingDateFormat, SettingNumberFormat, SettingWeekStart, SettingDigest} {
		out[name] = d.tenantDefault(name)
	}
	return out
}

// account is member's effective profile over the tenant's defaults. Call with d.mu held.
func (d *Console) account(member string, def defaults) Account {
	out := Account{Profile: Profile{Member: member}}
	if p := d.profiles[member]; p != nil {
		out.Profile = *p
	}
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
	out.Effective.Email = out.Email
	if out.Effective.Email == "" {
		out.Effective.Email = d.addressLocked(member)
	}
	return out
}

// Account is member's effective profile.
func (d *Console) Account(member string) Account {
	def := d.defaults()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.account(member, def)
}

// Location is the timezone member works in (ADR-0079 §3): their own, else the
// tenant's, else UTC.
func (d *Console) Location(member string) *time.Location {
	a := d.Account(member)
	if loc, err := time.LoadLocation(a.Effective.Timezone); err == nil && a.Effective.Timezone != "" {
		return loc
	}
	return time.UTC
}

// decideProfile applies a partial update: only the fields the payload names
// change; an empty value clears the member's choice. Call with d.mu held.
func (d *Console) decideProfile(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if d.members[id] == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if c.ID != id && c.Role() != Admin && !c.Replaying {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(s.GetPayload(), &fields) != nil {
		return nil, invalid
	}
	var p Profile
	if json.Unmarshal(s.GetPayload(), &p) != nil {
		return nil, invalid
	}
	if !c.Replaying {
		oneOf := func(v string, choices ...string) bool { return v == "" || slices.Contains(choices, v) }
		switch {
		case p.Language != "" && p.Language != "en" && (d.t == nil || !slices.Contains(d.t.languages(), p.Language)):
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
			cur = &Profile{Member: id}
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

// seen notes a member's activity, at most once a minute (not journaled: a
// restart loses the last minute, nothing more).
func (d *Console) seen(member string, now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.members[member] == nil {
		return
	}
	cur := d.profiles[member]
	if cur == nil {
		cur = &Profile{Member: member}
		d.profiles[member] = cur
	}
	if now.Sub(cur.LastSeen) >= time.Minute {
		cur.LastSeen = now
	}
}
