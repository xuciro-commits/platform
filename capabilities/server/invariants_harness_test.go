package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/engine/authorization"
	"platformserver/engine/business"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/authoring"
	"platformserver/studio/build"
	"platformserver/studio/packages"
)

var invariantKeys atomic.Int64

var invariantClock = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)

type invariantTenant struct {
	*Tenant
	entries []Entry
}

func invariantBindOwner(app platform.App, roles platform.OwnerBindings) {
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

func newInvariantTenant(tb testing.TB, id string) *invariantTenant {
	tb.Helper()
	services := platform.DefaultRuntimeCapabilities()
	services.BindOwner = invariantBindOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = business.NewRuntime
	seat := platform.Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}
	console := controlowner.NewConsole(id, func(id string) controlowner.Owners {
		actions := append(platform.AccessActions(), platform.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: authorization.NewPolicies(), Packages: packages.New(id), Actions: actions}
	}, seat)
	tenant, err := NewTenantWithCapabilities(id, services, console, build.New(id))
	if err != nil {
		tb.Fatal(err)
	}
	live := &invariantTenant{Tenant: tenant}
	tenant.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		live.entries = append(live.entries, e)
		return e.Body, nil
	}
	return live
}

func (t *invariantTenant) member(tb testing.TB) platform.Member {
	tb.Helper()
	m, ok := t.Member("builder")
	if !ok {
		tb.Fatalf("%s: the builder is not seated", t.ID)
	}
	return m
}

func (t *invariantTenant) submission(schema, typ, id string, payload any) *pb.Submission {
	return &pb.Submission{TenantId: t.ID, PrincipalId: "builder", Authority: build.ID,
		IdempotencyKey: fmt.Sprintf("invariant-%d", invariantKeys.Add(1)),
		Target:         &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
}

func (t *invariantTenant) accept(tb testing.TB, schema, typ, id string, payload any) *pb.ChangeRecord {
	tb.Helper()
	record, refusal := t.Submit(t.member(tb), t.submission(schema, typ, id, payload), invariantClock)
	if refusal != nil {
		tb.Fatalf("%s %s/%s was refused: %s: %s", schema, typ, id, refusal.Code, refusal.Message)
	}
	return record
}

func (t *invariantTenant) restore(tb testing.TB, entries []Entry) *invariantTenant {
	tb.Helper()
	stored := make([]Entry, len(entries))
	for i, e := range entries {
		raw, err := json.Marshal(e)
		if err != nil {
			tb.Fatal(err)
		}
		if err := json.Unmarshal(raw, &stored[i]); err != nil {
			tb.Fatal(err)
		}
	}
	fresh := newInvariantTenant(tb, t.ID)
	fresh.Outbound = func(*http.Request, bool) (*http.Response, error) {
		tb.Fatal("replay made an outbound call")
		return nil, nil
	}
	if err := fresh.recoverEntries(stored); err != nil {
		tb.Fatalf("replay of %d entries: %v", len(stored), err)
	}
	return fresh
}

func (t *invariantTenant) recordImage(tb testing.TB) map[string]json.RawMessage {
	tb.Helper()
	raw, _, err := t.Snapshot(func() int64 { return 0 })
	if err != nil {
		tb.Fatalf("snapshot: %v", err)
	}
	var state struct {
		Records map[string]json.RawMessage `json:"records"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		tb.Fatal(err)
	}
	return state.Records
}

func (t *invariantTenant) sealedRefusal(tb testing.TB, at int) (acceptedformat.Refusal, *pb.Submission) {
	tb.Helper()
	if at >= len(t.entries) || t.entries[at].Kind != "accepted-result" {
		tb.Fatalf("journal entry %d of %d is not an accepted result", at, len(t.entries))
	}
	saved, submission, err := acceptedformat.DecodeRefusal(t.entries[at].Body)
	if err != nil {
		tb.Fatalf("journal entry %d is not a sealed refusal: %v", at, err)
	}
	return saved, submission
}

func differingTypes(a, b map[string]json.RawMessage) []string {
	var out []string
	for typ, image := range a {
		if other, ok := b[typ]; !ok || !bytes.Equal(image, other) {
			out = append(out, typ)
		}
	}
	for typ := range b {
		if _, ok := a[typ]; !ok {
			out = append(out, typ)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func invariantObject(name, title string) map[string]any {
	return map[string]any{"name": name, "title": title, "fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}}
}
