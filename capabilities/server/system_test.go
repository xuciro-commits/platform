package platformserver

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/system"
	"platformserver/internal/systemscope"
	"platformserver/platform"
)

// The self-hosting acceptance: a person iterates the system through the same
// submission and acceptance path as any application, the kernel gate refuses
// what must not move, and a build result publishes an immutable release.

type systemKit struct {
	tenant  *Tenant
	keys    *atomic.Int64
	entries []Entry
}

func newSystemTenant(t *testing.T) *systemKit {
	t.Helper()
	seats := []Seat{
		{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{system.ID: system.Builder}}},
		{Subjects: []string{"publisher"}, Member: platform.Member{ID: "publisher", Roles: map[string]string{system.ID: system.Publisher}}},
	}
	tenant, err := NewTenant(systemscope.Tenant, NewConsole(systemscope.Tenant, seats...), NewAgents(systemscope.Tenant), ai.New(systemscope.Tenant), system.New(systemscope.Tenant))
	if err != nil {
		t.Fatal(err)
	}
	kit := systemKit{tenant: tenant, keys: &atomic.Int64{}}
	tenant.Record = func(e Entry) { kit.entries = append(kit.entries, e) }
	return &kit
}

func readChanges(t *testing.T, tenant *Tenant) []system.Change {
	t.Helper()
	member, _ := tenant.Member("publisher")
	out, refusal := tenant.Read(member, "system-changes")
	if refusal != nil {
		t.Fatalf("read system-changes: %v", refusal)
	}
	changes, _ := out.([]system.Change)
	return changes
}

func readReleases(t *testing.T, tenant *Tenant) []system.Release {
	t.Helper()
	member, _ := tenant.Member("publisher")
	out, refusal := tenant.Read(member, "system-releases")
	if refusal != nil {
		t.Fatalf("read system-releases: %v", refusal)
	}
	releases, _ := out.([]system.Release)
	return releases
}

func readActivations(t *testing.T, tenant *Tenant) []system.Activation {
	t.Helper()
	member, _ := tenant.Member("publisher")
	out, refusal := tenant.Read(member, "system-activations")
	if refusal != nil {
		t.Fatalf("read system-activations: %v", refusal)
	}
	activations, _ := out.([]system.Activation)
	return activations
}

func (k *systemKit) changes(t *testing.T) []system.Change   { return readChanges(t, k.tenant) }
func (k *systemKit) releases(t *testing.T) []system.Release { return readReleases(t, k.tenant) }
func (k *systemKit) activations(t *testing.T) []system.Activation {
	return readActivations(t, k.tenant)
}

func (k *systemKit) submit(t *testing.T, who, schema, typ, id string, payload any) *pb.ChangeRecord {
	t.Helper()
	member, ok := k.tenant.Member(who)
	if !ok {
		t.Fatalf("no member %q", who)
	}
	submission := &pb.Submission{TenantId: k.tenant.ID, PrincipalId: who, Authority: system.ID,
		IdempotencyKey: fmt.Sprintf("system-%d", k.keys.Add(1)),
		Target:         &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
	record, err := k.tenant.Submit(member, submission, time.Now())
	if err != nil {
		t.Fatalf("%s refused: %v / %s", schema, err, err.Message)
	}
	return record
}

func (k *systemKit) refuses(t *testing.T, who, schema, typ, id string, payload any) *pb.ChangeRecord {
	t.Helper()
	member, _ := k.tenant.Member(who)
	_, err := k.tenant.Submit(member, &pb.Submission{TenantId: k.tenant.ID, PrincipalId: who, Authority: system.ID,
		IdempotencyKey: fmt.Sprintf("refuse-%d", k.keys.Add(1)),
		Target:         &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, time.Now())
	if err == nil {
		t.Fatalf("%s was accepted", schema)
	}
	return nil
}

func TestSelfHostingProposesApprovesAndPublishes(t *testing.T) {
	kit := newSystemTenant(t)
	kit.submit(t, "builder", system.SchemaFileSave, system.FileType, "ignored", map[string]any{
		"path": "web/packages/build/src/index.tsx", "content": "export const title = \"Platform\";\n",
	})
	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "chg-1", map[string]any{
		"title": "Rename the studio title", "rationale": "The system should name itself Platform.",
		"origin": "ui", "files": []map[string]any{{"path": "web/packages/build/src/index.tsx", "content": "export const title = \"Platform\";\n"}},
	})
	// The proposal freezes its files; approval needs the publisher role.
	kit.refuses(t, "builder", system.SchemaApprove, system.ChangeType, "chg-1", map[string]any{})
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "chg-1", map[string]any{})
	// A failed build leaves the change failed and publishes nothing.
	kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "chg-1", map[string]any{"ok": false, "diagnostics": "go build failed"})
	changes := kit.changes(t)
	if len(changes) != 1 || changes[0].State != system.StateFailed {
		t.Fatalf("want one failed change, got %+v", changes)
	}
	// The same change built again is refused: only an approved change builds.
	kit.refuses(t, "publisher", system.SchemaBuilt, system.ChangeType, "chg-1", map[string]any{"ok": true})

	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "chg-2", map[string]any{
		"title": "Second iteration", "rationale": "Try again.", "origin": "prompt", "prompt": "rename the studio to Platform",
		"files": []map[string]any{{"path": "web/packages/build/src/index.tsx", "content": "export const title = \"Platform\";\n"}},
	})
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "chg-2", map[string]any{})
	kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "chg-2", map[string]any{"ok": true, "binary": "abc", "web": "def", "toolchain": "go1.27.1"})
	releases := kit.releases(t)
	if len(releases) != 1 || releases[0].Version != 1 || releases[0].State != system.StatePublished || releases[0].Change != "chg-2" {
		t.Fatalf("want one published release of chg-2, got %+v", releases)
	}
	kit.submit(t, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0})
	activations := kit.activations(t)
	if len(activations) != 1 || activations[0].Result != system.StateRequested {
		t.Fatalf("want one requested activation, got %+v", activations)
	}
	kit.submit(t, "publisher", system.SchemaFinished, system.ActivationType, "0001", map[string]any{"result": system.StateActive, "evidence": "/healthz ok"})
	releases = kit.releases(t)
	if releases[0].State != system.StateActive {
		t.Fatalf("want the release active, got %s", releases[0].State)
	}
}

func TestSelfHostingRefusesTheKernel(t *testing.T) {
	kit := newSystemTenant(t)
	for _, path := range []string{"contract/spec/k1.md", "capabilities/server/host.go", "capabilities/server/journal/sql.go"} {
		kit.refuses(t, "builder", system.SchemaFileSave, system.FileType, "ignored", map[string]any{"path": path, "content": "x"})
		kit.refuses(t, "builder", system.SchemaPropose, system.ChangeType, "chg", map[string]any{
			"title": "kernel", "rationale": "should not pass", "files": []map[string]any{{"path": path, "content": "x"}},
		})
	}
}

func TestSelfHostingIsOnlyWritableInTheSystemTenant(t *testing.T) {
	seats := []Seat{{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{system.ID: system.Builder}}}}
	other, err := NewTenant("tenant-a", NewConsole("tenant-a", seats...), NewAgents("tenant-a"), ai.New("tenant-a"), system.New("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := other.Member("builder")
	_, refusal := other.Submit(member, &pb.Submission{TenantId: "tenant-a", PrincipalId: "builder", Authority: system.ID,
		IdempotencyKey: "one", Target: &pb.EntityRef{Type: system.FileType, Id: "x"},
		Schema: &pb.SchemaRef{Name: system.SchemaFileSave, Version: 1}, Payload: platform.Raw(map[string]any{"path": "web/a.tsx", "content": "x"})}, time.Now())
	if refusal == nil || !strings.Contains(refusal.Message, "Only the system tenant") {
		t.Fatalf("a business tenant wrote the system: %v", refusal)
	}
}

func TestSelfHostingReplaysWithoutRebuilding(t *testing.T) {
	kit := newSystemTenant(t)
	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "chg", map[string]any{
		"title": "t", "rationale": "r", "files": []map[string]any{{"path": "web/a.tsx", "content": "x"}},
	})
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "chg", map[string]any{})
	entries := kit.entries
	if len(entries) == 0 {
		t.Fatal("no journal entries to replay")
	}
	seats := []Seat{
		{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{system.ID: system.Builder}}},
		{Subjects: []string{"publisher"}, Member: platform.Member{ID: "publisher", Roles: map[string]string{system.ID: system.Publisher}}},
	}
	fresh, err := NewTenant(systemscope.Tenant, NewConsole(systemscope.Tenant, seats...), NewAgents(systemscope.Tenant), ai.New(systemscope.Tenant), system.New(systemscope.Tenant))
	if err != nil {
		t.Fatal(err)
	}
	if err := fresh.Replay(entries); err != nil {
		t.Fatalf("replay: %v", err)
	}
	changes := readChanges(t, fresh)
	if len(changes) != 1 || changes[0].State != system.StateApproved {
		t.Fatalf("replay did not restore the change: %+v", changes)
	}
}

func TestSelfHostingKeepsOneBlobPerContent(t *testing.T) {
	kit := newSystemTenant(t)
	content := "export const title = \"Platform\";\n"
	for _, path := range []string{"web/one.tsx", "web/two.tsx"} {
		kit.submit(t, "builder", system.SchemaFileSave, system.FileType, "ignored", map[string]any{"path": path, "content": content})
	}
	files := readFiles(t, kit.tenant)
	if len(files) != 2 || files[0].Digest != files[1].Digest || files[0].Content != content {
		t.Fatalf("two paths with one content: %+v", files)
	}
	// A tombstone keeps the revision history and frees nothing.
	kit.submit(t, "builder", system.SchemaFileSave, system.FileType, "ignored", map[string]any{"path": "web/one.tsx", "revision": 1, "deleted": true})
	files = readFiles(t, kit.tenant)
	tomb := files[0]
	if tomb.Path != "web/one.tsx" || !tomb.Deleted || tomb.Revision != 2 || tomb.Content != "" {
		t.Fatalf("want web/one.tsx tombstoned at revision 2, got %+v", files)
	}
}

func TestSelfHostingRejectsAStaleRevision(t *testing.T) {
	kit := newSystemTenant(t)
	save := func(revision int, content string) *pb.ChangeRecord {
		return kit.submit(t, "builder", system.SchemaFileSave, system.FileType, "ignored",
			map[string]any{"path": "web/one.tsx", "revision": revision, "content": content})
	}
	save(0, "first")
	kit.refuses(t, "builder", system.SchemaFileSave, system.FileType, "ignored",
		map[string]any{"path": "web/one.tsx", "revision": 0, "content": "second"})
	save(1, "second")
	if files := readFiles(t, kit.tenant); files[0].Content != "second" || files[0].Revision != 2 {
		t.Fatalf("want the second write at revision 2, got %+v", files[0])
	}
}

func TestSelfHostingCancelsAnOpenWindow(t *testing.T) {
	kit := newSystemTenant(t)
	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "chg", map[string]any{
		"title": "t", "rationale": "r", "files": []map[string]any{{"path": "web/a.tsx", "content": "x"}},
	})
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "chg", map[string]any{})
	kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "chg", map[string]any{"ok": true})
	kit.submit(t, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 30})
	kit.submit(t, "publisher", system.SchemaCancel, system.ReleaseType, "0001", map[string]any{"reason": "wait for the next review"})
	if activations := kit.activations(t); activations[0].Result != "aborted" {
		t.Fatalf("want the activation aborted, got %+v", activations[0])
	}
	if releases := kit.releases(t); releases[0].State != system.StatePublished {
		t.Fatalf("want the release back to published, got %s", releases[0].State)
	}
}

func TestSelfHostingAnAgentCannotApproveItsOwnProposal(t *testing.T) {
	kit := newSystemTenant(t)
	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "chg", map[string]any{
		"title": "t", "rationale": "r", "agent": "builder",
		"files": []map[string]any{{"path": "web/a.tsx", "content": "x"}},
	})
	kit.refuses(t, "builder", system.SchemaApprove, system.ChangeType, "chg", map[string]any{})
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "chg", map[string]any{})
}

func readFiles(t *testing.T, tenant *Tenant) []system.FileView {
	t.Helper()
	member, _ := tenant.Member("publisher")
	out, refusal := tenant.Read(member, "system-files")
	if refusal != nil {
		t.Fatalf("read system-files: %v", refusal)
	}
	files, _ := out.([]system.FileView)
	return files
}
