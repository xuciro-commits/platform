package platformserver

import agentowner "platformserver/engine/agents"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"os"

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
	seats := []platform.Seat{
		{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{system.ID: system.Builder}}},
		{Subjects: []string{"publisher"}, Member: platform.Member{ID: "publisher", Roles: map[string]string{system.ID: system.Publisher}}},
	}
	source := systemTestSource()
	if len(sources) > 0 {
		source = sources[0]
	}
	tenant, err := NewTenant(systemscope.Tenant, newConsole(systemscope.Tenant, seats...), agentowner.New(systemscope.Tenant), ai.New(systemscope.Tenant), system.New(systemscope.Tenant, source))
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
		IdempotencyKey: fmt.Sprintf("maintenance-%d", k.keys.Add(1)),
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

// Captured from the original owner before the freeze. These are actual accepted
// images, not reconstructed decisions or a test-only switch reopening writes.
func retainedSystemHistory(t *testing.T, name string) []Entry {
	t.Helper()
	raw, err := os.ReadFile("testdata/system-history.json")
	if err != nil {
		t.Fatal(err)
	}
	var history map[string][]Entry
	if err := json.Unmarshal(raw, &history); err != nil {
		t.Fatal(err)
	}
	var entries []Entry
	switch name {
	case "code-active":
		entries = history["code"][:8]
	case "code-requested":
		entries = history["code"][:12]
	case "code-approved":
		entries = history["code"]
	case "definitions-active":
		entries = history["definitions"]
	case "mixed-requested":
		entries = history["mixed"][:5]
	case "mixed-active":
		entries = history["mixed"]
	}
	if len(entries) == 0 {
		t.Fatalf("missing retained history %s", name)
	}
	return entries
}

func TestSystemFreezeRefusesNewWorkForHumansAndAgents(t *testing.T) {
	for _, agent := range []bool{false, true} {
		kit := newSystemTenant(t)
		member, _ := kit.tenant.Member("publisher")
		member.Agent = agent
		if agent {
			member.ID = "agent:system.architect"
		}
		for _, test := range []struct {
			schema, typ string
			payload     any
		}{
			{system.SchemaFileSave, system.FileType, map[string]any{"path": "web/a.tsx", "content": "new"}},
			{system.SchemaPropose, system.ChangeType, map[string]any{"title": "new", "rationale": "new", "files": []any{map[string]any{"path": "web/a.tsx", "content": "new"}}}},
			{system.SchemaApprove, system.ChangeType, map[string]any{}},
			{system.SchemaActivate, system.ReleaseType, map[string]any{"window": 0}},
			{system.SchemaDeactivate, system.ReleaseType, map[string]any{"window": 0}},
		} {
			input := &pb.Submission{TenantId: kit.tenant.ID, PrincipalId: member.ID, Authority: system.ID, IdempotencyKey: "frozen:" + test.schema, Target: &pb.EntityRef{Type: test.typ, Id: "new"}, Schema: &pb.SchemaRef{Name: test.schema, Version: 1}, Payload: platform.Raw(test.payload)}
			if _, refusal := kit.tenant.Submit(member, input, time.Now()); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED || !strings.Contains(refusal.Message, "frozen") {
				t.Fatalf("agent=%t %s: %v", agent, test.schema, refusal)
			}
		}
		if len(kit.changes(t)) != 0 || len(kit.releases(t)) != 0 || len(kit.activations(t)) != 0 || len(readFiles(t, kit.tenant)) != 0 {
			t.Fatal("frozen work changed source or deployment records")
		}
	}
}

func TestSystemFreezeRetainsOriginalReceiptsAndHistory(t *testing.T) {
	entries := retainedSystemHistory(t, "code-active")
	original := append([]byte(nil), entries[0].Body...)
	kit := newSystemTenant(t)
	if err := kit.tenant.Replay(entries); err != nil {
		t.Fatal(err)
	}
	files := readFiles(t, kit.tenant)
	if len(files) != 2 || !files[0].Deleted || files[0].Revision != 2 || files[0].Content != "" || files[1].Content != "shared historical source" || files[1].Digest != systemscope.Digest([]byte("shared historical source")) || kit.tenant.records.Type(system.BlobType).Rows().Len() != 1 {
		t.Fatalf("lost retained source/blob/tombstone: %+v", files)
	}
	for _, f := range files {
		if f.Editable {
			t.Fatal("retained source advertised as editable")
		}
	}
	if releases := kit.releases(t); len(releases) != 1 || releases[0].State != system.StateActive {
		t.Fatalf("lost active release: %+v", releases)
	}
	var envelope struct{ Receipt json.RawMessage }
	if err := json.Unmarshal(entries[0].Body, &envelope); err != nil {
		t.Fatal(err)
	}
	receipt := new(pb.ChangeRecord)
	if err := protojson.Unmarshal(envelope.Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	member, _ := kit.tenant.Member("builder")
	answer, refusal := kit.tenant.Submit(member, receipt.Submission, time.Now())
	if refusal != nil || !proto.Equal(answer, receipt) {
		t.Fatalf("original accepted file receipt unavailable: %+v %v", answer, refusal)
	}
	if !bytes.Equal(entries[0].Body, original) || len(kit.entries) != 0 {
		t.Fatal("replay or retry rewrote history")
	}
	state, _, err := kit.tenant.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	fresh := newSystemTenant(t)
	if err := fresh.tenant.Restore(state); err != nil {
		t.Fatal(err)
	}
	if got := readFiles(t, fresh.tenant); len(got) != 2 || got[1].Content != "shared historical source" || got[0].Revision != 2 {
		t.Fatalf("checkpoint lost retained files: %+v", got)
	}
	if releases := fresh.releases(t); len(releases) != 1 || releases[0].State != system.StateActive {
		t.Fatal("checkpoint lost active release")
	}
}

func TestSystemFreezeReconcilesOriginalPendingOutcomes(t *testing.T) {
	for _, result := range []string{system.StateActive, "aborted"} {
		kit := newSystemTenant(t)
		if err := kit.tenant.Replay(retainedSystemHistory(t, "code-approved")); err != nil {
			t.Fatal(err)
		}
		kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "unfinished", map[string]any{"ok": false, "diagnostics": "not started before freeze"})
		if c := kit.changes(t)[0]; c.ID != "unfinished" || c.State != system.StateFailed {
			t.Fatalf("approved work was not closed: %+v", c)
		}
		kit.submit(t, "publisher", system.SchemaFinished, system.ActivationType, "0002", map[string]any{"result": result, "evidence": "static maintenance checked original process"})
		found := false
		for _, a := range kit.activations(t) {
			if a.ID == "0002" {
				found = true
				if a.Result != result {
					t.Fatalf("outcome=%+v", a)
				}
			}
		}
		if !found {
			t.Fatal("lost original activation identity")
		}
		kit.refuses(t, "publisher", system.SchemaFinished, system.ActivationType, "invented", map[string]any{"result": system.StateActive})
	}
}

func TestSystemFreezeAllowsRetainedBuildReportWithoutStartingActivation(t *testing.T) {
	kit := newSystemTenant(t)
	if err := kit.tenant.Replay(retainedSystemHistory(t, "code-approved")); err != nil {
		t.Fatal(err)
	}
	kit.submit(t, "publisher", system.SchemaBuilt, system.ChangeType, "unfinished", map[string]any{"ok": true, "binary": "retained-build", "web": "retained-web", "sourceDigest": "retained-source", "manifest": map[string]string{"web/a.tsx": "retained"}})
	if r := kit.releases(t); len(r) != 3 || r[0].State != system.StatePublished || r[0].Change != "unfinished" {
		t.Fatalf("old build result was not retained: %+v", r)
	}
	kit.refuses(t, "publisher", system.SchemaBuilt, system.ChangeType, "invented", map[string]any{"ok": true})
	kit.refuses(t, "publisher", system.SchemaActivate, system.ReleaseType, "0003", map[string]any{})
	if len(kit.activations(t)) != 2 {
		t.Fatal("build report started another activation")
	}
}

func TestSystemFreezeColdSourceAndAssistantAreReadOnly(t *testing.T) {
	source := &platform.SystemSource{Files: []platform.SystemSourceFile{{Path: "web/main.tsx", Editable: true}}, Read: func(string) (string, error) { return "retained source", nil }}
	kit := newSystemTenant(t, source)
	files := readFiles(t, kit.tenant)
	if len(files) != 1 || files[0].Content != "retained source" || files[0].Editable {
		t.Fatalf("cold source: %+v", files)
	}
	definition := kit.tenant.app(platform.AgentApp).(platform.AgentRuntime).AgentEvaluationRuntime().Definition("system.architect")
	if definition == nil {
		t.Fatal("retained history assistant unavailable")
	}
	for _, tool := range definition.Toolset {
		if tool.Kind == "action" || tool.Kind == "protocol" {
			t.Fatalf("frozen assistant has write tool %+v", tool)
		}
	}
}
