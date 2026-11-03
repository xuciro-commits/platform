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
)

func (b *Build) checkCreates(o Object) error {
	for _, a := range o.Actions {
		for _, cr := range a.Creates {
			where := fmt.Sprintf("the action %q creates %q", a.Name, cr.Object)
			if !strings.HasPrefix(cr.Object, ID+".") {
				return fmt.Errorf("%s, which is another app's object; change it through that app's actions", where)
			}
			if cr.Object == TypeOf(o.Name) {
				return fmt.Errorf("%s, which is this object itself", where)
			}
			if b.host == nil {
				continue
			}
			info, known := b.lookupEntity(cr.Object)
			if !known {
				return fmt.Errorf("%s, which this tenant has not published", where)
			}
			via, ok := info.Field(cr.Via)
			if !ok || via.Type != "reference" || via.Ref != TypeOf(o.Name) {
				return fmt.Errorf("%s through %q, which is not its reference to %s", where, cr.Via, TypeOf(o.Name))
			}
			for _, set := range cr.Sets {
				if set.Field == cr.Via {
					return fmt.Errorf("%s and sets %q, which the action fills with this record", where, set.Field)
				}
				field, ok := info.Field(set.Field)
				if !ok {
					return fmt.Errorf("%s and sets %q, which it has no field for", where, set.Field)
				}
				for _, input := range a.Inputs {
					if input.Name == set.From && input.Type == "reference" && (field.Type != "reference" || field.Ref != input.Ref) {
						return fmt.Errorf("%s reference input %s does not match field %s", where, input.Name, field.Name)
					}
				}
			}
		}
	}
	return nil
}

func (b *Build) create(c platform.Caller, cr Create, inputs map[string]any, parentType, parent, id string, now time.Time) (any, *kernel.Error) {
	if b.host == nil || !strings.HasPrefix(cr.Object, ID+".") {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not an object of this builder", cr.Object)
	}
	related, known := b.installed[cr.Object]
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not published", cr.Object)
	}
	info, err := platform.Describe(ID, related, func(reflect.Type) string { return "" })
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if f, ok := info.Field(cr.Via); !ok || f.Type != "reference" || f.Ref != parentType {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} has no reference {field} to {parent}", cr.Object, cr.Via, parentType)
	}
	creators := related.Standard.CreateRoles
	if creators == nil {
		creators = related.Standard.Roles
	}
	if !related.Standard.Create || !c.Holds(c.App, Builder) && !c.May(c.App, "create", creators).Allow {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{member} may not create {object}", c.ID, cr.Object)
	}
	v := reflect.New(reflect.TypeOf(related.Model)).Elem()
	v.Field(0).Set(reflect.ValueOf(platform.Record{ID: id}))
	if err := assign(v.FieldByName(GoName(cr.Via)), parent); err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	for _, set := range cr.Sets {
		f, _ := info.Field(set.Field)
		var x any
		switch {
		case set.From == "$me":
			x = c.ID
		case set.From == "$now" && f.Type == "date":
			x = c.Today(now)
		case set.From == "$now":
			x = now.UTC()
		case strings.HasPrefix(set.From, "="):
			x, _ = platform.Literal(f.Type, strings.TrimPrefix(set.From, "="))
		default:
			given, ok := inputs[set.From]
			if !ok || given == nil {
				continue
			}
			x = given
		}
		if err := assign(v.FieldByName(GoName(set.Field)), x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{object} cannot set {field}: {why}", cr.Object, set.Field, err.Error())
		}
	}
	if related.Lifecycle != nil {
		v.FieldByName("State").SetString(related.Lifecycle.Initial)
	}
	value := v.Interface()
	if err := c.Check(value); err != nil {
		return nil, err
	}
	return value, nil
}

func relatedID(rec platform.Record, a Action, n int) string {
	return fmt.Sprintf("%s-%s-%d-%d", rec.ID, a.Name, rec.Revision, n+1)
}

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

func (b *Build) Check(o Object, id string, engines ...platform.RuleEngine) error {
	if err := b.checkName(o.Name, id); err != nil {
		return err
	}
	if err := b.checkPropertyFields(o.Fields); err != nil {
		return err
	}
	if err := b.checkValueTypeFields(o.Fields); err != nil {
		return err
	}
	if err := CheckFields(o.Fields, b.fieldKnown); err != nil {
		return err
	}
	if err := checkDataSources(o); err != nil {
		return err
	}
	if err := CheckEnterpriseUse(o); err != nil {
		return err
	}
	if err := checkProcess(o, b.conditionLookup); err != nil {
		return err
	}
	if err := b.checkObjectRules(o, engines...); err != nil {
		return err
	}
	if err := b.checkConstraints(o, engines...); err != nil {
		return err
	}
	if err := b.checkRecordSets(o, engines...); err != nil {
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
	if err := b.checkTransactions(o, engines...); err != nil {
		return err
	}
	if err := b.checkCreates(o); err != nil {
		return err
	}
	if err := b.checkActionPosts(o); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := b.checkPosts(o); err != nil {
		return err
	}
	if err := b.checkShape(o); err != nil {
		return err
	}
	if err := b.checkPresentation(o); err != nil {
		return err
	}
	return checkAccess(o)
}

func checkDataSources(o Object) error {
	seen := map[string]bool{}
	fields := map[string]bool{}
	for _, field := range o.Fields {
		fields[field.Name] = !field.ReadOnly && field.AliasOf == "" && field.Formula == "" && field.FormulaExpr == nil
	}
	for _, source := range o.DataSources {
		identity := source.Kind + "/" + source.Source
		if !slices.Contains([]string{"source", "pipeline", "dataset"}, source.Kind) || source.Source == "" || source.Key == "" || len(source.Columns) == 0 || seen[identity] {
			return fmt.Errorf("data source needs a unique dataset, source or pipeline, key column, and mapped columns")
		}
		seen[identity] = true
		columns := map[string]bool{}
		attributes := map[string]bool{}
		for _, column := range source.Columns {
			if column.Column == "" || column.Attribute == "" || !fields[column.Attribute] || columns[column.Column] || attributes[column.Attribute] ||
				column.Attribute == "id" {
				return fmt.Errorf("data source %s has an unknown, repeated or invalid column mapping", identity)
			}
			columns[column.Column] = true
			attributes[column.Attribute] = true
		}
	}
	return nil
}

func (b *Build) checkShape(o Object) error {
	if len(o.Implements) == 0 && o.Extends == "" {
		return nil
	}
	var declared []platform.Interface
	if len(o.Implements) > 0 && b.host != nil {
		declared = b.host.Interfaces()
	}
	return CheckObjectShape(o, Entity(o), declared, b.fieldKnown, b.lookupEntity)
}

func (b *Build) checkName(name, id string) error {
	if !Named(name) {
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
	var creationError error
	if creation == nil && o.Numbering != nil {
		creation, creationError = compileLegacyNumbering(o)
	}
	entity := CompileObjectShape(o)
	entity.DataSources = slices.Clone(o.DataSources)
	model := entity.Model
	std, scope, roles := access(o)
	entity.Standard, entity.Scope = std, scope
	entity.Lifecycle = lifecycle(o, roles, creates, lookup)
	entity.PropertyBindings = propertyBindings(o.Fields)
	entity.ComputeAt = computeRulesOf(o, lookup)
	entity.Validate = validateOf(o, model, lookup)
	entity.ValidateAt = numberingOf(o, model)
	entity.CreateTxn = creation
	entity.EnterpriseUse = o.EnterpriseUse
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
			return creates.businessRuntime().Transact(c, r, creation, now)
		}
	}
	return entity
}
