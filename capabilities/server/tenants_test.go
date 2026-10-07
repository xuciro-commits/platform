package platformserver

import (
	"encoding/json"
	"os"
	"path/filepath"
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
