package build

import (
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

func validateLinkType(b *Build, f LinkType) (LinkType, error) {
	if !Named(f.Name) || !Named(f.Forward) || !Named(f.Reverse) {
		return f, fmt.Errorf("Link names must use lower-case letters and digits")
	}
	parent, parentFound := b.lookupEntity(f.Parent)
	info, childFound := b.lookupEntity(f.Child)
	_, parentInstalled := b.installed[f.Parent]
	_, childInstalled := b.installed[f.Child]
	_, parentSelected := b.jointEntities[f.Parent]
	_, childSelected := b.jointEntities[f.Child]
	if !parentFound || parent.App != ID || !parentInstalled && !parentSelected {
		return f, fmt.Errorf("Link parent needs this builder's published or jointly selected object")
	}
	if !childFound || info.App != ID || !childInstalled && !childSelected {
		return f, fmt.Errorf("Link child needs this builder's published or jointly selected object")
	}
	if f.Storage != "edge" {
		field, ok := info.Field(f.Via)
		if !ok {
			return f, fmt.Errorf("Link reference is unavailable")
		}
		f.Required = field.Required
	}
	if err := f.Definition().CheckSchema(parent, info); err != nil {
		return f, err
	}
	if !b.jointMode {
		if err := b.host.ValidateInstallLinkType(f.Definition()); err != nil {
			return f, err
		}
	}
	return f, nil
}

var linkKind = resourceKind[LinkType, platform.LinkType]{
	asset: platform.AssetLinkType, typ: LinkTypeType, prefix: "link", noun: "linkType", title: "Link type", plural: "Link types", capability: "linktypes",
	description: "A versioned relationship over an original reference; drafts do not change installed traversal.",
	transition:  "Install this reference-backed relationship as its next retained version.",
	immutable:   "A published link keeps its identity, objects, reference shape and retained constraints",
	records:     func(b *Build) map[string]LinkType { return b.linkTypes },
	validate:    validateLinkType,
	identity: func(a, b LinkType) bool {
		return a.Name == b.Name && a.Parent == b.Parent && a.Child == b.Child && a.Via == b.Via
	},
	stable: func(old, next LinkType) bool {
		return old.Definition().Storage != "reference" || old.Required == next.Required
	},
	guarantees:      linkKeepsGuarantees,
	adopt:           func(record *LinkType, frozen LinkType) { record.Required = frozen.Required },
	fromDeclaration: linkTypeFromDeclaration,
	contract:        func(d platform.LinkType) int { return d.Contract() },
	admit:           func(d platform.LinkType) bool { return d.Parent.App == ID && d.Child.App == ID },
	install: func(h platform.BuildHost, c platform.Caller, d platform.LinkType, version int) error {
		return h.InstallLinkType(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.LinkType) error { return h.ValidateInstallLinkType(d) },
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
	saved, err := linkKind.image(image)
	if err != nil {
		return platform.LinkType{}, err
	}
	return saved.Definition(), nil
}

func linkKeepsGuarantees(old, next LinkType) bool {
	before, after := old.Definition(), next.Definition()
	return before.Storage == after.Storage && !(before.Required && !after.Required || before.Cardinality != "many-to-many" && after.Cardinality == "many-to-many" || before.Cardinality == "one-to-one" && after.Cardinality != "one-to-one" || before.DeletePolicy == "restrict-active" && after.DeletePolicy != "restrict-active")
}

func linkTypeFromDeclaration(l platform.LinkType) LinkType {
	return LinkType{Name: l.Name, Title: l.Title, Description: l.Description, Parent: l.Parent.Name, Child: l.Child.Name, Via: l.Via, Forward: l.Forward, Reverse: l.Reverse, Required: l.Required, Storage: l.Storage, Roles: slices.Clone(l.Roles), Cardinality: l.Cardinality, DeletePolicy: l.DeletePolicy}
}

const LinkTypeType = "build.linktype"

const SchemaLinkType = LinkTypeType + ".publish"
