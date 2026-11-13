package control

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"platformkernel/kernel"
	"platformserver/platform"
	"regexp"
	"slices"
	"strings"
	"time"
)

type HostArtifactView struct {
	Candidate string `json:"candidate"`
	Active    bool   `json:"active,omitempty"`
	Digest    string `json:"digest,omitempty"`
	Size      int    `json:"size"`
	Assets    int    `json:"assets"`
	Verified  bool   `json:"verified"`
	Note      string `json:"note,omitempty"`
}

func HostArtifacts(ids []string, active string, raw func(string) []byte) []HostArtifactView {
	out := []HostArtifactView{}
	for _, id := range ids {
		body := raw(id)
		view := HostArtifactView{Candidate: id, Active: id == active, Size: len(body)}
		if candidate, err := platform.ReadCandidate(id, body); err == nil {
			view.Digest, view.Assets, view.Verified = candidate.ID, len(candidate.Assets), true
		} else {
			view.Note = err.Error()
		}
		out = append(out, view)
	}
	slices.SortStableFunc(out, func(a, b HostArtifactView) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Candidate, b.Candidate)
	})
	return out
}

type SupportError string

func (e SupportError) Error() string { return string(e) }

var ErrUnknownGrant = SupportError("unknown support grant")

func ApplyLifecycle(image *platform.HostControlImage, action string, quarantined bool, resumeBlocked func() bool) error {
	if quarantined && action != "open" && action != "resume" {
		return SupportError("a quarantined tenant is already stopped; repair it through recovery")
	}
	image.Lifecycle = ""
	if (action == "resume" || action == "open") && resumeBlocked() {
		return SupportError("Activate the saved compatible implementation before resuming business access")
	}
	if action == "suspend" || action == "decommission" {
		image.Lifecycle = action
	}
	return nil
}

func ValidLifecycleAction(action string) error {
	if !slices.Contains([]string{"suspend", "decommission", "resume", "open"}, action) {
		return SupportError("lifecycle action must be suspend, resume, decommission or open")
	}
	return nil
}

func ValidSupportSession(member, reason, subject string, minutes int, now time.Time) error {
	if member == "" || reason == "" || subject == "" || now.IsZero() || minutes < 1 || minutes > 24*60 {
		return SupportError("a support session needs a member, a reason and 1 to 1440 minutes")
	}
	return nil
}

func OpenSupportGrant(image *platform.HostControlImage, id, tenant, member, reason, subject string, minutes int, now time.Time) platform.SupportGrant {
	grant := platform.SupportGrant{ID: id, Tenant: tenant, Member: member, Reason: reason, Opened: subject, Expires: now.UTC().Add(time.Duration(minutes) * time.Minute)}
	image.Support = append(image.Support, grant)
	return grant
}

func UseSupportGrant(image *platform.HostControlImage, id string, now time.Time) (platform.SupportGrant, error) {
	i := slices.IndexFunc(image.Support, func(g platform.SupportGrant) bool { return g.ID == id })
	if i < 0 {
		return platform.SupportGrant{}, ErrUnknownGrant
	}
	if !image.Support[i].Expires.After(now) {
		return platform.SupportGrant{}, SupportError("the support session has ended")
	}
	image.Support[i].Uses++
	return image.Support[i], nil
}

func TailAudit(entries []platform.AuditEntry, n int) []platform.AuditEntry {
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return slices.Clone(entries)
}

type MigrationHooks struct {
	Hosted func() map[string]bool
	Export func(typ string) ([]byte, *kernel.Error)
	Import func(typ string, data []byte) ([]platform.ImportRow, *kernel.Error)
}

func MigrateRecords(from, to string, types []string, h MigrationHooks) (platform.MigrationResult, error) {
	if len(types) == 0 {
		return platform.MigrationResult{}, fmt.Errorf("name at least one type to migrate")
	}
	sorted := slices.Clone(types)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)
	hosted := h.Hosted()
	for _, typ := range sorted {
		if !hosted[typ] {
			return platform.MigrationResult{}, fmt.Errorf("%s does not host %s", to, typ)
		}
	}
	sum := sha256.Sum256([]byte(from + "\x00" + to + "\x00" + strings.Join(sorted, ",")))
	key := "migration:" + hex.EncodeToString(sum[:8])
	result := platform.MigrationResult{From: from, To: to, Key: key, Types: []platform.MigrationType{}}
	for _, typ := range sorted {
		data, refusal := h.Export(typ)
		if refusal != nil {
			return result, fmt.Errorf("export %s from %s: %s", typ, from, refusal.Code)
		}
		rows, refusal := h.Import(typ, data)
		if refusal != nil {
			return result, fmt.Errorf("import %s into %s: %s", typ, to, refusal.Code)
		}
		part := platform.MigrationType{Type: typ, Rows: len(rows)}
		for _, row := range rows {
			if row.Outcome == "ok" {
				part.Written++
			} else {
				part.Refused = append(part.Refused, row)
			}
		}
		result.Types = append(result.Types, part)
	}
	return result, nil
}

func LoadTemplates(dir string) []platform.TenantTemplate {
	out := []platform.TenantTemplate{}
	if dir == "" {
		return out
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var t platform.TenantTemplate
		if json.Unmarshal(raw, &t) != nil {
			log.Printf("template %s: not a tenant template", e.Name())
			continue
		}
		t.Name = strings.TrimSuffix(e.Name(), ".json")
		if t.Title == "" {
			t.Title = t.Name
		}
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b platform.TenantTemplate) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func NewTenantSpec(templates func() []platform.TenantTemplate, req platform.CreateTenantRequest, by string, now time.Time) (platform.TenantSpec, platform.TenantTemplate, error) {
	var tpl platform.TenantTemplate
	if !ValidTenantID(req.ID) {
		return platform.TenantSpec{}, tpl, fmt.Errorf("tenant id: 2-32 lower-case letters, digits and dashes, starting with a letter")
	}
	if !strings.HasPrefix(req.Admin, "user:") && !strings.HasPrefix(req.Admin, "client:") {
		return platform.TenantSpec{}, tpl, fmt.Errorf("admin: user:<email> or client:<id>")
	}
	if req.Template != "" {
		offered := templates()
		i := slices.IndexFunc(offered, func(t platform.TenantTemplate) bool { return t.Name == req.Template })
		if i < 0 {
			return platform.TenantSpec{}, tpl, fmt.Errorf("template %q: not offered", req.Template)
		}
		tpl = offered[i]
	}
	settings := map[string]string{}
	for k, v := range tpl.Settings {
		settings[k] = v
	}
	for k, v := range req.Settings {
		settings[k] = v
	}
	if req.Name != "" {
		settings[SettingName] = req.Name
	}
	_, after, _ := strings.Cut(req.Admin, ":")
	id, _, _ := strings.Cut(after, "@")
	id = strings.ToLower(regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(id), "-"))
	if id == "" {
		id = "admin"
	}
	seat := platform.Seat{Subjects: []string{req.Admin}, Member: platform.Member{ID: id, Roles: map[string]string{PlatformApp: Admin}}}
	return platform.TenantSpec{ID: req.ID, Name: req.Name, Template: req.Template, Seats: []platform.Seat{seat}, Settings: settings, Created: now.UTC(), By: by}, tpl, nil
}

type FirstDecisionHooks struct {
	Tenant       string
	Member       func(id string) (platform.Member, bool)
	HasApp       func(app string) bool
	DefaultRoles func() map[string]string
	Submit       func(admin platform.Member, key int, schema, typ, id string, payload any) error
}

func FirstDecisions(h FirstDecisionHooks, spec platform.TenantSpec, tpl platform.TenantTemplate) error {
	if len(spec.Seats) == 0 {
		return nil
	}
	admin, ok := h.Member(spec.Seats[0].ID)
	if !ok {
		return fmt.Errorf("tenant %s: first seat %s is not a member", h.Tenant, spec.Seats[0].ID)
	}
	key := 0
	decide := func(schema, typ, id string, payload any) error {
		key++
		if err := h.Submit(admin, key, schema, typ, id, payload); err != nil {
			return fmt.Errorf("%s %s: %s", schema, id, err.Error())
		}
		return nil
	}
	roles := tpl.AdminRoles
	if len(roles) == 0 {
		roles = h.DefaultRoles()
	}
	apps := make([]string, 0, len(roles))
	for app := range roles {
		apps = append(apps, app)
	}
	slices.Sort(apps)
	for _, app := range apps {
		if !h.HasApp(app) {
			continue
		}
		if err := decide(SchemaGrant, MemberType, admin.ID, map[string]string{"app": app, "role": roles[app]}); err != nil {
			return err
		}
		admin, _ = h.Member(admin.ID)
	}
	names := make([]string, 0, len(spec.Settings))
	for name := range spec.Settings {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := decide(SchemaSettingSet, SettingType, PlatformApp+"/"+name, map[string]string{"value": spec.Settings[name]}); err != nil {
			return err
		}
	}
	return nil
}

func HostAuthorizationInput(subject string, authenticated, admin bool, permission, tenant string) platform.AuthorizationInput {
	pass := platform.AuthorizationGate{Allowed: true, Reason: "The explicit host management boundary"}
	roles := []string{}
	if admin {
		roles = []string{"host-admin"}
	}
	definition, _ := platform.CanonicalDigest([]any{permission, tenant, "host-admin"})
	return platform.AuthorizationInput{Kind: "action", Subject: platform.AuthorizationSubject{ID: "host:" + subject, App: "host", Roles: roles}, Permission: permission, Allowed: []string{"host-admin"}, Context: &platform.AuthorizationContext{Tenant: pass, Principal: platform.AuthorizationGate{Allowed: authenticated && subject != "", Reason: "The operation principal is invalid or inactive"}, Row: pass, Fields: pass, Derived: pass, Markings: pass, Definition: definition}}
}
