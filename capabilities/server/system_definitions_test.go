package platformserver

import agentowner "platformserver/engine/agents"

import (
	"fmt"

	"slices"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/core"
	"platformserver/apps/files"
	"platformserver/apps/relations"
	"platformserver/apps/system"
	"platformserver/apps/work"
	"platformserver/studio/build"

	"platformserver/internal/systemscope"
	"platformserver/platform"
)

var systemDefinitionTestKeys atomic.Int64

func definitionOwnerTenant(t *testing.T, id string) *Tenant {
	t.Helper()
	seats := []platform.Seat{
		{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{"system": "builder", "build": "builder", "platform": "admin"}}},
		{Subjects: []string{"publisher"}, Member: platform.Member{ID: "publisher", Roles: map[string]string{"system": "publisher", "build": "builder", "platform": "admin"}}},
	}
	if id != systemscope.Tenant {
		for i := range seats {
			delete(seats[i].Member.Roles, "system")
		}
	}
	apps := []platform.App{newConsole(id, seats...), relations.New(id), ai.New(id), work.New(id), agentowner.New(id), files.New(id), core.New(id), build.New(id)}
	if id == systemscope.Tenant {
		source := &platform.SystemSource{Digest: systemscope.Digest([]byte("running baseline")), Read: func(string) (string, error) { return "baseline", nil }}
		apps = append(apps, system.New(id, source))
	}
	tenant, err := NewTenant(id, apps...)
	if err != nil {
		t.Fatal(err)
	}
	return tenant
}

func definitionDecision(tenant *Tenant, who, schema, typ, id string, payload any) (*pb.ChangeRecord, *kernel.Error) {
	member, _ := tenant.Member(who)
	tenant.mu.Lock()
	owner := tenant.owner["action:"+schema]
	authority := "build"
	version := ""
	if owner != nil {
		authority = owner.Manifest().ID
		if action, ok := owner.Manifest().Actions.Action(schema); ok && action.SchemaVersion != nil {
			version = action.SchemaVersion.Version
		}
	}
	tenant.mu.Unlock()
	submission := &pb.Submission{TenantId: tenant.ID, PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprintf("definitions-current-%d", systemDefinitionTestKeys.Add(1)), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
	if version != "" {
		return tenant.submit(member, submission, time.Now().UTC(), nil, version)
	}
	return tenant.Submit(member, submission, time.Now().UTC())
}
func mustDefinitionDecision(t *testing.T, tenant *Tenant, who, schema, typ, id string, payload any) {
	t.Helper()
	if _, refusal := definitionDecision(tenant, who, schema, typ, id, payload); refusal != nil {
		t.Fatalf("%s: %v / %s", schema, refusal, refusal.Message)
	}
}
func definitionDraft(t *testing.T, tenant *Tenant, title string) {
	t.Helper()
	mustDefinitionDecision(t, tenant, "builder", "build.object.create", "build.object", "global-note", map[string]any{"name": "globalnote", "title": title, "plural": "Global notes", "fields": []map[string]any{{"name": "title", "title": "Title", "type": "text"}}})
}
func definitionOf(t *testing.T, tenant *Tenant) platform.Definition {
	t.Helper()
	member, _ := tenant.Member("builder")
	definitions, refusal := coordinatedRecordRead(tenant, t.Context(), func() ([]platform.Definition, *kernel.Error) { return tenant.Definitions(member), nil })
	if refusal != nil {
		t.Fatal(refusal)
	}
	index := slices.IndexFunc(definitions, func(d platform.Definition) bool {
		return d.Ref == (platform.AssetRef{App: "build", Kind: platform.AssetObject, Name: "build.globalnote"})
	})
	if index < 0 {
		t.Fatal("the shared object is missing")
	}
	return definitions[index]
}

func TestSystemSavedReleaseReviewUsesTheActivationBoundary(t *testing.T) {
	tenant := definitionOwnerTenant(t, "system")
	definitionDraft(t, tenant, "System-only definition")
	member := memberOf(t, tenant, "builder")
	preview, err := tenant.PreviewRelease(member, platform.AssetObject, "global-note")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview: %s %v", preview.Diagnostic, err)
	}
	if _, err := tenant.SaveReleaseCandidate(member, platform.AssetObject, "global-note", preview.CandidateID, "save", txnAt); err != nil {
		t.Fatal(err)
	}
	review, err := tenant.ReviewSavedRelease(member, preview.CandidateID)
	if err != nil || review.CanActivate || review.ActivationDiagnostic != systemReleaseActivationDiagnostic || review.RunningDiagnostic != "" || len(review.Preview.Added) == 0 {
		t.Fatalf("system candidate must explain the approved-change path before activation: %+v %v", review, err)
	}
	if _, err := tenant.ActivateRelease(member, preview.CandidateID, "activate", txnAt); err == nil || err.Error() != review.ActivationDiagnostic {
		t.Fatalf("review and activation disagree: %v", err)
	}
	if tenant.ActiveRelease() != "" {
		t.Fatal("ordinary activation changed the system release")
	}
}

func restoreSystemDefinitions(t *testing.T, owner *Tenant, name string) []Entry {
	t.Helper()
	entries := retainedSystemHistory(t, name)
	if err := owner.Replay(entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestSystemDefinitionsRetainedHistoryRemainsReadOnlyInBusinessTenants(t *testing.T) {
	owner, business := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, owner, business)
	var entries []Entry
	business.Record = func(e Entry) { entries = append(entries, e) }
	restoreSystemDefinitions(t, owner, "definitions-active")
	definition := definitionOf(t, business)
	if definition.Source != "system" || definition.Entity.Title != "Retained system title" {
		t.Fatalf("shared definition=%#v", definition)
	}
	if len(entries) != 0 || business.records.types[build.ObjectType].rows.Len() != 0 {
		t.Fatal("projection wrote system metadata into business owner")
	}
	if _, refusal := definitionDecision(business, "builder", "build.object.create", "build.object", "shadow", map[string]any{"name": "globalnote", "title": "Shadow", "fields": []any{}}); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("shadow accepted: %v", refusal)
	}
	before := len(entries)
	mustDefinitionDecision(t, business, "builder", "build.globalnote.create", "build.globalnote", "note-one", map[string]any{"title": "Local data"})
	if len(entries) != before+1 {
		t.Fatal("ordinary business write did not retain its original owner")
	}
}

func TestSystemDefinitionsRetainedStorageDeltaKeepsOriginalInstallations(t *testing.T) {
	entries := retainedSystemHistory(t, "definitions-active")
	delta, err := acceptedStorageDelta(entries[len(entries)-1].Body)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range delta.Replacements {
		if row.Type == "build.object" && row.ID == "global-note" {
			found = true
		}
	}
	if !found {
		t.Fatal("retained SQL delta omitted original definition installation")
	}
}

func TestSystemDefinitionsRetainedRecoveryPrecedesBusinessRecords(t *testing.T) {
	owner, business := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, owner, business)
	ownerEntries := restoreSystemDefinitions(t, owner, "definitions-active")
	var businessEntries []Entry
	business.Record = func(e Entry) { businessEntries = append(businessEntries, e) }
	mustDefinitionDecision(t, business, "builder", "build.globalnote.create", "build.globalnote", "local-note", map[string]any{"title": "Kept by the business owner"})
	freshOwner, freshBusiness := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, freshOwner, freshBusiness)
	if err := freshOwner.Replay(ownerEntries); err != nil {
		t.Fatal(err)
	}
	if err := freshBusiness.Replay(businessEntries); err != nil {
		t.Fatal(err)
	}
	if freshBusiness.records.types["build.globalnote"].rows.Get("local-note") == nil || freshBusiness.records.types["build.object"].rows.Len() != 0 {
		t.Fatal("replay lost business data or copied system metadata")
	}
	ownerState, _, err := owner.Snapshot(func() int64 { return int64(len(ownerEntries)) })
	if err != nil {
		t.Fatal(err)
	}
	businessState, _, err := business.Snapshot(func() int64 { return int64(len(businessEntries)) })
	if err != nil {
		t.Fatal(err)
	}
	snapshotOwner, snapshotBusiness := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, snapshotOwner, snapshotBusiness)
	if err := snapshotOwner.Restore(ownerState); err != nil {
		t.Fatal(err)
	}
	if err := snapshotBusiness.Restore(businessState); err != nil {
		t.Fatal(err)
	}
	if snapshotBusiness.records.types["build.globalnote"].rows.Get("local-note") == nil || snapshotBusiness.records.types["build.object"].rows.Len() != 0 {
		t.Fatal("checkpoint lost business data or copied system metadata")
	}
}

func TestSystemDefinitionsFreezeRefusesNewCandidatesAndUnrelatedPreparation(t *testing.T) {
	owner := definitionOwnerTenant(t, "system")
	definitionDraft(t, owner, "New draft")
	if _, refusal := definitionDecision(owner, "builder", system.SchemaPropose, system.ChangeType, "new", map[string]any{"title": "New", "rationale": "New", "assets": []platform.AssetRef{{App: "build", Kind: platform.AssetObject, Name: "build.globalnote"}}}); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("definition-only proposal accepted: %v", refusal)
	}
}

func TestSystemDefinitionsFreezeReconcilesOnlyOriginalMixedActivation(t *testing.T) {
	owner := definitionOwnerTenant(t, "system")
	restoreSystemDefinitions(t, owner, "mixed-requested")
	publisher, _ := owner.Member("publisher")
	caller := platform.As(system.ID, publisher)
	if _, refusal := (hostView{t: owner}).FreezeSystemDefinitions(caller, nil, time.Now()); refusal == nil {
		t.Fatal("direct freeze capability remained writable")
	}
	var original []byte
	for _, release := range readReleases(t, owner) {
		if release.Version == 1 {
			original = release.Candidate
		}
	}
	if len(original) == 0 {
		t.Fatal("retained candidate missing")
	}
	for _, key := range []string{"system:invented", "new:0001"} {
		if _, refusal := (hostView{t: owner}).PrepareSystemDefinitions(caller, original, key, time.Now()); refusal == nil {
			t.Fatalf("unrelated activation %s prepared definitions", key)
		}
	}
	if _, refusal := (hostView{t: owner}).PrepareSystemDefinitions(caller, platform.Raw(platform.SystemDefinitionCandidate{ID: "invented", Bytes: []byte("invented")}), "system:0001", time.Now()); refusal == nil {
		t.Fatal("retained activation accepted a different candidate")
	}
	if _, exists := owner.records.types["build.globalnote"]; exists {
		t.Fatal("requested mixed release installed before original outcome")
	}
	mustDefinitionDecision(t, owner, "publisher", system.SchemaFinished, system.ActivationType, "0001", map[string]any{"result": system.StateActive, "evidence": "static maintenance verified original healthy process"})
	if _, exists := owner.records.types["build.globalnote"]; !exists {
		t.Fatal("original successful outcome lost its frozen installation")
	}
	fresh := definitionOwnerTenant(t, "system")
	restoreSystemDefinitions(t, fresh, "mixed-active")
	if _, exists := fresh.records.types["build.globalnote"]; !exists {
		t.Fatal("accepted mixed installation did not replay")
	}
}
