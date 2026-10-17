package build

import (
	"encoding/json"
	"fmt"

	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// LinkType owns one bounded declaration and its retained publications. Draft
// edits never replace what callers use until the publication result commits.
type LinkType struct {
	platform.Record
	Storage      string   `json:"storage,omitempty" choices:"reference,edge"`
	Roles        []string `json:"roles,omitempty" field:"aside"`
	DeletePolicy string   `json:"deletePolicy,omitempty" choices:"owner,restrict-active" title:"Relationship archive policy"`
	Cardinality  string   `json:"cardinality,omitempty" choices:"one-to-many,one-to-one,many-to-many" title:"Relationship cardinality"`
	Name         string   `json:"name" field:"required,search"`
	Title        string   `json:"title" field:"required,search"`
	Description  string   `json:"description" field:"required" type:"longtext"`
	Parent       string   `json:"parent" field:"required"`
	Child        string   `json:"child" field:"required"`
	Via          string   `json:"via,omitempty"`
	Forward      string   `json:"forward" field:"required"`
	Reverse      string   `json:"reverse" field:"required"`
	Required     bool     `json:"required" title:"Parent relationship required"`
	State        string   `json:"state" field:"readonly" choices:"draft,published"`
	Version      int      `json:"version,omitempty" field:"readonly"`
	Published    string   `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions     []string `json:"versions,omitempty" field:"readonly"`
}

func (f LinkType) Definition() platform.LinkType {
	storage := f.Storage
	if storage == "" {
		storage = "reference"
	}
	roles := f.Roles
	if storage == "edge" && len(roles) == 0 {
		roles = []string{Builder, User}
	}
	policy := f.DeletePolicy
	if policy == "" {
		policy = "owner"
	}
	cardinality := f.Cardinality
	if cardinality == "" {
		cardinality = "one-to-many"
	}
	return platform.LinkType{Name: f.Name, Title: f.Title, Description: f.Description, Parent: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: f.Parent}, Child: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: f.Child}, Via: f.Via, Forward: f.Forward, Reverse: f.Reverse, Required: f.Required, Storage: storage, Cardinality: cardinality, DeletePolicy: policy, Roles: roles}
}

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
	parent, parentFound := b.lookupEntity(f.Parent)
	info, childFound := b.lookupEntity(f.Child)
	_, parentInstalled := b.installed[f.Parent]
	_, childInstalled := b.installed[f.Child]
	_, parentSelected := b.jointEntities[f.Parent]
	_, childSelected := b.jointEntities[f.Child]
	if !parentFound || parent.App != ID || !parentInstalled && !parentSelected {
		return fmt.Errorf("Link parent needs this builder's published or jointly selected object")
	}
	if !childFound || info.App != ID || !childInstalled && !childSelected {
		return fmt.Errorf("Link child needs this builder's published or jointly selected object")
	}
	if f.Storage != "edge" {
		field, ok := info.Field(f.Via)
		if !ok {
			return fmt.Errorf("Link reference is unavailable")
		}
		f.Required = field.Required
	}
	if err := f.Definition().CheckSchema(parent, info); err != nil {
		return err
	}
	// A joint candidate validates its records and relationship constraints in
	// the existing private installation, after the selected objects are present.
	if !b.jointMode {
		if err := b.host.ValidateInstallLinkType(f.Definition()); err != nil {
			return err
		}
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
	if old, ok := wasPublished[LinkType](f.Published); ok && (old.Name != f.Name || old.Parent != f.Parent || old.Child != f.Child || old.Via != f.Via || old.Definition().Storage == "reference" && old.Required != f.Required || !linkKeepsGuarantees(old, *f)) {
		return fmt.Errorf("A published link keeps its identity, objects, reference shape and retained constraints")
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
	return definitionImage[LinkType](image, "link type", func(before, after LinkType) bool {
		return before.Name == after.Name && before.Parent == after.Parent && before.Child == after.Child && before.Via == after.Via && (after.Definition().Storage != "reference" || before.Required == after.Required) && linkKeepsGuarantees(before, after)
	})
}

func (b *Build) linkTypeInventory() ([]LinkType, error) {
	return readDefinitionInventory[LinkType](b.host.Automation(platform.Caller{}, ID))
}

func (b *Build) installLinkType(c platform.Caller, f LinkType) error {

	if err := b.host.InstallLinkType(c, f.Definition(), f.Version); err != nil {
		return err
	}
	b.linkTypes[f.Name] = f
	return nil
}

func (b *Build) LinkTypeDefinition(name string, version int) (platform.LinkType, int, bool) {
	return retainedDefinition(b.linkTypes, b.linkTypeInventory, linkTypeImage, name, version)
}

func (b *Build) linkTypeDeclarations() []platform.LinkType {
	return definitionDeclarations(b.linkTypes)
}

func linkKeepsGuarantees(old, next LinkType) bool {
	before, after := old.Definition(), next.Definition()
	return before.Storage == after.Storage && !(before.Required && !after.Required || before.Cardinality != "many-to-many" && after.Cardinality == "many-to-many" || before.Cardinality == "one-to-one" && after.Cardinality != "one-to-one" || before.DeletePolicy == "restrict-active" && after.DeletePolicy != "restrict-active")
}

func (b *Build) migrateLinkType(c platform.Caller, submission *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, submission, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		record, ok := platform.Get[LinkType](c, submission.GetTarget().GetId())
		installed, retained := wasPublished[LinkType](record.Published)
		if !ok || record.Archived || !retained || installed.Definition().Storage != "edge" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Publish an owned relationship before migrating")
		}
		binding := platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetLinkType, Name: installed.Name}, SourceVersion: b.Manifest().Version + ".link-" + strconv.Itoa(installed.Version)}
		return func(receipt *pb.ChangeRecord) {
			if c.MigrateOwnedRelations(receipt, binding, at) != nil {
				return
			}
			_ = c.Put(receipt, record)
		}, nil
	})
}

// LinkPublication decodes the original accepted owner image without a
// new publication decision or a generic release declaration union.
func (b *Build) LinkPublication(image []byte) (platform.LinkType, error) {
	saved, err := linkTypeImage(image)
	if err != nil {
		return platform.LinkType{}, err
	}
	return saved.Definition(), nil
}
