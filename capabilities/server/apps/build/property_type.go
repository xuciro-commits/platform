package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const PropertyTypeType = "build.propertytype"
const SchemaPropertyType = PropertyTypeType + ".publish"

// PropertyType owns one bounded declaration and its retained publications. Draft
// edits never replace what callers use until the publication result commits.
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

func (f PropertyType) definition() platform.PropertyType {
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
	if !named(f.Name) {
		return fmt.Errorf("Property name must use lower-case letters and digits")
	}
	if err := f.definition().Check(); err != nil {
		return err
	}
	list, err := b.propertyTypeInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		installed, _ := wasPublished[PropertyType](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Property type %s is already declared", f.Name)
		}
	}
	if old, ok := wasPublished[PropertyType](f.Published); ok && (old.Name != f.Name || old.Type != f.Type) {
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
	if f.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A property type may retain at most 64 published versions")
	}
	f.Version++
	f.State = "published"
	f.Published = published(*f)
	f.Versions = append(f.Versions, f.Published)
	if !c.Staging() {
		if err := b.installPropertyType(c, *f); err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	}
	return nil
}

func propertyTypeImage(image []byte) (PropertyType, error) {
	var record PropertyType
	if err := json.Unmarshal(image, &record); err != nil {
		return PropertyType{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return PropertyType{}, fmt.Errorf("accepted property type has an invalid version family")
	}
	var latest PropertyType
	for i, raw := range record.Versions {
		f, ok := wasPublished[PropertyType](raw)
		if !ok || f.State != "published" || f.ID != record.ID || f.Version != i+1 || f.Published != "" || len(f.Versions) != 0 || f.definition().Check() != nil || i > 0 && (f.Name != latest.Name || f.Type != latest.Type) {
			return PropertyType{}, fmt.Errorf("accepted property type has a malformed version %d", i+1)
		}
		latest = f
	}
	return latest, nil
}

func (b *Build) propertyTypeInventory() ([]PropertyType, error) {
	return readDefinitionInventory[PropertyType](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) installPropertyType(c platform.Caller, f PropertyType) error {

	if err := b.host.InstallPropertyType(c, f.definition(), f.Version); err != nil {
		return err
	}
	b.propertyTypes[f.Name] = f
	return nil
}

func (b *Build) PropertyTypeDefinition(name string, version int) (platform.PropertyType, int, bool) {
	installed, ok := b.propertyTypes[name]
	if ok && (version == 0 || version == installed.Version) {
		return installed.definition(), installed.Version, true
	}
	if version == 0 {
		return platform.PropertyType{}, 0, false
	}
	list, err := b.propertyTypeInventory()
	if err != nil {
		return platform.PropertyType{}, 0, false
	}
	for _, record := range list {
		if version < 1 || version > len(record.Versions) {
			continue
		}
		raw, _ := json.Marshal(record)
		latest, err := propertyTypeImage(raw)
		if err != nil || latest.Name != name {
			continue
		}
		f, ok := wasPublished[PropertyType](record.Versions[version-1])
		if ok && f.Version == version {
			return f.definition(), version, true
		}
	}
	return platform.PropertyType{}, 0, false
}

func (b *Build) propertyTypeDeclarations() []platform.PropertyType {
	names := make([]string, 0, len(b.propertyTypes))
	for name := range b.propertyTypes {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.PropertyType, 0, len(names))
	for _, name := range names {
		out = append(out, b.propertyTypes[name].definition())
	}
	return out
}
