package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const SchemaPropertyType = construction.PropertyTypeType + ".publish"

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

func (f PropertyType) Definition() platform.PropertyType {
	return platform.PropertyType{Name: f.Name, Title: f.Title, Description: f.Description, Type: f.Type}
}

func (b *Build) propertyTypeEntity() platform.Entity {
	return platform.Entity{Type: construction.PropertyTypeType, Title: "Property type", Plural: "Property types", Model: PropertyType{}, Display: "title",
		Description: "Reusable scalar meaning; object fields keep their own identity and access.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{construction.Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder}, Capability: "propertytypes"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this shared scalar property as its next retained version.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{construction.Builder}, Capability: "propertytypes", Payload: []platform.Field{}, Do: b.publishPropertyType}}}}
}

func (b *Build) checkPropertyType(f *PropertyType) error {
	if !construction.Named(f.Name) {
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
		installed, _ := construction.ReadPublication[PropertyType](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Property type %s is already declared", f.Name)
		}
	}
	if old, ok := construction.ReadPublication[PropertyType](f.Published); ok && (old.Name != f.Name || old.Type != f.Type) {
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
	return readDefinitionInventory[PropertyType](b.host.Automation(platform.Caller{}, construction.ID))
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
