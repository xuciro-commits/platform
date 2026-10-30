package build

import (
	"encoding/json"
	"fmt"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"

	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"
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
	return platform.Entity{Type: ObjectType, Title: "Object", Plural: "Objects", Model: Object{}, Display: "title",
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
	if err := CheckFields(o.Fields, b.fieldKnown); err != nil {
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

const BaseField = "base"

func CheckEnterpriseUse(o Object) error {
	if o.EnterpriseUse == nil {
		return nil
	}
	if o.EnterpriseUse.Kind != "boolean" {
		return fmt.Errorf("current enterprise use must return boolean")
	}
	refs := map[string]platform.ExprType{}
	for _, field := range o.Fields {
		refs[field.Name] = platform.ExprType{Kind: platform.RuleFieldKind(field.Type), Object: field.Ref}
	}
	if len(o.States) > 0 {
		refs["state"] = platform.ExprType{Kind: "choice"}
	}
	refs["archived"] = platform.ExprType{Kind: "boolean"}
	_, err := platform.CompileExpr(*o.EnterpriseUse, platform.ExprSchema{Refs: refs})
	return err
}

func CheckFields(fields []Field, known func(string) bool) error {
	seen := map[string]bool{}
	for _, f := range fields {
		switch {
		case !Named(f.Name):
			return fmt.Errorf("the field name %q is not lower-case letters and digits", f.Name)
		case seen[f.Name]:
			return fmt.Errorf("the field %q is declared twice", f.Name)
		case f.Name == "id" || f.Name == "state" || f.Name == "created" || f.Name == "changed" || f.Name == "revision" || f.Name == "archived":
			return fmt.Errorf("the field %q is the platform's own", f.Name)
		case f.Title == "":
			return fmt.Errorf("the field %q has no label", f.Name)
		case !slices.Contains(fieldTypes, f.Type):
			return fmt.Errorf("the field %q has no type %q", f.Name, f.Type)
		case f.Type == "choice" && strings.TrimSpace(f.Choices) == "":
			return fmt.Errorf("the choice field %q has no values", f.Name)
		case f.Type == "reference" && f.Ref == "":
			return fmt.Errorf("the reference field %q says nothing it refers to", f.Name)
		case f.Type == "reference" && f.Ref != "enterprise.element" && known != nil && !known(f.Ref):
			return fmt.Errorf("the field %q refers to %q, which this tenant has no object for", f.Name, f.Ref)
		case f.Ref == "enterprise.element" && f.Inverse != "":
			return fmt.Errorf("enterprise references do not declare inverse record relations")
		case f.Inverse != "" && f.Type != "reference":
			return fmt.Errorf("the field %q names how it is seen from elsewhere but refers to nothing", f.Name)
		case f.Inverse != "" && !Named(f.Inverse):
			return fmt.Errorf("the relation name %q of field %q is not lower-case letters and digits", f.Inverse, f.Name)
		case f.AliasOf != "" && (!Named(f.AliasOf) || f.AliasOf == f.Name || f.Formula != "" || f.FormulaExpr != nil):
			return fmt.Errorf("the field %q needs a direct read alias without a formula", f.Name)
		}
		seen[f.Name] = true
		if err := CheckEnterpriseReference(f.Type, f.Ref, f.Stereotype, f.Stereotypes); err != nil {
			return err
		}
	}
	return CheckConditions(fields)
}

func CheckEnterpriseReference(kind, ref, stereotype string, stereotypes []string) error {
	if ref == "enterprise.element" && kind != "reference" {
		return fmt.Errorf("enterprise element types require an enterprise reference")
	}
	if stereotype == "" && len(stereotypes) == 0 {
		return nil
	}
	if kind != "reference" || ref != "enterprise.element" {
		return fmt.Errorf("enterprise element types require an enterprise reference")
	}
	for _, value := range stereotypes {
		if value == "" {
			return fmt.Errorf("enterprise element types cannot be empty")
		}
	}
	for _, value := range append(slices.Clone(stereotypes), stereotype) {
		if value != "" && !slices.Contains([]string{"ActualOrganization", "ActualPerson", "ActualPost", "ActualLocation"}, value) {
			return fmt.Errorf("unsupported enterprise element type %q", value)
		}
	}
	return nil
}

func CheckConditions(fields []Field) error {
	for _, f := range fields {
		if f.When == "" {
			continue
		}
		condition, err := platform.ParseWhen(f.When)
		if err != nil {
			return fmt.Errorf("the field %q applies \"only when\" %q, which is not field=value,value", f.Name, f.When)
		}
		name := condition.Field
		i := slices.IndexFunc(fields, func(x Field) bool { return x.Name == name })
		switch {
		case i < 0 || name == f.Name:
			return fmt.Errorf("the field %q applies only when %q is set, which is not another field of this object", f.Name, name)
		case fields[i].When != "":
			return fmt.Errorf("the field %q depends on %q, which is conditional itself", f.Name, name)
		case fields[i].Type != "choice" && fields[i].Type != "boolean":
			return fmt.Errorf("the field %q depends on %q, which is neither a choice nor a boolean", f.Name, name)
		}
		allowed := []string{"true", "false"}
		if fields[i].Type == "choice" {
			allowed = ValueChoices(fields[i].Choices)
		}
		for _, v := range condition.In {
			if !slices.Contains(allowed, v) {
				return fmt.Errorf("the field %q depends on %q being %q, which it never is", f.Name, name, v)
			}
		}
	}
	return nil
}

var fieldTypes = []string{"text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference"}

func Named(s string) bool {
	if s == "" || !unicode.IsLetter(rune(s[0])) {
		return false
	}
	for _, r := range s {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func CheckObjectShape(o Object, model platform.Entity, declared []platform.Interface, known func(string) bool, lookup func(string) (platform.EntityInfo, bool)) error {
	if len(o.Implements) > 0 {
		info, err := platform.Describe(ID, model, func(reflect.Type) string { return "" })
		if err != nil {
			return err
		}
		for k, name := range o.Implements {
			if slices.Contains(o.Implements[:k], name) {
				return fmt.Errorf("the interface %s is listed twice", name)
			}
			i := slices.IndexFunc(declared, func(i platform.Interface) bool { return i.Name == name })
			if i < 0 {
				return fmt.Errorf("no app declares the interface %s", name)
			}
			if err := declared[i].Implements(info); err != nil {
				return err
			}
		}
	}
	if o.Extends != "" {
		if o.Extends == TypeOf(o.Name) || (known == nil || !known(o.Extends)) {
			return fmt.Errorf("the object cannot extend %s: not an object type this tenant has", o.Extends)
		}
		k := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == BaseField })
		if k < 0 || o.Fields[k].Type != "reference" || o.Fields[k].Ref != o.Extends || !o.Fields[k].Required {
			return fmt.Errorf("an extension of %s needs a required reference field %q pointing at it", o.Extends, BaseField)
		}
		if base, exists := lookup(o.Extends); exists {
			info, err := platform.Describe(ID, model, func(reflect.Type) string { return "" })
			if err != nil {
				return err
			}
			info.ExtensionSource = nil
			if err := platform.CheckExtension(info, base); err != nil {
				return err
			}
		}
	}
	return nil
}

func CompileObjectShape(o Object) platform.Entity {
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[platform.Record](), Anonymous: true,
		Tag: reflect.StructTag(fmt.Sprintf(`entity:"%s"`, TypeOf(o.Name)))}}
	for _, f := range o.Fields {
		tag := fmt.Sprintf(`json:"%s,omitempty" title:"%s"`, f.Name, f.Title)
		if f.AIInput != nil {
			if *f.AIInput {
				tag += ` ai:"allow"`
			} else {
				tag += ` ai:"deny"`
			}
		}
		if f.SourceType != "" {
			tag += fmt.Sprintf(` source:"%s"`, f.SourceType)
		}
		switch f.Type {
		case "longtext", "date", "datetime":
			tag += fmt.Sprintf(` type:"%s"`, f.Type)
		case "choice":
			tag += fmt.Sprintf(` choices:"%s"`, strings.Join(ValueChoices(f.Choices), ","))
		case "reference":
			tag += fmt.Sprintf(` ref:"%s"`, f.Ref)
			if f.Stereotype != "" {
				tag += fmt.Sprintf(` stereo:"%s"`, f.Stereotype)
			}
			if len(f.Stereotypes) > 0 {
				tag += fmt.Sprintf(` stereos:"%s"`, strings.Join(f.Stereotypes, ","))
			}
			if f.Inverse != "" {
				tag += fmt.Sprintf(` inverse:"%s"`, f.Inverse)
			}
		}
		var marks []string
		if f.Required {
			marks = append(marks, "required")
		}
		if f.Search {
			marks = append(marks, "search")
		}
		if f.Formula != "" || f.FormulaExpr != nil || f.ReadOnly || f.AliasOf != "" {
			marks = append(marks, "readonly")
		}
		if slices.ContainsFunc(o.VersionPins, func(pin platform.VersionPinBinding) bool { return pin.Version == f.Name }) && !slices.Contains(marks, "readonly") {
			marks = append(marks, "readonly")
		}
		if len(marks) > 0 {
			tag += fmt.Sprintf(` field:"%s"`, strings.Join(marks, ","))
		}
		if f.AliasOf != "" {
			tag += fmt.Sprintf(` alias:"%s"`, f.AliasOf)
		}
		if f.When != "" {
			tag += fmt.Sprintf(` when:"%s"`, f.When)
		}
		if len(f.Read) > 0 {
			tag += fmt.Sprintf(` read:"%s"`, strings.Join(append([]string{Builder}, f.Read...), ","))
		}
		if len(f.Write) > 0 {
			tag += fmt.Sprintf(` write:"%s"`, strings.Join(append([]string{Builder}, f.Write...), ","))
		}
		fieldType := goType(f.Type)
		if (!f.Required || f.FormulaExpr != nil) && (f.Type == "integer" || f.Type == "decimal" || f.Type == "boolean" || f.Type == "datetime" || f.Type == "money") {
			fieldType = reflect.PointerTo(fieldType)
		}
		fields = append(fields, reflect.StructField{Name: GoName(f.Name), Type: fieldType, Tag: reflect.StructTag(tag)})
	}
	var derived []platform.Derivation
	for _, f := range o.Fields {
		if f.SourceType == "generated" {
			name := "aisources" + f.Name
			fields = append(fields, reflect.StructField{Name: "AISources" + GoName(f.Name), Type: reflect.TypeFor[[]string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" type:"json" field:"readonly,aside"`, name))})
			derived = append(derived, platform.Derivation{From: name, Fields: []string{f.Name}})
		}
		if f.FormulaExpr != nil {
			name := "rulesources" + f.Name
			fields = append(fields, reflect.StructField{Name: "RuleSources" + GoName(f.Name), Type: reflect.TypeFor[[]string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s,omitempty" type:"json" field:"readonly,aside"`, name))})
			derived = append(derived, platform.Derivation{From: name, Fields: []string{f.Name}})
		}
	}
	withheld := ""
	if len(derived) > 0 {
		withheld = "rulewithheld"
		fields = append(fields, reflect.StructField{Name: "RuleWithheld", Type: reflect.TypeFor[bool](), Tag: reflect.StructTag(`json:"rulewithheld,omitempty" field:"readonly,aside"`)})
	}
	if len(o.States) > 0 {
		names := make([]string, 0, len(o.States))
		for _, st := range o.States {
			names = append(names, st.Name)
		}
		fields = append(fields, reflect.StructField{Name: "State", Type: reflect.TypeFor[string](),
			Tag: reflect.StructTag(fmt.Sprintf(`json:"state,omitempty" title:"State" field:"readonly" choices:"%s"`, strings.Join(names, ",")))})
	}
	model := reflect.New(reflect.StructOf(fields)).Elem().Interface()
	display := ""
	for _, f := range o.Fields {
		if display == "" && f.Search {
			display = f.Name
		}
	}
	localRules := map[string]platform.Expr{}
	for _, field := range o.Fields {
		if field.FormulaExpr != nil {
			localRules[field.Name] = *field.FormulaExpr
		}
	}
	return platform.Entity{LocalRules: localRules, Type: TypeOf(o.Name), Title: o.Title, Plural: o.Plural, Description: o.Description, Model: model, Display: display, Constraints: o.Constraints, Sets: o.Sets, Versioning: o.Versioning, VersionPins: o.VersionPins, Implements: slices.Clone(o.Implements), Extends: o.Extends, ExtensionSource: o.ExtensionSource, Derived: derived, Withheld: withheld, EnterpriseUse: o.EnterpriseUse}
}

func goType(kind string) reflect.Type {
	switch kind {
	case "integer":
		return reflect.TypeFor[int]()
	case "decimal":
		return reflect.TypeFor[float64]()
	case "money":
		return reflect.TypeFor[platform.Money]()
	case "datetime":
		return reflect.TypeFor[time.Time]()
	case "boolean":
		return reflect.TypeFor[bool]()
	}
	return reflect.TypeFor[string]()
}

func GoName(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}

func CompileObjectPage(o Object) platform.Page {
	names := []string{}
	for _, f := range o.Fields {
		names = append(names, f.Name)
	}
	list := slices.Clone(names)
	if len(list) > 4 {
		list = list[:4]
	}
	if len(o.States) > 0 {
		list, names = append(list, "state"), append(names, "state")
	}
	typ := TypeOf(o.Name)
	actions := []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: typ + ".create"}, {App: ID, Kind: platform.AssetAction, Name: typ + ".edit"}}
	for _, a := range o.Actions {
		actions = append(actions, platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: typ + "." + a.Name})
	}
	title := o.Plural
	if title == "" {
		title = o.Title
	}
	return platform.Page{Name: o.Name, Title: title, Description: o.Description, Layout: "list-detail",
		Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: typ}, ListFields: list, DetailFields: names, Actions: actions}
}

func (b *Build) checkPosts(o Object) error {
	for _, a := range o.Actions {
		for _, p := range a.Posts {
			where := fmt.Sprintf("the action %q posts to %q", a.Name, p.Object)
			if !strings.HasPrefix(p.Object, ID+".") {
				return fmt.Errorf("%s, which is another app's object; change it through that app's actions", where)
			}
			if p.Object == TypeOf(o.Name) {
				return fmt.Errorf("%s, which is this object itself", where)
			}
			if len(p.Match) == 0 {
				return fmt.Errorf("%s without a field that identifies the balance", where)
			}
			for _, m := range p.Match {
				if err := checkSource(o, a, m.From); err != nil {
					return fmt.Errorf("%s, match %q: %w", where, m.Field, err)
				}
			}
			if err := checkSource(o, a, p.Amount); err != nil {
				return fmt.Errorf("%s, amount: %w", where, err)
			}
			if b.host == nil {
				continue
			}
			info, known := b.lookupEntity(p.Object)
			if !known {
				return fmt.Errorf("%s, which this tenant has not published", where)
			}
			for _, m := range p.Match {
				if _, ok := info.Field(m.Field); !ok {
					return fmt.Errorf("%s and matches %q, which it has no field for", where, m.Field)
				}
			}
			f, ok := info.Field(p.Field)
			if !ok || f.Type != "integer" && f.Type != "decimal" && f.Type != "money" {
				return fmt.Errorf("%s into %q, which is not an integer, decimal or money field of it", where, p.Field)
			}
		}
	}
	return nil
}

func checkSource(o Object, a Action, from string) error {
	switch {
	case from == "$me", strings.HasPrefix(from, "="):
		return nil
	case strings.HasPrefix(from, "record."):
		name := strings.TrimPrefix(from, "record.")
		for _, f := range o.Fields {
			if f.Name == name {
				return nil
			}
		}
		return fmt.Errorf("%q is not a field of this object", name)
	}
	for _, in := range a.Inputs {
		if in.Name == from {
			return nil
		}
	}
	return fmt.Errorf("%q is not an input of the action", from)
}

func (b *Build) post(c platform.Caller, p Post, inputs map[string]any, record any, now time.Time) (any, *kernel.Error) {
	if b.host == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Postings need the host")
	}
	balance, known := b.installed[p.Object]
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{object} is not published", p.Object)
	}
	info, err := platform.Describe(ID, balance, func(reflect.Type) string { return "" })
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	own := map[string]any{}
	if raw, marshalErr := json.Marshal(record); marshalErr == nil {
		json.Unmarshal(raw, &own)
	}
	source := func(from string) any {
		switch {
		case from == "$me":
			return c.ID
		case strings.HasPrefix(from, "="):
			return strings.TrimPrefix(from, "=")
		case strings.HasPrefix(from, "record."):
			return own[strings.TrimPrefix(from, "record.")]
		}
		return inputs[from]
	}
	keys := make([]string, 0, len(p.Match))
	for _, m := range p.Match {
		keys = append(keys, platform.TextOf(source(m.From)))
	}
	id := platform.BalanceID(p.Object, keys)
	v := reflect.New(reflect.TypeOf(balance.Model)).Elem()
	if existing, ok := b.host.Record(p.Object + "/" + id); ok {
		if raw, marshalErr := json.Marshal(existing); marshalErr == nil {
			json.Unmarshal(raw, v.Addr().Interface())
		}
	} else {
		v.Field(0).Set(reflect.ValueOf(platform.Record{ID: id}))
	}
	for _, m := range p.Match {
		f, _ := info.Field(m.Field)
		x := source(m.From)
		if s, isText := x.(string); isText && strings.HasPrefix(m.From, "=") {
			x, _ = platform.Literal(f.Type, s)
		}
		if err := assign(v.FieldByName(GoName(m.Field)), x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The posting cannot match {field}: {why}", m.Field, err.Error())
		}
	}
	amount, amountErr := ValueNumber(source(p.Amount))
	if amountErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The posting's amount {amount} is not a number", p.Amount)
	}
	if p.Subtract {
		amount = -amount
	}
	f, _ := info.Field(p.Field)
	target := v.FieldByName(GoName(p.Field))
	if !target.IsValid() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The balance has no field {field}", p.Field)
	}
	var next float64
	switch f.Type {
	case "integer":
		next = float64(target.Int()) + amount
		if next != math.Trunc(next) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} counts whole units", p.Field)
		}
		target.SetInt(int64(next))
	case "decimal":
		next = target.Float() + amount
		target.SetFloat(next)
	case "money":
		m := target.Interface().(platform.Money)
		m.Amount += int64(math.Round(amount))
		if m.Currency == "" {
			if given, ok := source(p.Amount).(map[string]any); ok {
				m.Currency, _ = given["currency"].(string)
			}
		}
		next = float64(m.Amount)
		target.Set(reflect.ValueOf(m))
	default:
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is not a number field", p.Field)
	}
	if p.Floor && next < 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{field} would fall below zero", p.Field)
	}
	value := v.Interface()
	if err := c.Check(value); err != nil {
		return nil, err
	}
	return value, nil
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
		base := v.FieldByName(GoName(BaseField))
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
