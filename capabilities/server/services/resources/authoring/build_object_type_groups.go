package build

import (
	"fmt"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const ObjectTypeGroupType = "build.objecttypegroup"
const SchemaObjectTypeGroup = ObjectTypeGroupType + ".publish"

type ObjectTypeGroup struct {
	platform.Record
	Name        string              `json:"name" field:"required,search"`
	Title       string              `json:"title" field:"required,search"`
	Description string              `json:"description,omitempty" type:"longtext"`
	Members     []platform.AssetRef `json:"members" type:"json"`
	State       string              `json:"state" field:"readonly" choices:"draft,published"`
	Version     int                 `json:"version,omitempty" field:"readonly"`
	Published   string              `json:"published,omitempty" field:"readonly" type:"longtext"`
	Versions    []string            `json:"versions,omitempty" field:"readonly"`
}

func (g ObjectTypeGroup) Definition() platform.ObjectTypeGroup {
	return platform.ObjectTypeGroup{Name: g.Name, Title: g.Title, Description: g.Description, Members: append([]platform.AssetRef{}, g.Members...)}
}

func (b *Build) checkObjectTypeGroup(g ObjectTypeGroup, published bool) error {
	if !Named(g.Name) {
		return fmt.Errorf("Object type group name must use lower-case letters and digits")
	}
	if err := g.Definition().Check(); err != nil {
		return err
	}
	objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return err
	}
	for _, member := range g.Members {
		if info, ok := b.lookupEntity(member.Name); ok && info.App == member.App {
			continue
		}
		if !published && member.App == ID && slices.ContainsFunc(objects, func(o Object) bool {
			return !o.Archived && TypeOf(o.Name) == member.Name
		}) {
			continue
		}
		if published {
			return fmt.Errorf("Object type group member %s needs a published object type", member)
		}
		return fmt.Errorf("Object type group member %s is missing or archived", member)
	}
	return nil
}

func (b *Build) objectTypeGroupEntity() platform.Entity {
	entity := objectTypeGroupKind.entity(b)
	entity.Validate = func(c platform.Caller, record any) *kernel.Error {
		if !c.Replaying {
			if err := b.checkObjectTypeGroup(*record.(*ObjectTypeGroup), false); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
		return nil
	}
	return entity
}

var objectTypeGroupKind = resourceKind[ObjectTypeGroup, platform.ObjectTypeGroup]{
	asset: platform.AssetObjectTypeGroup, typ: ObjectTypeGroupType, prefix: "objecttypegroup", noun: "objectTypeGroup", title: "Object type group", plural: "Object type groups", capability: "objecttypegroups", joint: true,
	description: "A group of object types for organization and browsing.",
	transition:  "Publish this object type group as its next retained version.",
	immutable:   "A published object type group keeps its name",
	records:     func(b *Build) map[string]ObjectTypeGroup { return b.objectTypeGroups },
	validate: func(b *Build, g ObjectTypeGroup) (ObjectTypeGroup, error) {
		return g, b.checkObjectTypeGroup(g, true)
	},
	identity: func(a, b ObjectTypeGroup) bool { return a.Name == b.Name },
	fromDeclaration: func(g platform.ObjectTypeGroup) ObjectTypeGroup {
		return ObjectTypeGroup{Name: g.Name, Title: g.Title, Description: g.Description, Members: slices.Clone(g.Members)}
	},
	install: func(h platform.BuildHost, c platform.Caller, g platform.ObjectTypeGroup, version int) error {
		return h.InstallObjectTypeGroup(c, g, version)
	},
	validateInstall: func(h platform.BuildHost, g platform.ObjectTypeGroup) error {
		return h.ValidateInstallObjectTypeGroup(g)
	},
}
