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

const LinkTypeType = "build.linktype"
const SchemaLinkType = LinkTypeType + ".publish"

// LinkType owns one bounded declaration and its retained publications. Draft
// edits never replace what callers use until the publication result commits.
type LinkType struct {
	platform.Record
	Cardinality string   `json:"cardinality,omitempty" choices:"one-to-many,one-to-one" title:"Relationship cardinality"`
	Name        string   `json:"name" field:"required,search"`
	Title       string   `json:"title" field:"required,search"`
	Description string   `json:"description" field:"required" type:"longtext"`
	Parent      string   `json:"parent" field:"required"`
	Child       string   `json:"child" field:"required"`
	Via         string   `json:"via" field:"required"`
	Forward     string   `json:"forward" field:"required"`
	Reverse     string   `json:"reverse" field:"required"`
	Required    bool     `json:"required" field:"readonly"`
	State       string   `json:"state" field:"readonly" choices:"draft,published"`
	Version     int      `json:"version,omitempty" field:"readonly"`
	Published   string   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string `json:"versions,omitempty" field:"readonly"`
}

func (f LinkType) definition() platform.LinkType {
	cardinality := f.Cardinality
	if cardinality == "" {
		cardinality = "one-to-many"
	}
	return platform.LinkType{Name: f.Name, Title: f.Title, Description: f.Description, Parent: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: f.Parent}, Child: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: f.Child}, Via: f.Via, Forward: f.Forward, Reverse: f.Reverse, Required: f.Required, Storage: "reference", Cardinality: cardinality, DeletePolicy: "owner"}
}

func (f LinkType) Declaration() platform.LinkType { return f.definition() }

func (b *Build) linkTypeEntity() platform.Entity {
	return platform.Entity{Type: LinkTypeType, Title: "Link type", Plural: "Link types", Model: LinkType{}, Display: "title",
		Description: "A versioned relationship over an original reference; drafts do not change installed traversal.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "linktypes"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this reference-backed relationship as its next retained version.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "linktypes", Payload: []platform.Field{}, Do: b.publishLinkType}}}}
}

func (b *Build) checkLinkType(f *LinkType) error {
	if !named(f.Name) || !named(f.Forward) || !named(f.Reverse) {
		return fmt.Errorf("Link names must use lower-case letters and digits")
	}
	if _, ok := b.installed[f.Parent]; !ok {
		return fmt.Errorf("Link parent needs this builder's published object")
	}
	if _, ok := b.installed[f.Child]; !ok {
		return fmt.Errorf("Link child needs this builder's published object")
	}
	info, ok := b.host.Entity(f.Child)
	if !ok {
		return fmt.Errorf("Link child is unavailable")
	}
	field, ok := info.Field(f.Via)
	if !ok {
		return fmt.Errorf("Link reference is unavailable")
	}
	f.Required = field.Required
	if err := b.host.ValidateInstallLinkType(f.definition()); err != nil {
		return err
	}
	list, err := b.linkTypeInventory()
	if err != nil {
		return err
	}
	for _, other := range list {
		installed, _ := wasPublished[LinkType](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Link type %s is already declared", f.Name)
		}
	}
	if old, ok := wasPublished[LinkType](f.Published); ok && (old.Name != f.Name || old.Parent != f.Parent || old.Child != f.Child || old.Via != f.Via || old.Required != f.Required || old.definition().Cardinality == "one-to-one" && f.definition().Cardinality != "one-to-one") {
		return fmt.Errorf("A published link keeps its identity, objects, reference shape and retained uniqueness")
	}
	return nil
}

func (b *Build) publishLinkType(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	f, ok := record.(*LinkType)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkLinkType(f); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if f.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A link type may retain at most 64 published versions")
	}
	f.Version++
	f.State = "published"
	f.Published = published(*f)
	f.Versions = append(f.Versions, f.Published)
	if !c.Staging() {
		if err := b.installLinkType(c, *f); err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	}
	return nil
}

func linkTypeImage(image []byte) (LinkType, error) {
	var record LinkType
	if err := json.Unmarshal(image, &record); err != nil {
		return LinkType{}, err
	}
	if record.State != "published" || record.Version < 1 || record.Version > 64 || len(record.Versions) != record.Version || record.Published != record.Versions[record.Version-1] {
		return LinkType{}, fmt.Errorf("accepted link type has an invalid version family")
	}
	var latest LinkType
	for i, raw := range record.Versions {
		f, ok := wasPublished[LinkType](raw)
		if !ok || f.State != "published" || f.ID != record.ID || f.Version != i+1 || f.Published != "" || len(f.Versions) != 0 || f.definition().Check() != nil || i > 0 && (f.Name != latest.Name || f.Parent != latest.Parent || f.Child != latest.Child || f.Via != latest.Via || f.Required != latest.Required || latest.definition().Cardinality == "one-to-one" && f.definition().Cardinality != "one-to-one") {
			return LinkType{}, fmt.Errorf("accepted link type has a malformed version %d", i+1)
		}
		latest = f
	}
	return latest, nil
}

func (b *Build) linkTypeInventory() ([]LinkType, error) {
	return readDefinitionInventory[LinkType](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) installLinkType(c platform.Caller, f LinkType) error {

	if err := b.host.InstallLinkType(c, f.definition(), f.Version); err != nil {
		return err
	}
	b.linkTypes[f.Name] = f
	return nil
}

func (b *Build) LinkTypeDefinition(name string, version int) (platform.LinkType, int, bool) {
	installed, ok := b.linkTypes[name]
	if ok && (version == 0 || version == installed.Version) {
		return installed.definition(), installed.Version, true
	}
	if version == 0 {
		return platform.LinkType{}, 0, false
	}
	list, err := b.linkTypeInventory()
	if err != nil {
		return platform.LinkType{}, 0, false
	}
	for _, record := range list {
		if version < 1 || version > len(record.Versions) {
			continue
		}
		raw, _ := json.Marshal(record)
		latest, err := linkTypeImage(raw)
		if err != nil || latest.Name != name {
			continue
		}
		f, ok := wasPublished[LinkType](record.Versions[version-1])
		if ok && f.Version == version {
			return f.definition(), version, true
		}
	}
	return platform.LinkType{}, 0, false
}

func (b *Build) linkTypeDeclarations() []platform.LinkType {
	names := make([]string, 0, len(b.linkTypes))
	for name := range b.linkTypes {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.LinkType, 0, len(names))
	for _, name := range names {
		out = append(out, b.linkTypes[name].definition())
	}
	return out
}
