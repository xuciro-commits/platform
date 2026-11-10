package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

func (b *Build) captureExtensionSource(object *Object) error {
	if object.Extends == "" {
		object.ExtensionSource = nil
		return b.checkExtensionDependents(*object)
	}
	base, exists := b.lookupEntity(object.Extends)
	if !exists || b.host == nil {
		return fmt.Errorf("extension base %s is unavailable", object.Extends)
	}
	binding, exists := b.host.EntitySource(object.Extends)
	if !exists && base.App == ID {
		if _, selected := b.jointEntities[base.Type]; selected {
			binding, exists = platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: base.Type}, SourceVersion: b.Manifest().Version}, true
		}
	}
	if !exists {
		return fmt.Errorf("extension base %s has no declared package version", object.Extends)
	}
	raw, err := json.Marshal(platform.ExtensionSource{Base: binding, Fields: base.Fields})
	if err != nil {
		return err
	}
	var source platform.ExtensionSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return err
	}
	object.ExtensionSource = &source
	info, err := platform.Describe(ID, Entity(*object), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	return platform.CheckExtension(info, base)
}

func (b *Build) checkExtensionDependents(object Object) error {
	objects, _, _, err := b.releaseInventory()
	if err != nil {
		return err
	}
	var base platform.EntityInfo
	for _, record := range objects {
		published, exists := ReadPublication[Object](record.Published)
		if !exists || published.Extends != ID+"."+object.Name {
			continue
		}
		if base.Type == "" {
			base, err = platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
			if err != nil {
				return err
			}
		}
		extension, err := platform.Describe(ID, Entity(published), func(reflect.Type) string { return "" })
		if err != nil {
			return err
		}
		if next, selected := b.jointEntities[extension.Type]; selected {
			extension = next
			extension.ExtensionSource = nil
		}
		if err := platform.CheckExtension(extension, base); err != nil {
			return err
		}
	}
	return nil
}

func (b *Build) objectEntity() platform.Entity {
	return platform.Entity{Type: ObjectType, Title: "Object type", Plural: "Object types", Model: Object{}, Display: "title",
		Description: "An object this organisation defines itself: its name, what people call it and its fields. Publishing it installs it, and records of it are kept like any other app's.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			if err := b.normalizeObjectRules(record.(*Object)); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "objects"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being defined; nothing is installed yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Installed in this tenant: people work with its records."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"},
				Roles: []string{Builder}, Capability: "objects", Payload: []platform.Field{},
				Description: "Install the object as it stands. Published again, it takes its new fields, and records keep the values of the fields that remain.",
				Do: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *kernel.Error {
					object, ok := record.(*Object)
					if !ok {
						return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
					}
					compiled := *object
					if !c.Replaying {
						if err := b.bindObjectActions(c, &compiled); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
					}
					object = &compiled
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
						if err := b.Check(*object, object.ID, c.Rules()); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
						if err := b.validateObjectInstallation(c, *object); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
					} else if err := b.install(c, *object); err != nil {
						return err
					}
					object.Installed = TypeOf(object.Name)
					object.Published = published(*object)
					if len(object.ActionTypes) > 0 {
						object.Actions = nil
						for i := range object.ActionTypes {
							object.ActionTypes[i].SourceVersion = ""
						}
					}
					*record.(*Object) = *object
					return nil
				}}}}}
}

func (b *Build) conditionLookup(typ string) (platform.EntityInfo, bool) {
	if b.host == nil {
		return platform.EntityInfo{}, false
	}
	return b.lookupEntity(typ)
}

func (b *Build) Check(o Object, id string, engine platform.RuleEngine) error {
	if err := b.checkName(o.Name, id); err != nil {
		return err
	}
	if err := b.checkPropertyFields(o.Fields); err != nil {
		return err
	}
	if err := b.checkValueTypeFields(o.Fields); err != nil {
		return err
	}
	if err := ontology.CheckFields(o.Fields, b.fieldKnown); err != nil {
		return err
	}
	if err := ontology.CheckDataSources(o); err != nil {
		return err
	}
	if err := ontology.CheckEnterpriseUse(o, engine); err != nil {
		return err
	}
	if err := ontology.CheckProcess(o, ID, TypeOf(o.Name), Builder, User, FunctionCallType, b.conditionLookup); err != nil {
		return err
	}
	if err := b.checkObjectRules(o, engine); err != nil {
		return err
	}
	if err := b.checkConstraints(o, engine); err != nil {
		return err
	}
	if err := b.checkRecordSets(o, engine); err != nil {
		return err
	}
	if err := b.checkVersioning(o); err != nil {
		return err
	}
	if err := ontology.CheckFormulas(o.Fields); err != nil {
		return err
	}
	if err := ontology.CheckScope(o); err != nil {
		return err
	}
	if err := b.checkTransactions(o, engine); err != nil {
		return err
	}
	if err := b.checkShape(o); err != nil {
		return err
	}
	if err := b.checkPresentation(o); err != nil {
		return err
	}
	return ontology.CheckAccess(o, Builder, User)
}

func (b *Build) checkShape(o Object) error {
	if len(o.Implements) == 0 && o.Extends == "" {
		return nil
	}
	declared := b.declaredInterfaces()
	return ontology.CheckObjectShape(o, ID, TypeOf(o.Name), Entity(o), declared, b.fieldKnown, b.lookupEntity)
}

func (b *Build) checkName(name, id string) error {
	if !ontology.Named(name) {
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
	name, derived := strings.CutPrefix(ref, ID+".")
	if !derived {
		return false
	}
	objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(objects, func(o Object) bool { return !o.Archived && o.Name == name })
}

func (b *Build) taken(name, id string) (string, bool, error) {
	if b.host == nil {
		return "", false, nil
	}
	if _, mine := b.installed[TypeOf(name)]; !mine && b.host.Declares(TypeOf(name)) {
		return "an app's own type", true, nil
	}
	objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
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

func (b *Build) install(c platform.Caller, o Object) *kernel.Error {
	if c.Staging() {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if !c.Replaying {
		if err := b.Check(o, o.ID, c.Rules()); err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	}
	if err := b.validateObjectInstallation(c, o); err != nil {
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

func Entity(o Object) platform.Entity { return entityWith(o, nil, nil) }

func (b *Build) entity(o Object) platform.Entity {
	return entityWith(o, b, b.conditionLookup)
}

func entityWith(o Object, creates creator, lookup func(string) (platform.EntityInfo, bool)) platform.Entity {
	creation := o.CreateTransaction
	entity := ontology.CompileObjectShape(o, ID, Builder)
	entity.DataSources = slices.Clone(o.DataSources)
	model := entity.Model
	std, scope, roles := ontology.CompileAccess(o, Builder, User)
	entity.Standard, entity.Scope = std, scope
	entity.Lifecycle = lifecycle(o, roles, creates, lookup)
	entity.ComputeAt = computeRulesOf(o, lookup)
	entity.Validate = validateOf(o, model, lookup)
	entity.CreateTxn = creation
	entity.EnterpriseUse = o.EnterpriseUse
	entity.Protocols = objectProvisions(o, entity)
	if creation != nil {
		entity.ApplyCreateTxn = func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error {
			if creates == nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Creation transactions need their original owner")
			}
			return creates.businessRuntime().Transact(c, r, creation, now)
		}
	}
	return entity
}

func validateOf(o Object, model any, lookup func(string) (platform.EntityInfo, bool)) func(c platform.Caller, record any) *kernel.Error {
	if o.Extends == "" {
		return nil
	}
	typ := reflect.TypeOf(model)
	return func(c platform.Caller, record any) *kernel.Error {
		if !c.Replaying && lookup != nil {
			base, exists := lookup(o.Extends)
			if !exists {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Extension base {type} is unavailable", o.Extends)
			}
			shape := platform.EntityInfo{Type: TypeOf(o.Name), Extends: o.Extends, ExtensionSource: o.ExtensionSource}
			for _, field := range o.Fields {
				shape.Fields = append(shape.Fields, platform.FieldInfo{Name: field.Name, Type: field.Type, Ref: field.Ref, Required: field.Required, ReadOnly: field.ReadOnly, AliasOf: field.AliasOf})
			}
			if err := platform.CheckExtension(shape, base); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		v := reflect.ValueOf(record).Elem()
		base := v.FieldByName(ontology.GoName(BaseField))
		if !base.IsValid() || base.String() == "" {
			return nil
		}
		id := v.Field(0).Interface().(platform.Record).ID
		domain, _ := json.Marshal([][]any{{BaseField, "=", base.String()}})
		found, _, err := c.FindOf(typ, platform.Query{Domain: domain})
		if err != nil {
			return nil
		}
		for _, f := range found {
			if other := reflect.ValueOf(f).Field(0).Interface().(platform.Record); other.ID != id && !other.Archived {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{base} already has its {object} record", base.String(), o.Title)
			}
		}
		return nil
	}
}
