package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// probe takes a gateway's feed as a connector, tells the line's supervisors
// about each delivery, and runs a job that tells them again unless its limit is 0.
type probe struct{ *notes }

func (p probe) Manifest() platform.Manifest {
	m := p.notes.Manifest()
	m.Jobs = []platform.Job{{Name: "tick", Title: "Tick", Every: time.Minute}}
	m.Settings = []platform.Setting{{Name: "limit", Title: "Limit", Type: "integer", Default: "0"}}
	return m
}

var supervisors = platform.Recipient{Structure: "site", Unit: "L1", Role: "supervisor"}

func (p probe) Input(c platform.Caller, _ string, body []byte, now time.Time) (any, *kernel.Error) {
	if err := c.Deliver("p.topic", "", "", now); err != nil {
		return nil, err
	}
	c.Notify(platform.Notification{Title: "Feed " + string(body), Ref: "p.topic/feed"}, now, supervisors)
	return nil, nil
}

func (p probe) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	if c.Setting("limit") == "0" {
		return nil
	}
	c.Notify(platform.Notification{Title: "Tick", Key: "tick"}, now, supervisors, platform.Recipient{Member: "ana"})
	return nil
}

func TestOperations(t *testing.T) {
	var journal []Entry
	build := func() *Tenant {
		dir := NewConsole("t-1",
			Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{PlatformApp: Admin}}},
			Seat{Subjects: []string{"sup"}, Member: platform.Member{ID: "sup", Roles: map[string]string{}}},
			Seat{Subjects: []string{"op"}, Member: platform.Member{ID: "op", Roles: map[string]string{}}},
			Seat{Subjects: []string{"gw"}, Member: platform.Member{ID: "gw", Roles: map[string]string{}}})
		org := NewOrganization("t-1", platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}},
			Units: []platform.Unit{{ID: "plant", Kind: "plant"}, {ID: "L1", Kind: "line"}},
			Edges: []platform.Edge{{Structure: "site", Unit: "L1", Parent: "plant"}},
			Memberships: []platform.Membership{{Party: "member:sup", Unit: "plant", Role: "supervisor"}, {Party: "member:op", Unit: "L1", Role: "operator"},
				{Party: "member:ana", Unit: "plant", Role: "director", Until: "2026-01-01"}}})
		tn, err := NewTenant("t-1", dir, org, probe{newNotes("t-1", "p")})
		if err != nil {
			t.Fatal(err)
		}
		if err := tn.Connect(&pb.ConnectorDescriptor{ConnectorId: "gw", Direction: pb.ConnectorDirection_CONNECTOR_DIRECTION_PUSH,
			DataClasses: []string{"p.topic"}, Heartbeat: durationpb.New(time.Minute)}); err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	ana, sup, op, gw := member("ana"), member("sup"), member("op"), member("gw")
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	n := 0
	decide := func(m platform.Member, schema, typ, id, payload string) *kernel.Error {
		n++
		_, err := tn.Submit(m, &pb.Submission{TenantId: "t-1", PrincipalId: m.ID, Authority: PlatformApp, IdempotencyKey: fmt.Sprint("d", n),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, now)
		return err
	}
	code := func(err *kernel.Error) string {
		if err == nil {
			return "ok"
		}
		return err.Error()
	}

	// Connectors: deliveries, health, the last refused input, disable and enable.
	if _, err := tn.Input(gw, "p-feed", []byte("1"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.Input(gw, "heartbeat", nil, now); err != nil {
		t.Fatal(err)
	}
	if err := decide(op, SchemaConnectorOff, ConnectorType, "gw", "{}"); code(err) != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("an operator disabled a connector: %v", err)
	}
	if err := decide(ana, SchemaConnectorOff, ConnectorType, "gw", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.Input(gw, "p-feed", []byte("2"), now.Add(time.Second)); code(err) != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("a disabled connector delivered: %v", err)
	}
	c := tn.Connectors(now.Add(time.Second))
	if len(c) != 1 || c[0].Health != "disabled" || c[0].LastError == nil || c[0].LastError.Error != "ERROR_CODE_POLICY_DENIED" || c[0].LastSeen == nil {
		t.Fatalf("connectors %+v", c)
	}
	if err := decide(ana, SchemaConnectorOn, ConnectorType, "gw", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := tn.Input(gw, "p-feed", []byte("3"), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if c := tn.Connectors(now.Add(2 * time.Minute)); c[0].Health != "stale" {
		t.Fatalf("silent past its heartbeat, but %s", c[0].Health)
	}

	// Notifications reach who answers for the unit, and only them; each member reads and marks their own.
	inbox := func(m platform.Member) []platform.Notification {
		out, err := tn.Read(m, "notifications")
		if err != nil {
			t.Fatalf("%s reads notifications: %v", m.ID, err)
		}
		return out.([]platform.Notification)
	}
	if got := inbox(sup); len(got) != 2 || got[0].Title != "Feed 3" || got[0].App != "p" || got[0].Read {
		t.Fatalf("sup's inbox %+v", got)
	}
	if got := inbox(op); len(got) != 0 {
		t.Fatalf("an operator of the line got %+v", got)
	}
	if _, err := tn.Read(sup, "work"); code(err) != "ERROR_CODE_POLICY_DENIED" {
		t.Fatal("a member without the platform role read its work")
	}
	first := inbox(sup)[1].ID
	if err := decide(op, SchemaNotificationRead, NotificationType, first, "{}"); code(err) != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("op marked sup's notification: %v", err)
	}
	if err := decide(sup, SchemaNotificationRead, NotificationType, first, "{}"); err != nil {
		t.Fatal(err)
	}

	// Settings are typed; the job reads them. A run that tells no one is not journaled.
	if err := decide(ana, SchemaSettingSet, SettingType, "p/limit", `{"value":"many"}`); code(err) != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("an integer setting took text: %v", err)
	}
	entries := len(journal)
	tn.Work(now)
	if len(journal) != entries {
		t.Fatal("a job run that did nothing was journaled")
	}
	if err := decide(ana, SchemaSettingSet, SettingType, "p/limit", `{"value":"3"}`); err != nil {
		t.Fatal(err)
	}
	tn.Work(now.Add(time.Minute))
	tn.Work(now.Add(2 * time.Minute)) // "tick" is already in every inbox: nothing new
	if got := inbox(sup); len(got) != 3 || got[0].Title != "Tick" || len(inbox(ana)) != 1 {
		t.Fatalf("after ticks: sup %+v, ana %+v", got, inbox(ana))
	}
	kinds := []string{}
	for _, e := range journal {
		kinds = append(kinds, e.Kind)
	}
	if !slices.Equal(kinds, []string{"p-feed", "submission", "submission", "p-feed", "submission", "submission", "job"}) {
		t.Fatalf("journal %v", kinds)
	}
	if s := tn.Settings(); len(s) != 2 || s[1].App != "p" || s[1].Settings[0].Value != "3" { // the platform's own, then the app's
		t.Fatalf("settings %+v", s)
	}

	CheckReplay(t, tn, journal, build)
	// A replay rebuilds connectors, notifications, what was read and the settings.
	again := build()
	if err := again.Replay(journal); err != nil {
		t.Fatal(err)
	}
	view := func(tn *Tenant) string {
		c := tn.Connectors(now)
		raw, _ := json.Marshal([]any{c[0].Disabled, c[0].LastSeen, tn.notices, tn.Settings()})
		return string(raw)
	}
	if view(again) != view(tn) {
		t.Fatalf("replayed operations differ:\n%s\n%s", view(tn), view(again))
	}
}
