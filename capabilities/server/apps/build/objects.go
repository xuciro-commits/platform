package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (b *Build) objectEntity() platform.Entity {
	return platform.Entity{Type: construction.ObjectType, Title: "Object", Plural: "Objects", Model: construction.Object{}, Display: "title",
		Description: "An object this organisation defines itself: its name, what people call it and its fields. Publishing it installs it, and records of it are kept like any other app's.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			if err := b.normalizeObjectRules(record.(*construction.Object)); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder}, Capability: "objects"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being defined; nothing is installed yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Installed in this tenant: people work with its records."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"},
				Roles: []string{construction.Builder}, Capability: "objects", Payload: []platform.Field{},
				Description: "Install the object as it stands. Published again, it takes its new fields, and records keep the values of the fields that remain.",
				Do: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *kernel.Error {
					object, ok := record.(*construction.Object)
					if !ok {
						return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
					}
					if !c.Replaying {
						if err := b.captureExtensionSource(object); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
						if err := b.normalizeObjectRules(object); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
						if err := b.captureObjectSchema(object); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
					}
					if c.Staging() {
						if err := b.Check(*object, object.ID); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
						if err := b.validateObjectInstallation(*object); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
					} else if err := b.install(c, *object); err != nil {
						return err
					}
					object.Installed = construction.TypeOf(object.Name) // what its records are kept as, for people to see
					object.Published = published(*object)
					return nil
				}}}}}
}

func (b *Build) conditionLookup(typ string) (platform.EntityInfo, bool) {
	if b.host == nil {
		return platform.EntityInfo{}, false
	}
	return b.lookupEntity(typ)
}

func (b *Build) Check(o construction.Object, id string) error {
	if err := b.checkName(o.Name, id); err != nil {
		return err
	}
	if err := b.checkPropertyFields(o.Fields); err != nil {
		return err
	}
	if err := construction.CheckFields(o.Fields, b.fieldKnown); err != nil {
		return err
	}
	if err := checkProcess(o, b.conditionLookup); err != nil {
		return err
	}
	if err := b.checkObjectRules(o); err != nil {
		return err
	}
	if err := b.checkConstraints(o); err != nil {
		return err
	}
	if err := b.checkRecordSets(o); err != nil {
		return err
	}
	if err := b.checkVersioning(o); err != nil {
		return err
	}
	if err := checkFormulas(o.Fields); err != nil {
		return err
	}
	if err := checkScope(o); err != nil {
		return err
	}
	if err := b.checkTransactions(o); err != nil {
		return err
	}
	if err := b.checkCreates(o); err != nil {
		return err
	}
	if err := b.checkJournal(o); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := b.checkPosts(o); err != nil {
		return err
	}
	if err := b.checkShape(o); err != nil {
		return err
	}
	return checkAccess(o)
}

// BaseField is the reference an extension object carries to the record it extends.
const BaseField = construction.BaseField

// checkShape refuses an interface the tenant's apps do not declare or whose
// fields the object lacks, and an extension of a type that is not there, of
// itself, with a lifecycle of its own, or whose base field is not the base.
func (b *Build) checkShape(o construction.Object) error {
	if len(o.Implements) == 0 && o.Extends == "" {
		return nil
	}
	var declared []platform.Interface
	if len(o.Implements) > 0 && b.host != nil {
		declared = b.host.Interfaces()
	}
	return construction.CheckObjectShape(o, Entity(o), declared, b.fieldKnown, b.lookupEntity)
}

// checkName refuses a name that is not a name, or one already taken.
func (b *Build) checkName(name, id string) error {
	if !construction.Named(name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", name)
	}
	other, taken, err := b.taken(name, id)
	if err != nil {
		return err
	}
	if taken {
		return fmt.Errorf("the name %q is already %s", name, other)
	}
	return nil
}

// fieldKnown answers whether a reference names an object this tenant can
// compose against: an installed one, one selected as a draft in the candidate
// being built, or another saved draft object. Delivery still
// proves the object installs.
func (b *Build) fieldKnown(ref string) bool {
	if ref == "enterprise.element" {
		return true
	}
	if _, known := b.lookupEntity(ref); known {
		return true
	}
	if b.host == nil {
		return false
	}
	if b.host.Declares(ref) {
		return true
	}
	name, derived := strings.CutPrefix(ref, construction.ID+".")
	if !derived {
		return false
	}
	objects, err := readDefinitionInventory[construction.Object](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(objects, func(o construction.Object) bool { return !o.Archived && o.Name == name })
}

// taken says whether another object, or an app's own type, holds the name.
func (b *Build) taken(name, id string) (string, bool, error) {
	if b.host == nil {
		return "", false, nil
	}
	if _, mine := b.installed[construction.TypeOf(name)]; !mine && b.host.Declares(construction.TypeOf(name)) {
		return "an app's own type", true, nil
	}
	objects, err := readDefinitionInventory[construction.Object](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return "", false, err
	}
	for _, o := range objects {
		if o.Name == name && o.ID != id && !o.Archived {
			return "the object " + o.Title, true, nil
		}
	}
	return "", false, nil
}

// install declares the object in the running host: the record store keeps its
// records, the ledger takes its generated actions, and the asset registry
// offers it and its page. Installed again, it takes its new
// fields and its records come with it (D3).
func (b *Build) install(c platform.Caller, o construction.Object) *kernel.Error {
	if c.Staging() {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	// A restore installs definitions that were already accepted, in ID order;
	// objects that refer to each other (a visit creating follow-ups that refer
	// back) cannot each pass the draft checks before the other is installed.
	if err := b.Check(o, o.ID); err != nil && !c.Replaying {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	// Historical direct publication and pure recovery validate before extending
	// the ledger. New decisions validate privately and install only their saved
	// publication image after acceptance.
	if err := b.validateObjectInstallation(o); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	entity := b.entity(o)
	actions := objectActions(o, entity)
	pages, _, err := b.objectInstallationPages(o)
	if err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.ledger.Extend([]string{entity.Type}, actions); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	b.ledger.Catalog.ReplaceTarget(entity.Type, actions)
	if err := b.host.Install(c, entity, actions, pages...); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	b.installed[entity.Type] = entity
	return nil
}

// Entity is the declaration a defined object amounts to: a Go type built now,
// with the tags a developer would have written.
func Entity(o construction.Object) platform.Entity { return entityWith(o, nil, nil) }

// entity is the declaration as installed: its actions may create related
// records through this builder's host.
func (b *Build) entity(o construction.Object) platform.Entity {
	return entityWith(o, b, b.conditionLookup)
}

func entityWith(o construction.Object, creates creator, lookup func(string) (platform.EntityInfo, bool)) platform.Entity {
	creation := o.CreateTransaction
	var creationError error
	if creation == nil && o.Numbering != nil {
		creation, creationError = compileLegacyNumbering(o)
	}
	entity := construction.CompileObjectShape(o)
	model := entity.Model
	std, scope, roles := access(o)
	entity.Standard, entity.Scope = std, scope
	entity.Lifecycle = lifecycle(o, roles, creates, lookup)
	entity.PropertyBindings = propertyBindings(o.Fields)
	entity.ComputeAt = computeRulesOf(o, lookup)
	entity.Validate = validateOf(o, model, lookup)
	entity.ValidateAt = numberingOf(o, model)
	entity.CreateTxn = creation
	entity.Protocols = objectProvisions(o, entity)
	if creationError != nil {
		legacy := entity.ValidateAt
		entity.ValidateAt = func(c platform.Caller, record any, now time.Time) *kernel.Error {
			if c.Replaying && legacy != nil {
				return legacy(c, record, now)
			}
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", creationError.Error())
		}
	}
	if creation != nil {
		if legacy := entity.ValidateAt; legacy != nil {
			entity.ValidateAt = func(c platform.Caller, record any, now time.Time) *kernel.Error {
				if c.Replaying {
					return legacy(c, record, now)
				}
				return nil
			}
		}
		entity.ApplyCreateTxn = func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error {
			if creates == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Creation transactions need their original owner")
			}
			return creates.transact(c, r, creation, now)
		}
	}
	return entity
}
