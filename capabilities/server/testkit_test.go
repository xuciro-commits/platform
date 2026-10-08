package platformserver

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// The host tests' kit (docs/Testing.md "测试规范"). A test composes a tenant
// with the seats it needs, then speaks to it as a member through decide /
// refuse; it does not build pb.Submission by hand or keep its own idempotency
// counter. New tests use the kit; old closures move onto it when their file
// is touched.

// seatOf is one member of a tenant: a development token equal to its ID, and
// its app roles as app:role pairs ("build:builder", "pms:front-desk").
func seatOf(id string, roles ...string) Seat {
	m := platform.Member{ID: id, Roles: map[string]string{}}
	for _, r := range roles {
		for i := range r {
			if r[i] == ':' {
				m.Roles[r[:i]] = r[i+1:]
				break
			}
		}
	}
	return Seat{Subjects: []string{id}, Member: m}
}

// composeTenant is a tenant on the Console with these seats and apps, failing
// the test if the composition is refused.
func composeTenant(t *testing.T, id string, seats []Seat, apps ...platform.App) *Tenant {
	t.Helper()
	tn, err := NewTenant(id, append([]platform.App{NewConsole(id, seats...)}, apps...)...)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

// builderTenant is the common case: a tenant with the builder app, one
// builder ("builder") and one business user ("user"), plus any further apps.
func builderTenant(t *testing.T, id string, apps ...platform.App) *Tenant {
	t.Helper()
	return composeTenant(t, id, []Seat{seatOf("builder", build.ID+":"+build.Builder), seatOf("user", build.ID+":"+build.User)}, append([]platform.App{build.New(id)}, apps...)...)
}

// memberOf is the tenant's member with this ID, failing the test if the
// Console does not seat it.
func memberOf(t *testing.T, tn *Tenant, id string) platform.Member {
	t.Helper()
	m, ok := tn.Member(id)
	if !ok {
		t.Fatalf("%s: no member %q", tn.ID, id)
	}
	return m
}

var kitKeys atomic.Int64

// submission is a decision by member who under authority app, with a fresh
// idempotency key; schema is the full name ("build.object.create").
func submission(tn *Tenant, who platform.Member, app, schema, typ, id string, payload any) *pb.Submission {
	return &pb.Submission{TenantId: tn.ID, PrincipalId: who.ID, Authority: app, IdempotencyKey: fmt.Sprintf("kit-%d", kitKeys.Add(1)),
		Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
}

// decide submits as the member with this ID and fails the test on refusal;
// it resolves to the accepted change.
func decide(t *testing.T, tn *Tenant, who, app, schema, typ, id string, payload any, at time.Time) *pb.ChangeRecord {
	t.Helper()
	rec, err := tn.Submit(memberOf(t, tn, who), submission(tn, memberOf(t, tn, who), app, schema, typ, id, payload), at)
	if err != nil {
		t.Fatalf("%s %s/%s as %s: %s: %s", schema, typ, id, who, err.Code, err.Message)
	}
	return rec
}

// refuse submits and resolves to the refusal's "code: message", or "ok" when
// the host accepted — for tables of who-may-do-what.
func refuse(t *testing.T, tn *Tenant, who, app, schema, typ, id string, payload any, at time.Time) string {
	t.Helper()
	if _, err := tn.Submit(memberOf(t, tn, who), submission(tn, memberOf(t, tn, who), app, schema, typ, id, payload), at); err != nil {
		return fmt.Sprintf("%s: %s", err.Code, err.Message)
	}
	return "ok"
}

// publishObject creates and publishes a tenant-defined object as the builder
// and resolves to its record type ("build.<name>").
func publishObject(t *testing.T, tn *Tenant, who, id string, object map[string]any, at time.Time) string {
	t.Helper()
	decide(t, tn, who, build.ID, "build.object.create", build.ObjectType, id, object, at)
	decide(t, tn, who, build.ID, "build.object.publish", build.ObjectType, id, map[string]any{}, at)
	return build.ID + "." + object["name"].(string)
}
