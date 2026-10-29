package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"slices"
	"strconv"
	"time"
)

func propertyBindings(fields []Field) map[string]platform.AssetBinding {
	out := map[string]platform.AssetBinding{}
	for _, f := range fields {
		if f.Property != nil {
			out[f.Name] = *f.Property
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
func (b *Build) checkPropertyFields(fields []Field) error {
	for _, f := range fields {
		if f.Property == nil {
			continue
		}
		if b.host == nil || f.Property.Ref.App != ID {
			return fmt.Errorf("shared property needs its canonical tenant owner")
		}
		p, ok := b.host.ResolvePropertyType(*f.Property)
		if !ok {
			return fmt.Errorf("shared property version is unavailable")
		}
		if f.Choices != "" || f.Ref != "" || f.Inverse != "" {
			return fmt.Errorf("shared scalar property cannot declare another field shape")
		}
		if err := p.CheckField(platform.FieldInfo{Name: f.Name, Type: f.Type, Title: f.Title, Required: f.Required}); err != nil {
			return err
		}
	}
	return nil
}

const SchemaPropertyType = PropertyTypeType + ".publish"

type PropertyType struct {
	platform.Record
	Name        string   `json:"name" field:"required,search"`
	Title       string   `json:"title" field:"required,search"`
	Description string   `json:"description" field:"required" type:"longtext"`
	Type        string   `json:"type" field:"required" choices:"text,longtext,integer,decimal,date,datetime,boolean"`
	State       string   `json:"state" field:"readonly" choices:"draft,published"`
	Version     int      `json:"version,omitempty" field:"readonly"`
	Published   string   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string `json:"versions,omitempty" field:"readonly"`
}

func (f PropertyType) Definition() platform.PropertyType {
	return platform.PropertyType{Name: f.Name, Title: f.Title, Description: f.Description, Type: f.Type}
}

func (b *Build) propertyTypeEntity() platform.Entity {
	return platform.Entity{Type: PropertyTypeType, Title: "Property type", Plural: "Property types", Model: PropertyType{}, Display: "title",
		Description: "Reusable scalar meaning; object fields keep their own identity and access.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "propertytypes"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this shared scalar property as its next retained version.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "propertytypes", Payload: []platform.Field{}, Do: b.publishPropertyType}}}}
}

func (b *Build) checkPropertyType(f *PropertyType) error {
	if !Named(f.Name) {
		return fmt.Errorf("Property name must use lower-case letters and digits")
	}
	if err := f.Definition().Check(); err != nil {
		return err
	}
	list, err := b.propertyTypeInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		installed, _ := ReadPublication[PropertyType](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Property type %s is already declared", f.Name)
		}
	}
	if old, ok := ReadPublication[PropertyType](f.Published); ok && (old.Name != f.Name || old.Type != f.Type) {
		return fmt.Errorf("A published property keeps its identity and scalar type")
	}
	return nil
}

func (b *Build) publishPropertyType(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	f, ok := record.(*PropertyType)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkPropertyType(f); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return publishRetainedDefinition(c, f, "A property type may retain at most 64 published versions", b.installPropertyType)
}

func propertyTypeImage(image []byte) (PropertyType, error) {
	return definitionImage[PropertyType](image, "property type", func(before, after PropertyType) bool {
		return before.Name == after.Name && before.Type == after.Type
	})
}

func (b *Build) propertyTypeInventory() ([]PropertyType, error) {
	return readDefinitionInventory[PropertyType](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) installPropertyType(c platform.Caller, f PropertyType) error {

	if err := b.host.InstallPropertyType(c, f.Definition(), f.Version); err != nil {
		return err
	}
	b.propertyTypes[f.Name] = f
	return nil
}

func (b *Build) PropertyTypeDefinition(name string, version int) (platform.PropertyType, int, bool) {
	return retainedDefinition(b.propertyTypes, b.propertyTypeInventory, propertyTypeImage, name, version)
}

func propertyTypeReleaseAsset(f PropertyType, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetPropertyType, Name: f.Name}, sourceVersion+".property-"+strconv.Itoa(f.Version), f.Definition())
}

func (b *Build) PropertyTypeReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "property", "property type", platform.AssetPropertyType, name, sourceVersion, b.PropertyTypeDefinition)
}

func (b *Build) propertyTypeAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.propertyTypes, func(record PropertyType) (platform.ReleaseAsset, error) {
		return propertyTypeReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) InstallPropertyTypeAsset(asset platform.ReleaseAsset) error {
	version := b.definitionAssetVersion(asset, platform.AssetPropertyType, "property")
	var declaration platform.PropertyType
	if version == 0 || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &declaration) != nil || declaration.Check() != nil || declaration.Name != asset.Ref.Name {
		return fmt.Errorf("invalid builder property type asset %s", asset.Ref)
	}
	f := propertyTypeFromDeclaration(declaration)
	if _, exists := b.propertyTypes[f.Name]; exists {
		return fmt.Errorf("propertyType asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installPropertyType(platform.Caller{Replaying: true}, f)
}

func (b *Build) propertyTypeDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetPropertyType, "property type", b.propertyTypeInventory, func(record PropertyType) (PropertyType, error) {
		err := b.checkPropertyType(&record)
		return record, err
	}, func(record PropertyType) (platform.ReleaseAsset, error) {
		record.Version++
		return propertyTypeReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) preparePropertyTypeReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.propertyTypeInventory, platform.AssetPropertyType, "property", SchemaPropertyType, "property type", b.PropertyTypeReleaseAsset, func(record PropertyType, asset platform.ReleaseAsset, version int) (PropertyType, error) {
		var q platform.PropertyType
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Type != record.Type {
			return record, fmt.Errorf("invalid next property type version")
		}
		frozen := propertyTypeFromDeclaration(q)
		frozen.Record = record.Record
		frozen.State = "published"
		frozen.Version = version
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}

func propertyTypeFromDeclaration(p platform.PropertyType) PropertyType {
	return PropertyType{Name: p.Name, Title: p.Title, Description: p.Description, Type: p.Type}
}

const PropertyTypeType = "build.propertytype"
