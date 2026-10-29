package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"platformserver/aigateway"
	"platformserver/engine/authorization"
	"platformserver/engine/business"
	"platformserver/engine/resources"
	"platformserver/journal"
	"platformserver/studio/build"

	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/authoring"
	"platformserver/studio/packages"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// The host tests' kit (see AGENTS.md). A test composes a tenant
// with the seats it needs, then speaks to it as a member through decide /
// refuse; it does not build pb.Submission by hand or keep its own idempotency
// counter. New tests use the kit; old closures move onto it when their file
// is touched.

// seatOf is one member of a tenant: a development token equal to its ID, and
// its app roles as app:role pairs ("build:builder", "pms:front-desk").
func seatOf(id string, roles ...string) platform.Seat {
	m := platform.Member{ID: id, Roles: map[string]string{}}
	for _, r := range roles {
		for i := range r {
			if r[i] == ':' {
				m.Roles[r[:i]] = r[i+1:]
				break
			}
		}
	}
	return platform.Seat{Subjects: []string{id}, Member: m}
}

// composeTenant is a tenant on the Console with these seats and apps, failing
// the test if the composition is refused.
func composeTenant(t *testing.T, id string, seats []platform.Seat, apps ...platform.App) *Tenant {
	return composeTenantWithCapabilities(t, id, DefaultRuntimeCapabilities(), seats, apps...)
}

func composeTenantWithCapabilities(t *testing.T, id string, services RuntimeCapabilities, seats []platform.Seat, apps ...platform.App) *Tenant {
	t.Helper()
	tn, err := newTestTenantWithCapabilities(id, services, append([]platform.App{newConsole(id, seats...)}, apps...)...)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

// builderTenant is the common case: a tenant with the builder app, one
// builder ("builder") and one business user ("user"), plus any further apps.
func builderTenant(t *testing.T, id string, apps ...platform.App) *Tenant {
	t.Helper()
	return composeTenant(t, id, []platform.Seat{seatOf("builder", build.ID+":"+build.Builder), seatOf("user", build.ID+":"+build.User)}, append([]platform.App{build.New(id)}, apps...)...)
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
		t.Fatalf("%s %s/%s as %s: %s: %s (fault=%+v)", schema, typ, id, who, err.Code, err.Message, tn.fault.Load())
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

func testConsoleOf(t *Tenant) *controlowner.Console {
	c, _ := t.app(controlowner.PlatformApp).(*controlowner.Console)
	return c
}

// runJob takes the real accepted-work path for a single owned job. A test must
// not call Runner.Run with the legacy live runtime to create new records.
func runJob(t *testing.T, tn *Tenant, app, name string, at time.Time) {
	t.Helper()
	tn.mu.Lock()
	defer tn.mu.Unlock()
	for _, task := range tn.work.Jobs {
		if task.App == app && task.Job.Name == name {
			if outcome := tn.run(task, at, false); outcome != "ok" {
				t.Fatalf("job %s/%s: %s", app, name, outcome)
			}
			return
		}
	}
	t.Fatalf("job %s/%s is not declared", app, name)
}

// approveTestInference models the deployment operator's explicit origin review.
// Adding a tenant provider alone never grants this runtime permission.
func approveTestInference(t *testing.T, origin string, providers ...string) {
	t.Helper()
	policy := platform.AIInferencePolicy{}
	if raw := os.Getenv("PLATFORM_AI_INFERENCE_POLICY"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &policy); err != nil {
			t.Fatal(err)
		}
	}
	canonical, err := aigateway.Origin(origin)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range providers {
		found := false
		for _, site := range policy.Runtime {
			if site.Provider == provider && site.Origin == canonical {
				found = true
			}
		}
		if !found {
			policy.Runtime = append(policy.Runtime, platform.AIInferenceSite{Provider: provider, Origin: canonical, Location: "local"})
		}
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PLATFORM_AI_INFERENCE_POLICY", string(raw))
}

// An operator can configure the exact synthetic endpoint before inference in a
// fixture assembled before its HTTP server. This does not trust provider kind.
func approveTenantInference(t *testing.T, tenant *Tenant, origin string, providers ...string) {
	t.Helper()
	approveTestInference(t, origin, providers...)
	var policy platform.AIInferencePolicy
	if err := json.Unmarshal([]byte(os.Getenv("PLATFORM_AI_INFERENCE_POLICY")), &policy); err != nil {
		t.Fatal(err)
	}
	if err := aigateway.CheckPolicy(policy); err != nil {
		t.Fatal(err)
	}
	tenant.services.AIInference = &policy
}

func bindTestOwner(app platform.App, roles platform.OwnerBindings) {
	if owner, ok := app.(platform.AttachedAITraining); ok {
		owner.AttachAITraining(roles.Training)
	}
	if owner, ok := app.(platform.AttachedSystemDefinitions); ok {
		owner.AttachSystemDefinitions(roles.System)
	}
	if owner, ok := app.(platform.AttachedAgent); ok {
		owner.AttachAgent(roles.Agent)
	}
	if owner, ok := app.(platform.ConsoleAttached); ok {
		owner.AttachConsole(roles.Console)
	}
	if owner, ok := app.(platform.AttachedBuild); ok {
		owner.AttachBuild(roles.Build)
	}
	if owner, ok := app.(platform.AttachedFiles); ok {
		owner.AttachFiles(roles.Files)
	}
	if owner, ok := app.(platform.AttachedKnowledge); ok {
		owner.AttachKnowledge(roles.Knowledge)
	}
	if owner, ok := app.(platform.AttachedRelations); ok {
		owner.AttachRelations(roles.Relations)
	}
	if owner, ok := app.(platform.AttachedFlow); ok {
		owner.AttachFlow(roles.Flow)
	}
	if owner, ok := app.(platform.AttachedWork); ok {
		owner.AttachWork(roles.Work)
	}
}

func NewTenant(id string, apps ...platform.App) (*Tenant, error) {
	services := DefaultRuntimeCapabilities()
	services.BindOwner = bindTestOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = business.NewRuntime
	return NewTenantWithCapabilities(id, services, apps...)
}
func newTestTenantWithCapabilities(id string, services RuntimeCapabilities, apps ...platform.App) (*Tenant, error) {
	services.BindOwner = bindTestOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = business.NewRuntime
	return NewTenantWithCapabilities(id, services, apps...)
}

// Member fixtures use the identity owner's retained image, so tests never
// reach into its private directory maps after the owner moves packages.
func rewriteMember(t *testing.T, directory platform.AcceptedStateApp, id string, change func(*platform.Member)) {
	t.Helper()
	raw, err := directory.AcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]json.RawMessage
	if err = json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	var members map[string]*platform.Member
	if err = json.Unmarshal(state["members"], &members); err != nil {
		t.Fatal(err)
	}
	member := members[id]
	if member == nil {
		t.Fatalf("member %s unavailable", id)
	}
	change(member)
	state["members"] = platform.Raw(members)
	if err = directory.ApplyAcceptedState(platform.Raw(state)); err != nil {
		t.Fatal(err)
	}
}

func noticeValues(tenant *Tenant) []platform.Notification {
	values, _ := tenant.notices.State()
	return values
}

func pageActions(page *platform.Page) []platform.AssetRef {
	for _, trigger := range page.ActionTriggers {
		if trigger.Binding == "page" {
			return trigger.Actions
		}
	}
	return nil
}

func newConsole(tenant string, seats ...platform.Seat) *controlowner.Console {
	return controlowner.NewConsole(tenant, func(id string) controlowner.Owners {
		actions := append(platform.AccessActions(), platform.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: authorization.NewPolicies(), Packages: packages.New(id), Actions: actions}
	}, seats...)
}

type unavailableInventoryFiles struct{ *resources.MemoryFiles }

func (s unavailableInventoryFiles) Inventory(context.Context, string, string, int) (platform.FileInventoryPage, error) {
	return platform.FileInventoryPage{}, fmt.Errorf("inventory unavailable")
}

// A partial prefix cannot pretend to be an exhausted complete inventory.
type badCursorInventoryFiles struct {
	*resources.MemoryFiles
	key string
}

func (s badCursorInventoryFiles) Inventory(context.Context, string, string, int) (platform.FileInventoryPage, error) {
	return platform.FileInventoryPage{Files: []platform.StoredFile{{Key: s.key, Size: 2, Modified: time.Now().Add(-48 * time.Hour)}}, Next: "truncated"}, nil
}

// storedFiles enumerates the real store through its inventory contract.
func storedFiles(t *testing.T, files platform.FileInventory) []platform.StoredFile {
	t.Helper()
	var all []platform.StoredFile
	cursor := ""
	for {
		page, err := files.Inventory(t.Context(), "", cursor, platform.MaxFileInventoryPage)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page.Files...)
		if page.Next == "" {
			return all
		}
		cursor = page.Next
	}
}

// readView binds a committed read view to a SQL backed tenant for direct row
// inspection and returns the release. SQL rows are read only through a view; an
// unbound store refuses to answer. Memory and file tenants need no view.
func readView(t *testing.T, tn *Tenant) func() {
	t.Helper()
	if tn.records.current.View() != nil {
		return func() {}
	}
	views, ok := tn.records.current.Backend().(journal.StateReadViews)
	if !ok {
		return func() {}
	}
	view, err := views.BeginStateRead(context.Background(), tn.ID, tn.records.current.Applied())
	if err != nil {
		t.Fatal(err)
	}
	tn.records.current.Bind(view)
	return func() {
		tn.records.current.Bind(nil)
		_ = view.Close()
	}
}

func rowCount(t *testing.T, tn *Tenant, typ string) int {
	t.Helper()
	defer readView(t, tn)()
	return tn.records.types[typ].rows.Len()
}
