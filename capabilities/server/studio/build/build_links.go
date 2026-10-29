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
	if !Named(f.Name) || !Named(f.Forward) || !Named(f.Reverse) {
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
		installed, _ := ReadPublication[LinkType](other.Published)
		if other.ID != f.ID && (other.Name == f.Name || installed.Name == f.Name) {
			return fmt.Errorf("Link type %s is already declared", f.Name)
		}
	}
	if old, ok := ReadPublication[LinkType](f.Published); ok && (old.Name != f.Name || old.Parent != f.Parent || old.Child != f.Child || old.Via != f.Via || old.Definition().Storage == "reference" && old.Required != f.Required || !linkKeepsGuarantees(old, *f)) {
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
	return publishRetainedDefinition(c, f, "A link type may retain at most 64 published versions", b.installLinkType)
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

func linkKeepsGuarantees(old, next LinkType) bool {
	before, after := old.Definition(), next.Definition()
	return before.Storage == after.Storage && !(before.Required && !after.Required || before.Cardinality != "many-to-many" && after.Cardinality == "many-to-many" || before.Cardinality == "one-to-one" && after.Cardinality != "one-to-one" || before.DeletePolicy == "restrict-active" && after.DeletePolicy != "restrict-active")
}

func (b *Build) migrateLinkType(c platform.Caller, submission *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, submission, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		record, ok := platform.Get[LinkType](c, submission.GetTarget().GetId())
		installed, retained := ReadPublication[LinkType](record.Published)
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

func (b *Build) LinkPublication(image []byte) (platform.LinkType, error) {
	saved, err := linkTypeImage(image)
	if err != nil {
		return platform.LinkType{}, err
	}
	return saved.Definition(), nil
}

func linkTypeReleaseAsset(f LinkType, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetLinkType, Name: f.Name}, sourceVersion+".link-"+strconv.Itoa(f.Version), f.Definition())
}

func (b *Build) LinkTypeReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "link", "link type", platform.AssetLinkType, name, sourceVersion, b.LinkTypeDefinition)
}

func (b *Build) linkTypeAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.linkTypes, func(record LinkType) (platform.ReleaseAsset, error) {
		return linkTypeReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) InstallLinkTypeAsset(asset platform.ReleaseAsset) error {
	version := b.definitionAssetVersion(asset, platform.AssetLinkType, "link")
	var declaration platform.LinkType
	if version == 0 || json.Unmarshal(asset.Body, &declaration) != nil || asset.ContractVersion != declaration.Contract() || declaration.Check() != nil || declaration.Name != asset.Ref.Name || declaration.Parent.App != ID || declaration.Child.App != ID {
		return fmt.Errorf("invalid builder link type asset %s", asset.Ref)
	}
	f := linkTypeFromDeclaration(declaration)
	if _, exists := b.linkTypes[f.Name]; exists {
		return fmt.Errorf("linkType asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installLinkType(platform.Caller{Replaying: true}, f)
}

func (b *Build) linkTypeDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetLinkType, "link type", b.linkTypeInventory, func(record LinkType) (LinkType, error) {
		err := b.checkLinkType(&record)
		return record, err
	}, func(record LinkType) (platform.ReleaseAsset, error) {
		record.Version++
		return linkTypeReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) prepareLinkTypeReleasePublications(assets []platform.ReleaseAsset) ([]platform.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.linkTypeInventory, platform.AssetLinkType, "link", SchemaLinkType, "link type", b.LinkTypeReleaseAsset, func(record LinkType, asset platform.ReleaseAsset, version int) (LinkType, error) {
		prior, retained := ReadPublication[LinkType](record.Published)
		var q platform.LinkType
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Parent.Name != record.Parent || q.Child.Name != record.Child || q.Via != record.Via || retained && !linkKeepsGuarantees(prior, linkTypeFromDeclaration(q)) {
			return record, fmt.Errorf("invalid next link type version")
		}
		frozen := linkTypeFromDeclaration(q)
		frozen.Record = record.Record
		frozen.State = "published"
		frozen.Version = version
		record.Required = frozen.Required
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}

func linkTypeFromDeclaration(l platform.LinkType) LinkType {
	return LinkType{Name: l.Name, Title: l.Title, Description: l.Description, Parent: l.Parent.Name, Child: l.Child.Name, Via: l.Via, Forward: l.Forward, Reverse: l.Reverse, Required: l.Required, Storage: l.Storage, Roles: slices.Clone(l.Roles), Cardinality: l.Cardinality, DeletePolicy: l.DeletePolicy}
}

const LinkTypeType = "build.linktype"

const SchemaLinkType = LinkTypeType + ".publish"
