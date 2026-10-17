package build

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const MigrationPlanType = "build.migration-plan"
const SchemaMigrationPlan = MigrationPlanType + ".publish"

type MigrationPlanRecord struct {
	platform.Record
	Name              string                 `json:"name" field:"required,search"`
	Title             string                 `json:"title" field:"required,search"`
	Plan              platform.MigrationPlan `json:"plan" type:"json" field:"required"`
	State             string                 `json:"state" field:"readonly" choices:"draft,published"`
	DefinitionVersion int                    `json:"definitionVersion,omitempty" field:"readonly" title:"Definition version"`
	Published         string                 `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string               `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

func (m MigrationPlanRecord) descriptor() platform.MigrationDescriptor {
	return platform.MigrationDescriptor{Name: m.Name, Title: m.Title, Plan: m.Plan}
}

func (b *Build) migrationPlanEntity() platform.Entity {
	return platform.Entity{Type: MigrationPlanType, Title: "Migration plan", Plural: "Migration plans", Model: MigrationPlanRecord{}, Display: "title",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "migrations"},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			m := record.(*MigrationPlanRecord)
			if err := b.checkMigrationPlan(*m); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Retain this migration plan as an immutable asset without executing it", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "migrations", Payload: []platform.Field{}, Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
				m := record.(*MigrationPlanRecord)
				next, err := b.retainMigrationPlan(*m)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				*m = next
				return nil
			}}}}}
}

func (b *Build) migrationPlanInventory() ([]MigrationPlanRecord, error) {
	return readDefinitionInventory[MigrationPlanRecord](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) checkMigrationPlan(m MigrationPlanRecord) error {
	if !named(m.Name) {
		return fmt.Errorf("The migration plan name must use lower-case letters and digits")
	}
	if err := m.descriptor().Check(); err != nil {
		return err
	}
	rows, err := b.migrationPlanInventory()
	if err != nil {
		return err
	}
	for _, other := range rows {
		if other.ID != m.ID && other.Name == m.Name {
			return fmt.Errorf("The migration plan name is already used")
		}
	}
	if m.Published != "" {
		var old platform.ReleaseAsset
		if json.Unmarshal([]byte(m.Published), &old) != nil || old.Ref.Name != m.Name {
			return fmt.Errorf("A published migration plan keeps its name")
		}
	}
	return nil
}

func migrationPlanAsset(m MigrationPlanRecord, version int) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetMigration, Name: m.Name}, definitionVersion+".migration-"+strconv.Itoa(version), m.descriptor())
}

func decodeMigrationAsset(asset platform.ReleaseAsset) (platform.MigrationDescriptor, int, error) {
	var d platform.MigrationDescriptor
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, definitionVersion+".migration-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetMigration || asset.ContractVersion != 1 || platform.CheckAssetDependencies(asset) != nil || json.Unmarshal(asset.Body, &d) != nil || d.Name != asset.Ref.Name || d.Check() != nil {
		return d, 0, fmt.Errorf("Invalid migration asset")
	}
	return d, version, nil
}

func (b *Build) retainMigrationPlan(m MigrationPlanRecord) (MigrationPlanRecord, error) {
	if err := b.checkMigrationPlan(m); err != nil {
		return m, err
	}
	if m.DefinitionVersion >= 64 {
		return m, fmt.Errorf("The migration plan version family is full")
	}
	asset, err := migrationPlanAsset(m, m.DefinitionVersion+1)
	if err != nil {
		return m, err
	}
	raw, err := json.Marshal(asset)
	if err != nil {
		return m, err
	}
	m.State, m.DefinitionVersion, m.Published = "published", m.DefinitionVersion+1, string(raw)
	m.Versions = append(slices.Clone(m.Versions), m.Published)
	return m, nil
}

func (b *Build) migrationPlanAssets() ([]platform.ReleaseAsset, error) {
	return portableDefinitionAssets(b.migrationPlanInventory, decodeMigrationAsset, "migration plan")
}

func (b *Build) migrationPlanPinned(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	return pinnedDefinitionAsset(b.migrationPlanInventory, decodeMigrationAsset, ref, version, "migration plan")
}

func (b *Build) migrationPlanDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	rows, e := b.migrationPlanInventory()
	if e != nil {
		err = e
		return
	}
	i := slices.IndexFunc(rows, func(m MigrationPlanRecord) bool { return m.ID == id && !m.Archived })
	if i < 0 {
		err = fmt.Errorf("Migration plan draft not found")
		return
	}
	m := rows[i]
	if err = b.checkMigrationPlan(m); err != nil {
		return
	}
	if m.DefinitionVersion >= 64 {
		err = fmt.Errorf("The migration plan version family is full")
		return
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetMigration, Name: m.Name}
	prior, hadPrior = next, m.Published != ""
	a, e := migrationPlanAsset(m, m.DefinitionVersion+1)
	if e != nil {
		err = e
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(a platform.ReleaseAsset) bool { return a.Ref == next })
	after = append(after, a)
	return
}

func (b *Build) prepareMigrationPlanInstallations(assets []platform.ReleaseAsset) ([]platform.AssetInstallation, error) {
	rows, err := b.migrationPlanInventory()
	if err != nil {
		return nil, err
	}
	out := []platform.AssetInstallation{}
	for _, a := range assets {
		if a.Ref.Kind != platform.AssetMigration || a.Ref.App != ID {
			continue
		}
		d, version, err := decodeMigrationAsset(a)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(m MigrationPlanRecord) bool { return m.Name == d.Name && !m.Archived })
		if i < 0 {
			return nil, fmt.Errorf("Migration asset needs its owner draft")
		}
		m := rows[i]
		if version <= m.DefinitionVersion {
			prior, err := b.migrationPlanPinned(a.Ref, a.SourceVersion)
			if err != nil || !equalMigrationAssets(prior, a) {
				return nil, fmt.Errorf("Retained migration plan bytes differ")
			}
			continue
		}
		if version != m.DefinitionVersion+1 {
			return nil, fmt.Errorf("Migration version must be the next retained version")
		}
		m.Title, m.Plan = d.Title, d.Plan
		m.State, m.DefinitionVersion = "published", version
		raw, _ := json.Marshal(a)
		m.Published = string(raw)
		m.Versions = append(slices.Clone(m.Versions), m.Published)
		image, _ := json.Marshal(m)
		out = append(out, platform.AssetInstallation{Ref: a.Ref, Schema: SchemaMigrationPlan, Record: MigrationPlanType + "/" + m.ID, Image: image})
	}
	return out, nil
}

func equalMigrationAssets(a, b platform.ReleaseAsset) bool {
	var x, y any
	return a.Ref == b.Ref && a.SourceVersion == b.SourceVersion && a.ContractVersion == b.ContractVersion && len(a.Requires) == len(b.Requires) && json.Unmarshal(a.Body, &x) == nil && json.Unmarshal(b.Body, &y) == nil && reflect.DeepEqual(x, y)
}

func migrationPlanInstallation(image json.RawMessage) (platform.AssetInstallation, error) {
	return portableDefinitionInstallation[MigrationPlanRecord](image, MigrationPlanType, SchemaMigrationPlan, "migration", decodeMigrationAsset)
}

// Legacy plan inputs are frozen through the same declaration owner inside
// their start decision; there is still only one compiler and executor.
func (b *Build) freezeMigrationInput(c platform.Caller, r *pb.ChangeRecord, plan platform.MigrationPlan, binding *platform.AssetBinding) (platform.MigrationPlan, *platform.AssetBinding, *kernel.Error) {
	if binding != nil {
		a, err := b.ResolvePinned(binding.Ref, binding.SourceVersion)
		if err != nil {
			return plan, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		d, _, err := decodeMigrationAsset(a)
		if err != nil {
			return plan, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		if plan.From != "" && !reflect.DeepEqual(plan, d.Plan) {
			return plan, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The migration asset differs from the reviewed plan")
		}
		copy := *binding
		return d.Plan, &copy, nil
	}
	raw, _ := json.Marshal(plan)
	digest := sha256.Sum256(raw)
	name := fmt.Sprintf("migration%x", digest[:12])
	m := MigrationPlanRecord{Record: platform.Record{ID: "plan:" + r.GetSubmission().GetTarget().GetId()}, Name: name, Title: "Migration plan", Plan: plan}
	rows, err := b.migrationPlanInventory()
	if err != nil {
		return plan, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	for _, row := range rows {
		if row.Name == name {
			var a platform.ReleaseAsset
			if json.Unmarshal([]byte(row.Published), &a) == nil {
				d, _, err := decodeMigrationAsset(a)
				if err == nil && reflect.DeepEqual(d.Plan, plan) {
					return plan, &platform.AssetBinding{Ref: a.Ref, SourceVersion: a.SourceVersion}, nil
				}
			}
		}
	}
	m, err = b.retainMigrationPlan(m)
	if err != nil {
		return plan, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := c.Put(r, m); err != nil {
		return plan, nil, err
	}
	var a platform.ReleaseAsset
	_ = json.Unmarshal([]byte(m.Published), &a)
	return plan, &platform.AssetBinding{Ref: a.Ref, SourceVersion: a.SourceVersion}, nil
}
