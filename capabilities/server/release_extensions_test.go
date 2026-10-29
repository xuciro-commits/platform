package platformserver

import "platformserver/studio/build"

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

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
	return composeTenant(t, id, []platform.Seat{seatOf("builder", "build:builder", "core:steward"), seatOf("user", "build:user")}, build.New(id), newExtensionCorePackage(id, version, added, incompatible))
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
	for id, row := range live.records.types["build.materialextra"].rows.All() {
		raw, err := row.Image()
		if err != nil {
			t.Fatal(err)
		}
		out[id] = raw
	}
	return out
}
