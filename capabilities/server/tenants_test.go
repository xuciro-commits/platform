package platformserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// ADR-0078 §2.2: a host creates a tenant from a template with its first
// administrator; its settings and the administrator's roles are ordinary,
// journaled decisions; the spec is written back so the tenant survives a restart.
func TestCreateTenantFromTemplate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "plant.json"), []byte(`{"title":"Plant","settings":{"timezone":"Asia/Shanghai","currency":"CNY"}}`), 0o644)
	file := filepath.Join(t.TempDir(), "tenants.json")
	os.WriteFile(file, []byte(`[]`), 0o644)
	d := &Deployment{TenantsFile: file, TemplatesDir: dir}
	d.Tenants(TenantSpec{ID: "dev"})
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

	if _, _, err := d.spec(CreateTenantRequest{ID: "Bad ID", Admin: "user:a@b.c"}, "ops", now); err == nil {
		t.Fatal("an invalid id was accepted")
	}
	if _, _, err := d.spec(CreateTenantRequest{ID: "acme", Admin: "user:a@b.c", Template: "none"}, "ops", now); err == nil {
		t.Fatal("an unknown template was accepted")
	}
	spec, tpl, err := d.spec(CreateTenantRequest{ID: "acme", Name: "Acme Ltd", Admin: "user:jane.doe@acme.test", Template: "plant", Settings: map[string]string{"currency": "USD"}}, "ops", now)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Seats[0].ID != "jane-doe" || spec.Settings["timezone"] != "Asia/Shanghai" || spec.Settings["currency"] != "USD" || spec.Settings[SettingName] != "Acme Ltd" {
		t.Fatalf("spec %+v", spec)
	}
	if err := d.specs.add(spec); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	if !strings.Contains(string(raw), `"id": "acme"`) {
		t.Fatalf("spec not written back: %s", raw)
	}
	tn, err := NewTenant("acme", NewConsole("acme", d.SeatsFor("acme")...), newNotes("acme", "a"))
	if err != nil {
		t.Fatal(err)
	}
	tn.Record = func(Entry) {}
	if err := firstDecisions(tn, spec, tpl, now); err != nil {
		t.Fatal(err)
	}
	admin, _ := tn.member("jane-doe")
	if admin.Roles[PlatformApp] != Admin {
		t.Fatalf("admin roles %v", admin.Roles)
	}
	console := consoleOf(tn)
	record := console.tenantRecord()
	if record.Name != "Acme Ltd" || record.Settings[SettingTimezone] != "Asia/Shanghai" || record.Settings[SettingCurrency] != "USD" || record.Members != 1 {
		t.Fatalf("tenant record %+v", record)
	}

	// ADR-0079 §2: a profile over the tenant's defaults; only the fields sent change.
	decide := func(who platform.Member, target string, payload string) string {
		_, err := tn.Submit(who, &pb.Submission{TenantId: "acme", PrincipalId: who.ID, Authority: PlatformApp, IdempotencyKey: "p" + payload,
			Target: &pb.EntityRef{Type: ProfileType, Id: target}, Schema: &pb.SchemaRef{Name: SchemaProfileUpdate, Version: 1}, Payload: json.RawMessage(payload)}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	if got := decide(admin, "jane-doe", `{"displayName":"Jane Doe","timezone":"Mars/Olympus"}`); got != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("an unknown timezone: %s", got)
	}
	if got := decide(admin, "jane-doe", `{"displayName":"Jane Doe","timezone":"Europe/Berlin","digest":"daily"}`); got != "ok" {
		t.Fatal(got)
	}
	if got := decide(admin, "jane-doe", `{"title":"COO"}`); got != "ok" {
		t.Fatal(got)
	}
	a := console.Account("jane-doe")
	if a.DisplayName != "Jane Doe" || a.Title != "COO" || a.Effective.Timezone != "Europe/Berlin" || a.Effective.Email != "jane.doe@acme.test" || a.Effective.Digest != "daily" {
		t.Fatalf("account %+v", a)
	}
	if got := decide(admin, "jane-doe", `{"timezone":""}`); got != "ok" {
		t.Fatal(got)
	}
	if a := console.Account("jane-doe"); a.Effective.Timezone != "Asia/Shanghai" || console.Location("jane-doe").String() != "Asia/Shanghai" {
		t.Fatalf("an empty value returns to the tenant's default: %+v", a.Effective)
	}
	if got := decide(admin, "nobody", `{"title":"x"}`); got != "ERROR_CODE_NOT_FOUND" {
		t.Fatal(got)
	}
	// The directory survives a snapshot round trip with its profiles.
	snap, _ := console.Snapshot()
	again := NewConsole("acme")
	if err := again.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if again.Account("jane-doe").DisplayName != "Jane Doe" {
		t.Fatal("profile lost in the snapshot")
	}
}

// ADR-0079 §3: mail waits for the digest hour and the end of quiet hours in
// the member's own timezone; "today" is the member's day.
func TestReachDueAndMemberToday(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	at := func(h, m int) time.Time { return time.Date(2026, 10, 6, h, m, 0, 0, sh) }
	for _, c := range []struct {
		name string
		r    Reach
		now  time.Time
		want time.Time
	}{
		{"instant", Reach{Location: sh}, at(14, 5), at(14, 5)},
		{"hourly", Reach{Location: sh, Digest: "hourly"}, at(14, 5), at(15, 0)},
		{"daily before 8", Reach{Location: sh, Digest: "daily"}, at(2, 0), at(8, 0)},
		{"daily after 8", Reach{Location: sh, Digest: "daily"}, at(14, 5), time.Date(2026, 10, 7, 8, 0, 0, 0, sh)},
		{"quiet over midnight, at night", Reach{Location: sh, QuietFrom: "22:00", QuietTo: "08:00"}, at(23, 30), time.Date(2026, 10, 7, 8, 0, 0, 0, sh)},
		{"quiet over midnight, early", Reach{Location: sh, QuietFrom: "22:00", QuietTo: "08:00"}, at(3, 0), at(8, 0)},
		{"quiet within day", Reach{Location: sh, QuietFrom: "12:00", QuietTo: "13:00"}, at(12, 30), at(13, 0)},
		{"outside quiet", Reach{Location: sh, QuietFrom: "22:00", QuietTo: "08:00"}, at(9, 0), at(9, 0)},
	} {
		if got := c.r.Due(c.now); !got.Equal(c.want) {
			t.Errorf("%s: due %s, want %s", c.name, got.In(sh), c.want)
		}
	}
	m := platform.Member{ID: "x", Timezone: "Asia/Shanghai"}
	if got := m.Today(time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)); got != "2026-10-07" {
		t.Errorf("today in Shanghai at 17:00 UTC: %s", got)
	}
	if got := (platform.Member{}).Today(time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)); got != "2026-10-06" {
		t.Errorf("today without a zone: %s", got)
	}
}

// ADR-0078 §3.3: grants add up; Roles is the primary role per app; a grant
// past its until day no longer holds; revoke removes one role or all.
func TestGrantsAddUpAndExpire(t *testing.T) {
	seat := Seat{Subjects: []string{"user:admin@example.test"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}}
	tn, err := NewTenant("g", NewConsole("g", seat), newNotes("g", "a"))
	if err != nil {
		t.Fatal(err)
	}
	tn.Record = func(Entry) {}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	decide := func(schema, payload string) string {
		admin, _ := tn.member("admin")
		_, err := tn.Submit(admin, &pb.Submission{TenantId: "g", PrincipalId: "admin", Authority: PlatformApp, IdempotencyKey: schema + payload,
			Target: &pb.EntityRef{Type: MemberType, Id: "admin"}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: json.RawMessage(payload)}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	if got := decide(SchemaGrant, `{"app":"a","role":"writer","until":"2026-10-7"}`); got != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("bad day accepted: %s", got)
	}
	if got := decide(SchemaGrant, `{"app":"a","role":"writer","reason":"covers"}`); got != "ok" {
		t.Fatal(got)
	}
	if got := decide(SchemaGrant, `{"app":"platform","role":"auditor","until":"2000-01-01"}`); got != "ok" {
		t.Fatal(got)
	}
	m, _ := tn.member("admin")
	if m.Roles[PlatformApp] != Admin || m.Roles["a"] != "writer" || len(m.Grants) != 2 || !m.Holds("a", "writer") || m.Holds(PlatformApp, "auditor") {
		t.Fatalf("grants %+v roles %v", m.Grants, m.Roles)
	}
	if m.Grants[1].By != "admin" || m.Grants[1].Reason != "covers" || m.Grants[1].At.IsZero() {
		t.Fatalf("grant provenance %+v", m.Grants[1])
	}
	if got := decide(SchemaRevoke, `{"app":"a","role":"other"}`); got != "ok" {
		t.Fatal(got)
	}
	if m, _ = tn.member("admin"); m.Roles["a"] != "writer" {
		t.Fatal("revoking an unheld role removed the held one")
	}
	if got := decide(SchemaRevoke, `{"app":"a"}`); got != "ok" {
		t.Fatal(got)
	}
	if m, _ = tn.member("admin"); m.Roles["a"] != "" || m.Roles[PlatformApp] != Admin {
		t.Fatalf("after revoke %v", m.Roles)
	}
	snap, _ := consoleOf(tn).Snapshot()
	if !strings.Contains(string(snap), `"grants"`) {
		t.Fatal("grants not in the directory snapshot")
	}

	// ADR-0078 §3.4: the engine decides by any role held, and explains.
	if x, err := tn.Explain("admin", "a.note"); err != nil || x.Verdict.Allow || x.Verdict.Rule != "none" || x.Verdict.Reason == "" {
		t.Fatalf("explain before grant: %+v %v", x, err)
	}
	if got := decide(SchemaGrant, `{"app":"a","role":"writer"}`); got != "ok" {
		t.Fatal(got)
	}
	if x, _ := tn.Explain("admin", "a.note"); !x.Verdict.Allow || x.Verdict.Role != "writer" || x.App != "a" {
		t.Fatalf("explain after grant: %+v", x)
	}
	if x, _ := tn.Explain("admin", "platform:read:members"); !x.Verdict.Allow {
		t.Fatalf("explain read: %+v", x)
	}
	if _, err := tn.Explain("nobody", "a.note"); err == nil {
		t.Fatal("unknown member explained")
	}
	admin, _ := tn.member("admin")
	if _, err := tn.Submit(admin, &pb.Submission{TenantId: "g", PrincipalId: "admin", Authority: "a", IdempotencyKey: "note-1",
		Target: &pb.EntityRef{Type: "a.topic", Id: "t1"}, Schema: &pb.SchemaRef{Name: "a.note", Version: 1}, Payload: json.RawMessage(`{"text":"hi"}`)}, now); err != nil {
		t.Fatalf("second role did not unlock the action: %v", err)
	}
}

// ADR-0078 D: a custom role, a policy, a team and a delegation, each through the engine.
func TestAccessConfiguration(t *testing.T) {
	seats := []Seat{
		{Subjects: []string{"user:admin@example.test"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin}}},
		{Subjects: []string{"user:bo@example.test"}, Member: platform.Member{ID: "bo", Roles: map[string]string{}}},
		{Subjects: []string{"user:cy@example.test"}, Member: platform.Member{ID: "cy", Roles: map[string]string{}}},
	}
	tn, err := NewTenant("x", NewConsole("x", seats...), newNotes("x", "a"))
	if err != nil {
		t.Fatal(err)
	}
	tn.Record = func(Entry) {}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	seq := 0
	decide := func(who, app, schema, typ, id, payload string) string {
		m, _ := tn.member(who)
		seq++
		_, err := tn.Submit(m, &pb.Submission{TenantId: "x", PrincipalId: who, Authority: app, IdempotencyKey: fmt.Sprint("k", seq),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: json.RawMessage(payload)}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	note := func(who string) string { return decide(who, "a", "a.note", "a.topic", "t", `{"text":"x"}`) }
	if got := note("bo"); got == "ok" {
		t.Fatal("bo could note without a role")
	}
	// A custom role over the app's action; held like any role.
	if got := decide("admin", PlatformApp, SchemaRoleSave, RoleType, "writer", `{"app":"a","title":"x","actions":["a.note"]}`); got != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("declared role overwritten: %s", got)
	}
	if got := decide("admin", PlatformApp, SchemaRoleSave, RoleType, "scribe", `{"app":"a","title":"Scribe","actions":["a.note"]}`); got != "ok" {
		t.Fatal(got)
	}
	if got := decide("admin", PlatformApp, SchemaGrant, MemberType, "bo", `{"app":"a","role":"scribe"}`); got != "ok" {
		t.Fatal(got)
	}
	if got := note("bo"); got != "ok" {
		t.Fatalf("custom role did not unlock the action: %s", got)
	}
	if x, _ := tn.Explain("bo", "a.note"); !x.Verdict.Allow || x.Verdict.Role != "scribe" {
		t.Fatalf("%+v", x)
	}
	// A deny policy wins over the role; removed, the role is back.
	if got := decide("admin", PlatformApp, SchemaPolicySave, PolicyType, "freeze", `{"effect":"deny","permission":"a.*","where":{"member":"bo"}}`); got != "ok" {
		t.Fatal(got)
	}
	if got := note("bo"); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("policy ignored: %s", got)
	}
	if x, _ := tn.Explain("bo", "a.note"); x.Verdict.Allow || x.Verdict.Policy != "freeze" {
		t.Fatalf("%+v", x)
	}
	if got := decide("admin", PlatformApp, SchemaPolicyDrop, PolicyType, "freeze", `{}`); got != "ok" {
		t.Fatal(got)
	}
	// A team: cy holds the team's grant while a member of it.
	if got := decide("admin", PlatformApp, SchemaTeamSave, TeamType, "desk", `{"name":"Desk","members":["cy"],"grants":[{"app":"a","role":"writer"}]}`); got != "ok" {
		t.Fatal(got)
	}
	if cy, _ := tn.member("cy"); !cy.Holds("a", "writer") || cy.Grants[0].By != "team:desk" {
		t.Fatalf("team grant missing: %+v", cy.Grants)
	}
	if got := decide("admin", PlatformApp, SchemaTeamRemove, TeamType, "desk", `{}`); got != "ok" {
		t.Fatal(got)
	}
	if cy, _ := tn.member("cy"); cy.Holds("a", "writer") {
		t.Fatal("team grant survived the team")
	}
	// Delegation: bo gives cy what bo holds in a, until a day; cy cannot delegate what cy lacks.
	if got := decide("cy", PlatformApp, SchemaDelegate, MemberType, "bo", `{"app":"a","until":"2026-12-01"}`); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("delegated nothing: %s", got)
	}
	if got := decide("bo", PlatformApp, SchemaDelegate, MemberType, "cy", `{"app":"a","until":"2026-12-01","reason":"holiday"}`); got != "ok" {
		t.Fatal(got)
	}
	cy, _ := tn.member("cy")
	if !cy.Holds("a", "scribe") || cy.Grants[0].By != "bo" || cy.Grants[0].Until != "2026-12-01" || note("cy") != "ok" {
		t.Fatalf("delegation: %+v", cy.Grants)
	}
	// Survives a snapshot and restore, custom role included.
	snap, _ := consoleOf(tn).Snapshot()
	restored, _ := NewTenant("x", NewConsole("x", seats...), newNotes("x", "a"))
	if err := consoleOf(restored).Restore(snap); err != nil {
		t.Fatal(err)
	}
	if access := consoleOf(restored).Access(); len(access.Roles) != 1 || !restored.app("a").Manifest().Actions.PermitsAny([]string{"scribe"}, "a.note") {
		t.Fatalf("custom role not restored: %+v", access)
	}
}

// ADR-0079 C/D: standing (invite, suspend, resume, offboard), personal
// tokens with scopes, sessions; all of it replays.
func TestLifecycleTokensAndSessions(t *testing.T) {
	seats := []Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin, "a": "writer"}}},
		{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{"a": "writer"}}},
	}
	compose := func() *Tenant {
		tn, err := NewTenant("l", NewConsole("l", seats...), newNotes("l", "a"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	seq := 0
	submitAs := func(who platform.Member, app, schema, typ, id, payload string) string {
		seq++
		_, err := tn.Submit(who, &pb.Submission{TenantId: "l", PrincipalId: who.ID, Authority: app, IdempotencyKey: fmt.Sprint("k", seq),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: json.RawMessage(payload)}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	decide := func(who, app, schema, typ, id, payload string) string {
		m, _ := tn.member(who)
		return submitAs(m, app, schema, typ, id, payload)
	}
	d := consoleOf(tn)
	// Invite: stands invited, may be granted, becomes active on first sign-in.
	if got := decide("admin", PlatformApp, SchemaInvite, MemberType, "cy", `{"subject":"user:cy@example.test"}`); got != "ok" {
		t.Fatal(got)
	}
	if got := decide("admin", PlatformApp, SchemaGrant, MemberType, "cy", `{"app":"a","role":"writer"}`); got != "ok" {
		t.Fatal(got)
	}
	if cy, _ := tn.member("cy"); cy.Status != platform.MemberInvited || !cy.Holds("a", "writer") {
		t.Fatalf("invited: %+v", cy)
	}
	d.seen("cy", now)
	if cy, _ := tn.member("cy"); cy.Status != "" {
		t.Fatal("first sign-in did not accept the invitation")
	}
	// Suspend: holds nothing, cannot act; resume: back.
	if got := decide("admin", PlatformApp, SchemaMemberSuspend, MemberType, "bo", `{"reason":"audit"}`); got != "ok" {
		t.Fatal(got)
	}
	if bo, _ := tn.member("bo"); bo.Active() || len(bo.RolesIn("a")) != 0 || decide("bo", "a", "a.note", "a.topic", "t", `{}`) == "ok" {
		t.Fatalf("suspended member still acts: %+v", bo)
	}
	if got := decide("admin", PlatformApp, SchemaMemberResume, MemberType, "bo", `{}`); got != "ok" {
		t.Fatal(got)
	}
	if got := decide("bo", "a", "a.note", "a.topic", "t", `{}`); got != "ok" {
		t.Fatal(got)
	}
	if got := decide("admin", PlatformApp, SchemaMemberSuspend, MemberType, "admin", `{}`); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("suspended self: %s", got)
	}
	// Tokens: issued by bo, scoped, secret once; a token cannot mint tokens.
	if got := decide("bo", PlatformApp, SchemaTokenIssue, TokenType, "ci", `{"label":"CI","scopes":["a.note"],"until":"2027-01-01"}`); got != "ok" {
		t.Fatal(got)
	}
	secret, ok := d.Minted("ci", "bo", now)
	if !ok || !strings.HasPrefix(secret, tokenPrefix) {
		t.Fatalf("no secret: %q %v", secret, ok)
	}
	if _, again := d.Minted("ci", "bo", now); again {
		t.Fatal("secret handed out twice")
	}
	if _, wrong := d.Minted("ci", "admin", now); wrong {
		t.Fatal("secret handed to another member")
	}
	if raw, _ := json.Marshal(entries); strings.Contains(string(raw), secret) || strings.Contains(string(raw), secret[len(tokenPrefix):]) {
		t.Fatal("the journal keeps the secret")
	}
	viaToken, ok := d.MemberByToken(secret, now)
	if !ok || viaToken.ID != "bo" || !slices.Equal(viaToken.Scopes, []string{"a.note"}) {
		t.Fatalf("token sign-in: %+v %v", viaToken, ok)
	}
	if got := submitAs(viaToken, "a", "a.note", "a.topic", "t2", `{}`); got != "ok" {
		t.Fatalf("in-scope action refused: %s", got)
	}
	if got := submitAs(viaToken, PlatformApp, SchemaProfileUpdate, ProfileType, "bo", `{"title":"x"}`); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("out-of-scope action allowed: %s", got)
	}
	if got := submitAs(viaToken, PlatformApp, SchemaTokenIssue, TokenType, "ci2", `{"label":"more"}`); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("token minted a token: %s", got)
	}
	if _, ok := d.MemberByToken(secret, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); ok {
		t.Fatal("expired token accepted")
	}
	if views := d.Tokens("bo", now); len(views) != 1 || views[0].Label != "CI" || views[0].LastUsed.IsZero() {
		t.Fatalf("token views: %+v", views)
	}
	// Sessions: two credentials seen, end the other, it is refused.
	d.noticed("bo", "cred-1", "Mozilla/5.0 Chrome/130.0", "sign-in", now)
	d.noticed("bo", "cred-2", "curl/8.0", "sign-in", now.Add(time.Minute))
	if ss := d.Sessions("bo", "cred-1", now); len(ss) != 2 || ss[0].Agent != "curl 8" || ss[1].Current != true {
		t.Fatalf("sessions: %+v", ss)
	}
	if n := d.EndOtherSessions("bo", "cred-1", now); n != 1 || d.noticed("bo", "cred-2", "", "sign-in", now) || !d.noticed("bo", "cred-1", "", "sign-in", now) {
		t.Fatal("ending other sessions")
	}
	// The ended session stays listed, marked, for a day; then it is gone.
	if ss := d.Sessions("bo", "cred-1", now); len(ss) != 2 || !ss[0].Current || ss[1].Ended.IsZero() || ss[1].Agent != "curl 8" {
		t.Fatalf("ended session not shown as ended: %+v", ss)
	}
	if ss := d.Sessions("bo", "cred-1", now.Add(endedSessionsShown+time.Minute)); len(ss) != 1 || !ss[0].Current {
		t.Fatalf("ended session did not age out: %+v", ss)
	}
	// Admin revokes the token; offboard removes subjects and grants.
	if got := decide("admin", PlatformApp, SchemaTokenRevoke, TokenType, "ci", `{}`); got != "ok" {
		t.Fatal(got)
	}
	if _, ok := d.MemberByToken(secret, now); ok {
		t.Fatal("revoked token accepted")
	}
	if got := decide("admin", PlatformApp, SchemaOffboard, MemberType, "bo", `{"reason":"left"}`); got != "ok" {
		t.Fatal(got)
	}
	if _, ok := d.Member("bo"); ok {
		t.Fatal("one who left still signs in")
	}
	if bo, ok := tn.member("bo"); !ok || bo.Status != platform.MemberLeft || len(bo.Grants) != 0 {
		t.Fatalf("offboarded: %+v", bo)
	}
	if got := decide("admin", PlatformApp, SchemaMemberResume, MemberType, "bo", `{}`); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("resumed one who left: %s", got)
	}
	CheckReplay(t, tn, entries, compose)
}
