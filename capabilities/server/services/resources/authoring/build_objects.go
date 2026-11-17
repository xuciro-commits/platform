package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
	ontology "platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

func (b *Build) captureExtensionSource(object *ontology.Object) error {
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

func (b *Build) checkExtensionDependents(object ontology.Object) error {
	objects, _, _, err := b.releaseInventory()
	if err != nil {
		return err
	}
	var base platform.EntityInfo
	for _, record := range objects {
		published, exists := publication.Read[ontology.Object](record.Published)
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
	return ontology.ObjectEntity(ontology.ObjectEntityEnvironment{
		NormalizeRules:   b.normalizeObjectRules,
		BindActions:      b.bindObjectActions,
		CaptureExtension: b.captureExtensionSource,
		CaptureSchema:    b.captureObjectSchema,
		Check:            b.Check,
		ValidateInstallation: func(c platform.Caller, object ontology.Object) error {
			return b.validateObjectInstallation(c, object)
		},
		Install: b.install,
	})
}

func (b *Build) conditionLookup(typ string) (platform.EntityInfo, bool) {
	if b.host == nil {
		return platform.EntityInfo{}, false
	}
	return b.lookupEntity(typ)
}

func (b *Build) Check(o ontology.Object, id string, engine platform.RuleEngine) error {
	if err := b.checkName(o.Name, id); err != nil {
		return err
	}
	if err := ontology.CheckPropertyFields(o.Fields, ID, b.host); err != nil {
		return err
	}
	if err := ontology.CheckValueTypeFields(o.Fields, ID, b.host); err != nil {
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
	if err := ontology.CheckProcess(o, ID, TypeOf(o.Name), Builder, User, functionauthoring.FunctionCallType, b.conditionLookup); err != nil {
		return err
	}
	if err := b.checkObjectRules(o, engine); err != nil {
		return err
	}
	if err := ontology.CheckObjectConstraints(o, Entity(o), b.conditionLookup, b.checkRuleCollections, engine); err != nil {
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

func (b *Build) checkShape(o ontology.Object) error {
	if len(o.Implements) == 0 && o.Extends == "" {
		return nil
	}
	declared := b.declaredInterfaces()
	return ontology.CheckObjectShape(o, ID, TypeOf(o.Name), Entity(o), declared, b.fieldKnown, b.lookupEntity)
}

func (b *Build) checkName(name, id string) error {
	if !publication.ValidName(name) {
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
	objects, err := readDefinitionInventory[ontology.Object](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(objects, func(o ontology.Object) bool { return !o.Archived && o.Name == name })
}

func (b *Build) taken(name, id string) (string, bool, error) {
	if b.host == nil {
		return "", false, nil
	}
	if _, mine := b.installed[TypeOf(name)]; !mine && b.host.Declares(TypeOf(name)) {
		return "an app's own type", true, nil
	}
	objects, err := readDefinitionInventory[ontology.Object](b.host.Automation(platform.Caller{}, ID))
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

func (b *Build) install(c platform.Caller, o ontology.Object) *platform.Error {
	if c.Staging() {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if !c.Replaying {
		if err := b.Check(o, o.ID, c.Rules()); err != nil {
			return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	}
	if err := b.validateObjectInstallation(c, o); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	entity := b.entity(o)
	actions := objectActions(o, entity)
	pages, _, err := b.objectInstallationPages(o)
	if err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.ledger.Extend([]string{entity.Type}, actions); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	b.ledger.Catalog().ReplaceTarget(entity.Type, actions)
	if err := b.host.Install(c, entity, actions, pages...); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	b.installed[entity.Type] = entity
	return nil
}

func Entity(o ontology.Object) platform.Entity {
	return ontology.CompileObjectEntity(o, ontology.ObjectRuntimeEnvironment{})
}

func (b *Build) entity(o ontology.Object) platform.Entity {
	entity := ontology.CompileObjectEntity(o, ontology.ObjectRuntimeEnvironment{
		Lookup: b.conditionLookup,
		Transaction: func(c platform.Caller, r *pb.ChangeRecord, transaction *platform.BusinessTxn, now time.Time) *platform.Error {
			return b.businessRuntime().Transact(c, r, transaction, now)
		},
	})
	entity.DataSources = slices.Clone(o.DataSources)
	entity.ComputeAt = computeRulesOf(o, b.conditionLookup)
	entity.Protocols = b.objectProvisions(o, entity)
	return entity
}
