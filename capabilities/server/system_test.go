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

func newSystemTenant(t *testing.T, sources ...*platform.SystemSource) *systemKit {
	t.Helper()
	seats := []Seat{
		{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{system.ID: system.Builder}}},
		{Subjects: []string{"publisher"}, Member: platform.Member{ID: "publisher", Roles: map[string]string{system.ID: system.Publisher}}},
	}
	source := systemTestSource()
	if len(sources) > 0 {
		source = sources[0]
	}
	tenant, err := NewTenant(systemscope.Tenant, NewConsole(systemscope.Tenant, seats...), NewAgents(systemscope.Tenant), ai.New(systemscope.Tenant), system.New(systemscope.Tenant, source))
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
	kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "chg-2", map[string]any{"ok": true, "binary": "abc", "web": "def", "toolchain": "go1.27.1", "sourceDigest": "test-source-built", "manifest": map[string]string{"web/a.tsx": "a"}})
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
	fresh, err := NewTenant(systemscope.Tenant, NewConsole(systemscope.Tenant, seats...), NewAgents(systemscope.Tenant), ai.New(systemscope.Tenant), system.New(systemscope.Tenant, systemTestSource()))
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
	kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "chg", map[string]any{"ok": true, "binary": "abc", "web": "def", "sourceDigest": "test-source-built", "manifest": map[string]string{"web/a.tsx": "a"}})
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
	// Deliberately grant publisher: denial must come from the managed-agent
	// boundary, rather than accidentally proving only a missing role.
	agent, _ := kit.tenant.Member("publisher")
	agent.Agent = true
	agent.ID = "agent:system.architect"
	input := func(schema, typ, id string, payload any) *pb.Submission {
		return &pb.Submission{TenantId: kit.tenant.ID, PrincipalId: agent.ID, Authority: system.ID, IdempotencyKey: fmt.Sprintf("agent-%d", kit.keys.Add(1)), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
	}
	proposal := input(system.SchemaPropose, system.ChangeType, "agent-change", map[string]any{"title": "t", "rationale": "r", "prompt": "Improve the interface", "files": []map[string]any{{"path": "web/a.tsx", "content": "x"}}})
	if _, err := kit.tenant.Submit(agent, proposal, time.Now()); err != nil {
		t.Fatal(err)
	}
	changes := kit.changes(t)
	if len(changes) != 1 || changes[0].Agent != agent.ID || changes[0].Origin != "prompt" {
		t.Fatalf("unattributed managed proposal: %+v", changes)
	}
	if _, err := kit.tenant.Submit(agent, input(system.SchemaApprove, system.ChangeType, "agent-change", map[string]any{}), time.Now()); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("publisher agent approved: %v", err)
	}
	// The ordinary human publisher remains eligible; AI role protection must
	// not introduce an unrelated two-human approval policy.
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "agent-change", map[string]any{})
}

func systemTestSource() *platform.SystemSource {
	return &platform.SystemSource{Digest: "test-source-baseline", Read: func(path string) (string, error) { return "", fmt.Errorf("%s not in the test baseline", path) }}
}

func readFiles(t *testing.T, tenant *Tenant) []system.FileView {
	t.Helper()
	member, _ := tenant.Member("publisher")
	out, refusal := tenant.Read(member, "system-files")
	if refusal != nil {
		t.Fatalf("read system-files: %v", refusal)
	}
	files, _ := out.([]system.FileView)
	for i, file := range files {
		value, err := tenant.Read(member, "system-file", platform.Raw(map[string]any{"path": file.Path}))
		if err != nil {
			t.Fatal(err)
		}
		files[i] = value.(system.FileView)
	}
	return files
}

func (k *systemKit) publishCode(t *testing.T, id string) {
	t.Helper()
	k.submit(t, "builder", system.SchemaPropose, system.ChangeType, id, map[string]any{"title": id, "rationale": "owner lifecycle", "files": []map[string]any{{"path": "web/a.tsx", "content": id}}})
	k.submit(t, "publisher", system.SchemaApprove, system.ChangeType, id, map[string]any{})
	k.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, id, map[string]any{"ok": true, "binary": "bin-" + id, "web": "web-" + id, "sourceDigest": "source-" + id, "manifest": map[string]string{"web/a.tsx": systemscope.Digest([]byte(id))}})
}

func TestSelfHostingWindowExpiryDoesNotReleaseAnUnfinishedActivation(t *testing.T) {
	kit := newSystemTenant(t)
	kit.publishCode(t, "first")
	kit.publishCode(t, "second")
	kit.submit(t, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0})
	member, _ := kit.tenant.Member("publisher")
	call := func(schema, id string, payload any) *pb.Submission {
		return &pb.Submission{TenantId: kit.tenant.ID, PrincipalId: member.ID, Authority: system.ID, IdempotencyKey: fmt.Sprintf("later-%d", kit.keys.Add(1)), Target: &pb.EntityRef{Type: system.ReleaseType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
	}
	later := time.Now().Add(time.Minute)
	for _, input := range []*pb.Submission{call(system.SchemaCancel, "0001", map[string]any{"activationId": "0001"}), call(system.SchemaActivate, "0002", map[string]any{"window": 0})} {
		if _, err := kit.tenant.Submit(member, input, later); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatalf("elapsed window admitted %s: %v", input.Schema.Name, err)
		}
	}
	kit.submit(t, "publisher", system.SchemaFinished, system.ActivationType, "0001", map[string]any{"result": "aborted", "evidence": "probe refused"})
	kit.submit(t, "publisher", system.SchemaActivate, system.ReleaseType, "0002", map[string]any{"window": 30})
}

func TestSelfHostingRepeatedActivationsKeepTheirOwnEvidence(t *testing.T) {
	kit := newSystemTenant(t)
	kit.publishCode(t, "first")
	kit.submit(t, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 30})
	kit.submit(t, "publisher", system.SchemaCancel, system.ReleaseType, "0001", map[string]any{"activationId": "0001", "reason": "review again"})
	kit.submit(t, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 30})
	values := kit.activations(t)
	if len(values) != 2 {
		t.Fatalf("re-activation overwrote history: %+v", values)
	}
	var requested string
	for _, v := range values {
		if v.Result == system.StateRequested {
			requested = v.ID
		} else if v.ID != "0001" || v.Failure != "review again" {
			t.Fatalf("lost cancellation: %+v", v)
		}
	}
	if requested == "0001" || requested == "" {
		t.Fatalf("new activation identity missing: %+v", values)
	}
	kit.refuses(t, "publisher", system.SchemaCancel, system.ReleaseType, "0001", map[string]any{"activationId": "0001"})
	kit.submit(t, "publisher", system.SchemaCancel, system.ReleaseType, "0001", map[string]any{"activationId": requested})
}

func TestSelfHostingColdSourceReadsAndExplicitEmptyEdits(t *testing.T) {
	contents := map[string]string{"web/src/main.tsx": "running code", "contract/spec/K1.md": "frozen code"}
	source := &platform.SystemSource{Digest: "cold-baseline", Files: []platform.SystemSourceFile{{Path: "web/src/main.tsx", Digest: systemscope.Digest([]byte(contents["web/src/main.tsx"])), Editable: true}, {Path: "contract/spec/K1.md", Editable: false}}, Read: func(path string) (string, error) { return contents[path], nil }}
	kit := newSystemTenant(t, source)
	reader, _ := kit.tenant.Member("publisher")
	values, err := kit.tenant.Read(reader, "system-files", platform.Raw(map[string]any{"search": "MAIN", "limit": 1}))
	if err != nil {
		t.Fatal(err)
	}
	listed := values.([]system.FileView)
	if len(listed) != 1 || listed[0].Path != "web/src/main.tsx" || listed[0].Content != "" || listed[0].Revision != 0 {
		t.Fatalf("cold index read: %+v", listed)
	}
	value, err := kit.tenant.Read(reader, "system-file", platform.Raw(map[string]any{"path": "web/src/main.tsx"}))
	if err != nil || value.(system.FileView).Content != "running code" {
		t.Fatalf("cold file read: %+v / %v", value, err)
	}
	kit.submit(t, "builder", system.SchemaFileSave, system.FileType, "file", map[string]any{"path": "web/src/main.tsx", "revision": 0, "content": "saved draft"})
	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "empty", map[string]any{"title": "empty", "rationale": "empty is content", "files": []map[string]any{{"path": "web/src/main.tsx", "revision": 1, "content": ""}}})
	change := kit.changes(t)[0]
	if change.SourceDigest != "cold-baseline" || len(change.Files) != 1 || change.Files[0].Content != "" || change.Files[0].Digest != systemscope.Digest(nil) {
		t.Fatalf("explicit empty was replaced by the draft: %+v", change)
	}
}

func TestSelfHostingStaleBuildClosesWithoutPublishing(t *testing.T) {
	kit := newSystemTenant(t)
	kit.publishCode(t, "first")
	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "stale", map[string]any{"title": "Stale", "rationale": "concurrent review", "files": []map[string]any{{"path": "web/a.tsx", "content": "stale"}}})
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "stale", map[string]any{})
	kit.submit(t, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0})
	kit.submit(t, "publisher", system.SchemaFinished, system.ActivationType, "0001", map[string]any{"result": "active"})
	kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "stale", map[string]any{"ok": true, "binary": "bin", "web": "web", "sourceDigest": "stale-source", "manifest": map[string]string{"web/a.tsx": "stale"}})
	if len(kit.releases(t)) != 1 {
		t.Fatal("stale baseline published")
	}
	for _, c := range kit.changes(t) {
		if c.ID == "stale" && c.State != system.StateFailed {
			t.Fatalf("stale build blocks updater: %+v", c)
		}
	}
}

func TestSelfHostingReviewKeepsTheProposedBaselineThroughRecovery(t *testing.T) {
	contents := map[string]string{"web/main.tsx": "running", "web/old.tsx": "remove me"}
	source := &platform.SystemSource{Digest: "review-baseline", Read: func(path string) (string, error) { return contents[path], nil }}
	for path, content := range contents {
		source.Files = append(source.Files, platform.SystemSourceFile{Path: path, Digest: systemscope.Digest([]byte(content)), Editable: true})
	}
	kit := newSystemTenant(t, source)
	kit.submit(t, "builder", system.SchemaFileSave, system.FileType, "draft", map[string]any{"path": "web/main.tsx", "content": "unrelated draft"})
	kit.submit(t, "builder", system.SchemaPropose, system.ChangeType, "review", map[string]any{
		"title": "Review exact source", "rationale": "Keep the actual baseline, not another writer's draft", "files": []map[string]any{
			{"path": "web/main.tsx", "content": "proposed"}, {"path": "web/old.tsx", "deleted": true}, {"path": "web/new.tsx", "content": ""},
		},
	})
	kit.submit(t, "publisher", system.SchemaApprove, system.ChangeType, "review", map[string]any{})
	kit.submit(t, "builder", system.SchemaFileSave, system.FileType, "draft", map[string]any{"path": "web/main.tsx", "content": "later draft", "revision": 1})
	contents["web/main.tsx"] = "later source"
	fresh := newSystemTenant(t)
	if err := fresh.tenant.Replay(kit.entries); err != nil {
		t.Fatal(err)
	}
	change := fresh.changes(t)[0]
	if change.SourceDigest != "review-baseline" || change.State != system.StateApproved || len(change.Files) != 3 {
		t.Fatalf("lost frozen proposal: %+v", change)
	}
	for _, file := range change.Files {
		if file.Before == nil {
			t.Fatalf("missing before image: %+v", file)
		}
		switch file.Path {
		case "web/main.tsx":
			if file.Before.Content != "running" || file.Before.Digest != systemscope.Digest([]byte("running")) || file.Content != "proposed" {
				t.Fatalf("review drifted to a draft or another source: %+v", file)
			}
		case "web/old.tsx":
			if !file.Deleted || file.Before.Content != "remove me" || file.Before.Digest == "" {
				t.Fatalf("deletion lost its original content: %+v", file)
			}
		case "web/new.tsx":
			if file.Before.Digest != "" || file.Content != "" || file.Digest != systemscope.Digest(nil) {
				t.Fatalf("new empty file lost its identity: %+v", file)
			}
		}
	}
}

func TestSelfHostingReviewDoesNotTrustAnAgentCorrelationLabel(t *testing.T) {
	kit := newSystemTenant(t)
	publisher, _ := kit.tenant.Member("publisher")
	_, err := kit.tenant.Submit(publisher, &pb.Submission{TenantId: kit.tenant.ID, PrincipalId: publisher.ID, Authority: AgentApp, IdempotencyKey: "start-review-run",
		Target: &pb.EntityRef{Type: RunType, Id: "review-run"}, Schema: &pb.SchemaRef{Name: SchemaRunStart, Version: 1},
		Payload: platform.Raw(map[string]any{"agent": "system.architect", "goal": "Review source"})}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	agent := publisher
	agent.ID, agent.Agent = "agent:system.architect", true
	for _, run := range []string{"review-run", "nonexistent-run"} {
		_, err := kit.tenant.Submit(agent, &pb.Submission{TenantId: kit.tenant.ID, PrincipalId: agent.ID, Authority: system.ID, IdempotencyKey: "propose-" + run, CorrelationId: run,
			Target: &pb.EntityRef{Type: system.ChangeType, Id: run}, Schema: &pb.SchemaRef{Name: system.SchemaPropose, Version: 1},
			Payload: platform.Raw(map[string]any{"title": run, "rationale": "Review source", "files": []map[string]any{{"path": "web/new.tsx", "content": "new"}}})}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range kit.changes(t) {
		if change.Run != "" {
			t.Fatalf("a correlation label invented agent provenance: %+v", change)
		}
	}
}
