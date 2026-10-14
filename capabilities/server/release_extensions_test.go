package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

// This is a native package upgrade, not an edit of the live record registry.
// Reuse the real Core declarations and seeds with a later material model;
// generated decisions, authority, persistence and recovery remain the host's.
type extensionCorePackage struct {
	*core.Core
	ledger   *platform.Ledger
	entities []platform.Entity
	version  string
}

func newExtensionCorePackage(tenant, version string, added string, incompatible bool) *extensionCorePackage {
	entities := core.Entities()
	for i := range entities {
		if entities[i].Type != core.MaterialType {
			continue
		}
		typ := reflect.TypeOf(entities[i].Model)
		fields := make([]reflect.StructField, typ.NumField())
		for j := range fields {
			fields[j] = typ.Field(j)
		}
		if incompatible {
			for j := range fields {
				if fields[j].Name == "Weight" {
					fields[j].Type = reflect.TypeFor[string]()
				}
			}
		}
		if added != "" {
			fields = append(fields, reflect.StructField{Name: "PackageField", Type: reflect.TypeFor[string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" title:"Package field"`, added))})
		}
		if added != "" || incompatible {
			entities[i].Model = reflect.New(reflect.StructOf(fields)).Elem().Interface()
		}
	}
	var actions []platform.Action
	var types []string
	for _, entity := range entities {
		types = append(types, entity.Type)
		actions = append(actions, platform.EntityActions(entity)...)
	}
	return &extensionCorePackage{Core: core.New(tenant), ledger: platform.NewLedger(tenant, core.ID, platform.NewCatalog(actions...), types...), entities: entities, version: version}
}

func (a *extensionCorePackage) Manifest() platform.Manifest {
	m := a.Core.Manifest()
	m.Version = a.version
	m.Entities = a.entities
	m.Actions = a.ledger.Catalog
	return m
}
func (a *extensionCorePackage) AcceptedLedger() *platform.Ledger { return a.ledger }
func (a *extensionCorePackage) Declarations() []*pb.AuthorityDeclaration {
	return a.ledger.Declarations()
}
func (a *extensionCorePackage) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }
func (a *extensionCorePackage) Restore(raw json.RawMessage) error  { return a.ledger.Restore(raw) }
func (a *extensionCorePackage) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	r, refusal, _ := a.ledger.Generated(c, s, at, nil, a.entities...)
	return r, refusal
}

func extensionTenant(t *testing.T, id, version, added string, incompatible bool) *Tenant {
	t.Helper()
	return composeTenant(t, id, []Seat{seatOf("builder", "build:builder", "core:steward"), seatOf("user", "build:user")}, build.New(id), newExtensionCorePackage(id, version, added, incompatible))
}

func extensionObjectFields() []build.Field {
	return []build.Field{{Name: "base", Title: "Material", Type: "reference", Ref: core.MaterialType, Required: true, Inverse: "materialextra"},
		{Name: "customergrade", Title: "Customer grade", Type: "text", Search: true}, {Name: "inspectiondue", Title: "Inspection due", Type: "date"}}
}

func extensionFixture(t *testing.T, live *Tenant) string {
	t.Helper()
	publishObject(t, live, "builder", "extension", map[string]any{"name": "materialextra", "title": "Material extension", "extends": core.MaterialType, "fields": extensionObjectFields(),
		"states":  []build.State{{Name: "open", Title: "Open"}, {Name: "reviewed", Title: "Reviewed"}},
		"actions": []build.Action{{Name: "review", Title: "Review", From: []string{"open"}, To: "reviewed"}}}, txnAt)
	from := saveExtensionDraft(t, live, "extension", "source")
	if _, err := live.ActivateRelease(memberOf(t, live, "builder"), from, "source-activate", txnAt); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("M%d", i)
		decide(t, live, "builder", core.ID, core.MaterialType+".create", core.MaterialType, id, map[string]any{"name": id, "code": id, "kind": "raw", "baseUom": "uom-pce"}, txnAt)
		decide(t, live, "user", build.ID, "build.materialextra.create", "build.materialextra", id, map[string]any{"base": id, "customergrade": fmt.Sprintf("Grade %d", i), "inspectiondue": "2027-01-01"}, txnAt)
	}
	return from
}

func saveExtensionDraft(t *testing.T, live *Tenant, draft, key string) string {
	t.Helper()
	m := memberOf(t, live, "builder")
	p, err := live.PreviewRelease(m, platform.AssetObject, draft)
	if err != nil || p.Diagnostic != "" {
		t.Fatalf("preview: %+v %v", p, err)
	}
	id, err := live.SaveReleaseCandidate(m, platform.AssetObject, draft, p.CandidateID, key, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func extensionImages(t *testing.T, live *Tenant) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for id, row := range live.records.types["build.materialextra"].rows.all() {
		raw, err := row.image()
		if err != nil {
			t.Fatal(err)
		}
		out[id] = raw
	}
	return out
}

func TestCustomerExtensionsSurviveNativePackageUpgradeAndRecovery(t *testing.T) {
	for _, backend := range journalsUnderTest(t) {
		t.Run(backend.name, func(t *testing.T) {
			id := fmt.Sprintf("extension-upgrade-%d", time.Now().UnixNano())
			defer backend.cleanup(t, id)
			journal := backend.open(t, id)
			if _, err := journal.Entries(context.Background(), id, 0); err != nil {
				t.Fatal(err)
			}
			persist := func(e Entry, key, hash string) ([]byte, error) {
				return journal.AppendAccepted(context.Background(), id, e, key, hash)
			}
			live := extensionTenant(t, id, "1", "", false)
			live.AcceptResult = persist
			from := extensionFixture(t, live)
			before := extensionImages(t, live)
			original := slices.Clone(live.releases.raw(from))
			metadata := live.records.types[build.ObjectType].rows.get("extension").value.Interface().(build.Object)
			if metadata.ExtensionSource == nil || metadata.ExtensionSource.Base.SourceVersion != "1" {
				t.Fatal("publication did not capture the original Core package")
			}
			position := journal.Position(id)
			journal.Close()
			journal = backend.reopen(t, id)
			defer journal.Close()
			entries, err := journal.Entries(context.Background(), id, 0)
			if err != nil {
				t.Fatal(err)
			}
			live = extensionTenant(t, id, "2", "packagecode", false)
			if err := live.Replay(entries); err != nil {
				t.Fatalf("native package upgrade cannot recover: %v", err)
			}
			live.AcceptResult = persist
			for rowID, old := range before {
				if !bytes.Equal(old, extensionImages(t, live)[rowID]) {
					t.Fatal("upgrading Core replaced extension customer bytes")
				}
			}
			if journal.Position(id) != position || !bytes.Equal(original, live.releases.raw(from)) {
				t.Fatal("recovery rewrote history or the old candidate")
			}
			// This candidate omits the extension. It must still validate and retain it.
			decide(t, live, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "consumer", map[string]any{"name": "consumer", "title": "Material consumer", "fields": []build.Field{{Name: "material", Title: "Material", Type: "reference", Ref: core.MaterialType}}}, txnAt)
			to := saveExtensionDraft(t, live, "consumer", "v2")
			review, err := live.ReviewSavedRelease(memberOf(t, live, "builder"), to)
			if err != nil || !review.CanActivate || len(review.Extensions) != 1 || review.Extensions[0].Base.SourceVersion != "1" || review.Extensions[0].TargetVersion != "2" || review.Extensions[0].Issue != "" {
				t.Fatalf("extension upgrade review: %+v %v", review, err)
			}
			live.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
			if _, err := live.ActivateRelease(memberOf(t, live, "builder"), to, "v2-activate", txnAt); err == nil {
				t.Fatal("failed append activated the package upgrade")
			}
			if live.releases.active != from {
				t.Fatal("failed append changed activation")
			}
			live.AcceptResult = persist
			if _, err := live.ActivateRelease(memberOf(t, live, "builder"), to, "v2-activate", txnAt); err != nil {
				t.Fatal(err)
			}
			for rowID, old := range before {
				if !bytes.Equal(old, extensionImages(t, live)[rowID]) {
					t.Fatal("activation replaced extension data outside its closure")
				}
			}
			decide(t, live, "user", build.ID, "build.materialextra.review", "build.materialextra", "M0", map[string]any{}, txnAt)
			decide(t, live, "builder", core.ID, core.MaterialType+".edit", core.MaterialType, "M0", map[string]any{"name": "Upgraded material", "packagecode": "P-2"}, txnAt)
			snapshot, pos, err := live.Snapshot(func() int64 { return journal.Position(id) })
			if err != nil {
				t.Fatal(err)
			}
			restored := extensionTenant(t, id, "2", "packagecode", false)
			if err := restored.Restore(snapshot); err != nil {
				t.Fatal(err)
			}
			if pos != journal.Position(id) {
				t.Fatal("snapshot position differs")
			}
			entries, err = journal.Entries(context.Background(), id, 0)
			if err != nil {
				t.Fatal(err)
			}
			final := extensionTenant(t, id, "2", "packagecode", false)
			if err := final.Replay(entries); err != nil {
				t.Fatal(err)
			}
			for rowID, expected := range extensionImages(t, live) {
				if !bytes.Equal(expected, extensionImages(t, restored)[rowID]) || !bytes.Equal(expected, extensionImages(t, final)[rowID]) {
					t.Fatal("recovery changed extension facts or actions")
				}
			}
			if final.records.types[build.ObjectType].rows.get("extension").value.Interface().(build.Object).ExtensionSource.Base.SourceVersion != "1" {
				t.Fatal("base upgrade silently rebound the published extension")
			}
		})
	}
}

func TestCustomerExtensionConflictRequiresAnExplicitMigration(t *testing.T) {
	const id = "extension-conflict"
	live := extensionTenant(t, id, "1", "", false)
	var entries []Entry
	persist := func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	live.AcceptResult = persist
	from := extensionFixture(t, live)
	before := extensionImages(t, live)
	upgraded := extensionTenant(t, id, "3", "customergrade", false)
	if err := upgraded.Replay(entries); err != nil {
		t.Fatalf("conflict should pause upgrades, not destroy recovery: %v", err)
	}
	upgraded.AcceptResult = persist
	if got := refuse(t, upgraded, "user", build.ID, "build.materialextra.edit", "build.materialextra", "M0", map[string]any{"customergrade": "Overwrite"}, txnAt); !strings.Contains(got, "conflicts") {
		t.Fatalf("incompatible extension wrote new data: %s", got)
	}
	// A candidate for a different app cannot escape the conflict.
	decide(t, upgraded, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "consumer", map[string]any{"name": "consumer", "title": "Material consumer", "fields": []build.Field{{Name: "material", Title: "Material", Type: "reference", Ref: core.MaterialType}}}, txnAt)
	to := saveExtensionDraft(t, upgraded, "consumer", "conflicting-package")
	review, err := upgraded.ReviewSavedRelease(memberOf(t, upgraded, "builder"), to)
	if err != nil || review.CanActivate || len(review.Extensions) != 1 || !strings.Contains(review.Extensions[0].Issue, "customergrade") {
		t.Fatalf("conflict not reported: %+v %v", review, err)
	}
	if _, err := upgraded.ActivateRelease(memberOf(t, upgraded, "builder"), to, "conflicting-activate", txnAt); err == nil || upgraded.releases.active != from {
		t.Fatal("base package silently overrode the extension")
	}
	// Preserve the historical name as a read alias, move the customer value to
	// an explicit new field, then run the same original migration executor.
	fields := extensionObjectFields()
	fields[1].ReadOnly = true
	fields[1].AliasOf = "localgrade"
	fields = append(fields, build.Field{Name: "localgrade", Title: "Local customer grade", Type: "text", Search: true})
	decide(t, upgraded, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, "extension", map[string]any{"fields": fields}, txnAt)
	to = saveExtensionDraft(t, upgraded, "extension", "resolved-package")
	plan := platform.MigrationPlan{From: from, To: to, Scope: platform.MigrationScope{Type: "build.materialextra"}, Steps: []platform.MigrateStep{{Kind: "rename", Types: []string{"build.materialextra"}, Batch: 2, Fields: []platform.MigrateField{{From: "customergrade", To: "localgrade"}}}}}
	m := memberOf(t, upgraded, "builder")
	migration, err := upgraded.ReviewMigrationPlan(m, plan, txnAt)
	if err != nil {
		t.Fatal(err)
	}
	decide(t, upgraded, "builder", build.ID, "build.migration.start", platform.MigrationRunType, "M", map[string]any{"plan": plan, "review": migration.ID, "reviewedAt": migration.At}, txnAt)
	decide(t, upgraded, "builder", build.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	if _, err := upgraded.ActivateRelease(m, to, "early", txnAt); err == nil {
		t.Fatal("partial conflict resolution activated")
	}
	recovered := extensionTenant(t, id, "3", "customergrade", false)
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	recovered.AcceptResult = persist
	decide(t, recovered, "builder", build.ID, "build.migration.advance", platform.MigrationRunType, "M", map[string]any{}, txnAt)
	if _, err := recovered.ActivateRelease(memberOf(t, recovered, "builder"), to, "resolved", txnAt); err != nil {
		t.Fatal(err)
	}
	for rowID, old := range before {
		var original, current map[string]any
		_ = json.Unmarshal(old, &original)
		raw, _ := recovered.records.types["build.materialextra"].rows.get(rowID).image()
		_ = json.Unmarshal(raw, &current)
		if current["localgrade"] != original["customergrade"] || current["inspectiondue"] != original["inspectiondue"] {
			t.Fatal("conflict resolution lost a customer field")
		}
	}
	decide(t, recovered, "user", build.ID, "build.materialextra.review", "build.materialextra", "M1", map[string]any{}, txnAt)
	if got := refuse(t, recovered, "user", build.ID, "build.materialextra.edit", "build.materialextra", "M2", map[string]any{"customergrade": "Alias write"}, txnAt); got == "ok" {
		t.Fatal("retained alias became writable")
	}
	final := extensionTenant(t, id, "3", "customergrade", false)
	if err := final.Replay(entries); err != nil {
		t.Fatal(err)
	}
	for rowID, expected := range extensionImages(t, recovered) {
		if !bytes.Equal(expected, extensionImages(t, final)[rowID]) {
			t.Fatal("resolved extension recovery differs")
		}
	}
}

func TestCustomerExtensionIncompatibleBaseAndProvenanceAreRejected(t *testing.T) {
	live := extensionTenant(t, "extension-incompatible", "1", "", false)
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	extensionFixture(t, live)
	upgraded := extensionTenant(t, live.ID, "4", "", true)
	if err := upgraded.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.checkExtensionSet(); err == nil || !strings.Contains(err.Error(), "weight") {
		t.Fatalf("incompatible base accepted: %v", err)
	}
	if got := refuse(t, upgraded, "user", build.ID, "build.materialextra.review", "build.materialextra", "M0", map[string]any{}, txnAt); !strings.Contains(got, "incompatible") {
		t.Fatalf("incompatible extension action ran: %s", got)
	}
	if got := refuse(t, live, "builder", build.ID, build.ObjectType+".edit", build.ObjectType, "extension", map[string]any{"extensionSource": map[string]any{"base": map[string]any{"sourceVersion": "forged"}}}, txnAt); got == "ok" {
		t.Fatal("builder supplied host-owned provenance")
	}
	decide(t, live, "builder", build.ID, build.ObjectType+".create", build.ObjectType, "shadow", map[string]any{"name": "shadow", "title": "Shadow", "extends": core.MaterialType, "fields": []build.Field{{Name: "base", Title: "Material", Type: "reference", Ref: core.MaterialType, Required: true}, {Name: "code", Title: "Overridden code", Type: "text"}}}, txnAt)
	if got := refuse(t, live, "builder", build.ID, build.SchemaPublish, build.ObjectType, "shadow", map[string]any{}, txnAt); !strings.Contains(got, "conflicts") {
		t.Fatalf("new extension shadows a base field: %s", got)
	}
}
