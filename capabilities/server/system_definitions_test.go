package platformserver

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/apps/files"
	"platformserver/apps/relations"
	"platformserver/apps/system"
	"platformserver/apps/work"
	"platformserver/internal/construction"
	"platformserver/internal/systemscope"
	"platformserver/platform"
)

var systemDefinitionTestKeys atomic.Int64

func definitionOwnerTenant(t *testing.T, id string) *Tenant {
	t.Helper()
	seats := []Seat{
		{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{"system": "builder", "build": "builder", "platform": "admin"}}},
		{Subjects: []string{"publisher"}, Member: platform.Member{ID: "publisher", Roles: map[string]string{"system": "publisher", "build": "builder", "platform": "admin"}}},
	}
	if id != systemscope.Tenant {
		for i := range seats {
			delete(seats[i].Member.Roles, "system")
		}
	}
	apps := []platform.App{NewConsole(id, seats...), relations.New(id), ai.New(id), work.New(id), NewAgents(id), files.New(id), core.New(id), build.New(id)}
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
	submission := &pb.Submission{TenantId: tenant.ID, PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprintf("definitions-%d", systemDefinitionTestKeys.Add(1)), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}
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
func proposeDefinition(t *testing.T, tenant *Tenant, id string, files []map[string]any) {
	t.Helper()
	mustDefinitionDecision(t, tenant, "builder", system.SchemaPropose, system.ChangeType, id, map[string]any{"title": "System object", "rationale": "Share an owner-built object through the system release", "assets": []platform.AssetRef{{App: "build", Kind: platform.AssetObject, Name: "build.globalnote"}}, "files": files})
	mustDefinitionDecision(t, tenant, "publisher", system.SchemaApprove, system.ChangeType, id, map[string]any{})
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

func TestSystemDefinitionsFreezeThenInstallAndRemainReadOnlyInBusinessTenants(t *testing.T) {
	owner, business := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, owner, business)
	var businessEntries []Entry
	business.Record = func(entry Entry) { businessEntries = append(businessEntries, entry) }
	definitionDraft(t, owner, "Original title")
	proposeDefinition(t, owner, "definition-one", nil)
	// A later draft edit must not mutate the approved canonical candidate.
	mustDefinitionDecision(t, owner, "builder", "build.object.edit", "build.object", "global-note", map[string]any{"title": "Unapproved later title"})
	mustDefinitionDecision(t, owner, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0})
	definition := definitionOf(t, business)
	if definition.Source != "system" || definition.Entity == nil || definition.Entity.Title != "Original title" {
		t.Fatalf("shared definition = %#v", definition)
	}
	if len(businessEntries) != 0 {
		t.Fatal("runtime projection wrote into the business journal")
	}
	if metadata := business.records.types[construction.ObjectType]; metadata.rows.Len() != 0 {
		t.Fatal("system metadata rows were copied into the business tenant")
	}
	if _, refusal := definitionDecision(business, "builder", "build.object.create", "build.object", "shadow", map[string]any{"name": "globalnote", "title": "Shadow", "fields": []any{}}); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("local shadow was not refused: %v", refusal)
	}
	before := len(businessEntries) // the refused shadow has its own durable refusal
	mustDefinitionDecision(t, business, "builder", "build.globalnote.create", "build.globalnote", "note-one", map[string]any{"title": "Local data"})
	if len(businessEntries) != before+1 {
		t.Fatalf("business decision added %d journal entries, want one", len(businessEntries)-before)
	}
}

func TestMixedSystemChangeInstallsDefinitionsOnlyAfterSuccessfulFinished(t *testing.T) {
	owner := definitionOwnerTenant(t, "system")
	definitionDraft(t, owner, "Mixed version")
	proposeDefinition(t, owner, "mixed", []map[string]any{{"path": "docs/system-copy.md", "content": "new source"}})
	if _, exists := owner.records.types["build.globalnote"]; exists {
		t.Fatal("approval installed a mixed definition before building")
	}
	manifest := map[string]string{"docs/system-copy.md": systemscope.Digest([]byte("new source"))}
	mustDefinitionDecision(t, owner, "publisher", system.SchemaBuilt, system.ChangeType, "mixed", map[string]any{"ok": true, "binary": "binary-proof", "web": "web-proof", "sourceDigest": systemscope.Manifest(manifest), "manifest": manifest})
	mustDefinitionDecision(t, owner, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0})
	if _, exists := owner.records.types["build.globalnote"]; exists {
		t.Fatal("the activation request installed definitions before the updater finished")
	}
	mustDefinitionDecision(t, owner, "publisher", system.SchemaFinished, system.ActivationType, "0001", map[string]any{"result": system.StateActive, "evidence": "new process healthy"})
	if _, exists := owner.records.types["build.globalnote"]; !exists {
		t.Fatal("successful finished did not install the frozen definition")
	}
}

func TestSystemDefinitionActivationStorageDeltaContainsOriginalReleaseInstallations(t *testing.T) {
	owner := definitionOwnerTenant(t, "system")
	var entries []Entry
	owner.Record = func(entry Entry) { entries = append(entries, entry) }
	definitionDraft(t, owner, "Stored title")
	proposeDefinition(t, owner, "stored", nil)
	mustDefinitionDecision(t, owner, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0})
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
		t.Fatal("SQL delta omitted the original definition owner's accepted publication replacement")
	}
}

func TestSystemDefinitionsRecoverBeforeBusinessRecordsWithoutCopyingMetadata(t *testing.T) {
	owner, business := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, owner, business)
	var ownerEntries, businessEntries []Entry
	owner.Record = func(entry Entry) { ownerEntries = append(ownerEntries, entry) }
	business.Record = func(entry Entry) { businessEntries = append(businessEntries, entry) }
	definitionDraft(t, owner, "Recoverable title")
	proposeDefinition(t, owner, "recovery", nil)
	mustDefinitionDecision(t, owner, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0})
	mustDefinitionDecision(t, business, "builder", "build.globalnote.create", "build.globalnote", "local-note", map[string]any{"title": "Kept by the business owner"})
	freshOwner, freshBusiness := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, freshOwner, freshBusiness)
	if err := freshOwner.Replay(ownerEntries); err != nil {
		t.Fatal(err)
	}
	if err := freshBusiness.Replay(businessEntries); err != nil {
		t.Fatal(err)
	}
	if freshBusiness.records.types["build.globalnote"].rows.Get("local-note") == nil {
		t.Fatal("business replay lost its own record")
	}
	if freshBusiness.records.types["build.object"].rows.Len() != 0 {
		t.Fatal("business recovery copied system metadata")
	}
	if got := definitionOf(t, freshBusiness); got.Source != "system" || got.Entity.Title != "Recoverable title" {
		t.Fatalf("recovered definition=%#v", got)
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
	if snapshotBusiness.records.types["build.globalnote"].rows.Get("local-note") == nil {
		t.Fatal("business checkpoint lost its own record")
	}
	if snapshotBusiness.records.types["build.object"].rows.Len() != 0 {
		t.Fatal("business checkpoint copied system metadata")
	}
}
func TestSystemDefinitionsRefuseAnExistingBusinessOwnerConflictBeforeActivation(t *testing.T) {
	owner, business := definitionOwnerTenant(t, "system"), definitionOwnerTenant(t, "business")
	NewHost(nil, owner, business)
	definitionDraft(t, business, "Business-owned title")
	mustDefinitionDecision(t, business, "builder", "build.object.publish", "build.object", "global-note", map[string]any{})
	definitionDraft(t, owner, "Shared title")
	proposeDefinition(t, owner, "conflict", nil)
	if _, refusal := definitionDecision(owner, "publisher", system.SchemaActivate, system.ReleaseType, "0001", map[string]any{"window": 0}); refusal == nil {
		t.Fatal("a conflicting global definition was activated")
	}
	if got := definitionOf(t, business); got.Source == "system" || got.Entity.Title != "Business-owned title" {
		t.Fatalf("business owner was overwritten: %#v", got)
	}
}
