package build

import (
	"encoding/json"
	"errors"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

type definitionKind interface {
	assetKind() platform.AssetKind
	schema() string
	entity(*Build) platform.Entity
	resolve(*Build, string, string) (platform.ReleaseAsset, error)
	assets(*Build) ([]platform.ReleaseAsset, error)
	draftAssets(*Build, string) ([]platform.ReleaseAsset, []platform.ReleaseAsset, platform.AssetRef, platform.AssetRef, bool, error)
	prepare(*Build, []platform.ReleaseAsset) ([]platform.ReleasePublication, error)
	draft(platform.ReleaseAsset) (string, any, error)
	installAsset(*Build, platform.ReleaseAsset) error
	validateImage(*Build, []byte) error
	applyImage(*Build, []byte) error
	restore(*Build) error
}

type resourceKind[T interface{ Definition() D }, D interface{ Check() error }] struct {
	asset           platform.AssetKind
	typ             string
	prefix          string
	noun            string
	title           string
	plural          string
	capability      string
	description     string
	transition      string
	immutable       string
	joint           bool
	records         func(*Build) map[string]T
	validate        func(*Build, T) (T, error)
	identity        func(a, b T) bool
	stable          func(old, next T) bool
	guarantees      func(old, next T) bool
	adopt           func(record *T, frozen T)
	fromDeclaration func(D) T
	contract        func(D) int
	admit           func(D) bool
	install         func(platform.BuildHost, platform.Caller, D, int) error
	validateInstall func(platform.BuildHost, D) error
}

var definitionKinds = []definitionKind{&queryKind, &propertyKind, &valueKind, &linkKind}

func definitionKindOf(kind platform.AssetKind) definitionKind {
	for _, k := range definitionKinds {
		if k.assetKind() == kind {
			return k
		}
	}
	return nil
}

func definitionKindOfSchema(schema string) definitionKind {
	for _, k := range definitionKinds {
		if k.schema() == schema {
			return k
		}
	}
	return nil
}

func (b *Build) kindAssets(k definitionKind) func() ([]platform.ReleaseAsset, error) {
	return func() ([]platform.ReleaseAsset, error) { return k.assets(b) }
}

func (b *Build) InstallDefinitionAsset(asset platform.ReleaseAsset) error {
	k := definitionKindOf(asset.Ref.Kind)
	if k == nil {
		return fmt.Errorf("asset %s has no definition kind", asset.Ref)
	}
	return k.installAsset(b, asset)
}

func (k *resourceKind[T, D]) assetKind() platform.AssetKind { return k.asset }

func (k *resourceKind[T, D]) schema() string { return k.typ + ".publish" }

func (k *resourceKind[T, D]) label() string { return strings.ToLower(k.title) }

func (k *resourceKind[T, D]) entity(b *Build) platform.Entity {
	return platform.Entity{Type: k.typ, Title: k.title, Plural: k.plural, Model: *new(T), Display: "title",
		Description: k.description,
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: k.capability},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: k.transition, From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: k.capability, Payload: []platform.Field{},
				Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
					return k.publish(b, c, record)
				}}}}}
}

func (k *resourceKind[T, D]) publish(b *Build, c platform.Caller, record any) *kernel.Error {
	f, ok := record.(*T)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	bound, err := k.check(b, *f)
	if err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	*f = bound
	return publishRetainedDefinition(c, f, "A "+k.label()+" may retain at most 64 published versions", func(c platform.Caller, f T) error { return k.installRecord(b, c, f) })
}

func (k *resourceKind[T, D]) check(b *Build, f T) (T, error) {
	f, err := k.validate(b, f)
	if err != nil {
		return f, err
	}
	family := definitionFamilyOf(f)
	list, err := k.inventory(b)
	if err != nil {
		return f, err
	}
	for _, other := range list {
		known := definitionFamilyOf(other)
		installed, _ := ReadPublication[T](known.Published)
		if known.ID != family.ID && (known.Name == family.Name || definitionFamilyOf(installed).Name == family.Name) {
			return f, fmt.Errorf("%s %s is already declared", k.title, family.Name)
		}
	}
	if old, ok := ReadPublication[T](family.Published); ok && !k.compatible(old, f) {
		return f, errors.New(k.immutable)
	}
	return f, nil
}

func (k *resourceKind[T, D]) compatible(old, next T) bool {
	return k.identity(old, next) && (k.stable == nil || k.stable(old, next)) && (k.guarantees == nil || k.guarantees(old, next))
}

func (k *resourceKind[T, D]) image(raw []byte) (T, error) {
	return definitionImage[T, D](raw, k.label(), k.compatible)
}

func (k *resourceKind[T, D]) inventory(b *Build) ([]T, error) {
	return readDefinitionInventory[T](b.host.Automation(platform.Caller{}, ID))
}

func (k *resourceKind[T, D]) installRecord(b *Build, c platform.Caller, f T) error {
	family := definitionFamilyOf(f)
	if err := k.install(b.host, c, f.Definition(), family.Version); err != nil {
		return err
	}
	k.records(b)[family.Name] = f
	return nil
}

func (k *resourceKind[T, D]) contractOf(d D) int {
	if k.contract == nil {
		return 1
	}
	return k.contract(d)
}

func (k *resourceKind[T, D]) definition(b *Build, name string, version int) (D, int, bool) {
	if k.joint {
		if asset, selected := b.jointDeclarations[platform.AssetRef{App: ID, Kind: k.asset, Name: name}]; selected {
			ordinal := b.definitionAssetVersion(asset, k.asset, k.prefix)
			if ordinal == version && version > 0 {
				var d D
				valid := json.Unmarshal(asset.Body, &d) == nil && asset.ContractVersion == k.contractOf(d) && definitionFamilyOf(k.fromDeclaration(d)).Name == name && d.Check() == nil
				return d, ordinal, valid
			}
		}
	}
	return retainedDefinition(k.records(b), func() ([]T, error) { return k.inventory(b) }, k.image, name, version)
}

func (k *resourceKind[T, D]) releaseAsset(b *Build, f T) (platform.ReleaseAsset, error) {
	family := definitionFamilyOf(f)
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: k.asset, Name: family.Name}, b.Manifest().Version+"."+k.prefix+"-"+strconv.Itoa(family.Version), f.Definition())
}

func (k *resourceKind[T, D]) resolve(b *Build, name, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, k.prefix, k.label(), k.asset, name, sourceVersion, func(name string, version int) (D, int, bool) { return k.definition(b, name, version) })
}

func (k *resourceKind[T, D]) assets(b *Build) ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(k.records(b), func(f T) (platform.ReleaseAsset, error) { return k.releaseAsset(b, f) })
}

func (k *resourceKind[T, D]) fromAsset(b *Build, asset platform.ReleaseAsset) (T, error) {
	var zero T
	version := b.definitionAssetVersion(asset, k.asset, k.prefix)
	var d D
	if version == 0 || json.Unmarshal(asset.Body, &d) != nil || asset.ContractVersion != k.contractOf(d) || d.Check() != nil || definitionFamilyOf(k.fromDeclaration(d)).Name != asset.Ref.Name || k.admit != nil && !k.admit(d) {
		return zero, fmt.Errorf("invalid builder %s asset %s", k.label(), asset.Ref)
	}
	f := k.fromDeclaration(d)
	family := definitionFamilyOf(f)
	family.Version, family.State = version, "published"
	setDefinitionFamily(&f, family)
	return f, nil
}

func (k *resourceKind[T, D]) installAsset(b *Build, asset platform.ReleaseAsset) error {
	f, err := k.fromAsset(b, asset)
	if err != nil {
		return err
	}
	if _, exists := k.records(b)[definitionFamilyOf(f).Name]; exists {
		return fmt.Errorf("%s asset %s is already installed", k.noun, asset.Ref)
	}
	return k.installRecord(b, platform.Caller{Replaying: true}, f)
}

func (k *resourceKind[T, D]) draftAssets(b *Build, id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, k.asset, k.label(), func() ([]T, error) { return k.inventory(b) }, func(record T) (T, error) {
		return k.check(b, record)
	}, func(record T) (platform.ReleaseAsset, error) {
		family := definitionFamilyOf(record)
		family.Version++
		setDefinitionFamily(&record, family)
		return k.releaseAsset(b, record)
	})
}

func (k *resourceKind[T, D]) prepare(b *Build, assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, func() ([]T, error) { return k.inventory(b) }, k.asset, k.prefix, k.schema(), k.label(), func(name, version string) (platform.ReleaseAsset, error) { return k.resolve(b, name, version) }, func(record T, asset platform.ReleaseAsset, version int) (T, error) {
		family := definitionFamilyOf(record)
		prior, retained := ReadPublication[T](family.Published)
		var d D
		if version != family.Version+1 || json.Unmarshal(asset.Body, &d) != nil || d.Check() != nil {
			return record, fmt.Errorf("invalid next %s version", k.label())
		}
		frozen := k.fromDeclaration(d)
		if !k.identity(record, frozen) || retained && k.guarantees != nil && !k.guarantees(prior, frozen) {
			return record, fmt.Errorf("invalid next %s version", k.label())
		}
		kept := definitionFamilyOf(frozen)
		kept.Record, kept.State, kept.Version = family.Record, "published", version
		setDefinitionFamily(&frozen, kept)
		if k.adopt != nil {
			k.adopt(&record, frozen)
		}
		family.State, family.Version, family.Published = "published", version, published(frozen)
		family.Versions = append(slices.Clone(family.Versions), family.Published)
		setDefinitionFamily(&record, family)
		return record, nil
	})
}

func (k *resourceKind[T, D]) draft(asset platform.ReleaseAsset) (string, any, error) {
	var d D
	if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
		return "", nil, err
	}
	return k.typ, k.fromDeclaration(d), nil
}

func (k *resourceKind[T, D]) validateImage(b *Build, raw []byte) error {
	selected, err := k.image(raw)
	if err != nil {
		return err
	}
	return k.validateInstall(b.host, selected.Definition())
}

func (k *resourceKind[T, D]) applyImage(b *Build, raw []byte) error {
	return applyDefinitionHistory(raw, k.image, func(c platform.Caller, f T) error { return k.installRecord(b, c, f) })
}

func (k *resourceKind[T, D]) restore(b *Build) error {
	return restoreDefinitionPublications(b, func() ([]T, error) { return k.inventory(b) }, k.schema())
}
