package ontology

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func BusinessVersionInstallOrder(objects []Object) ([]Object, error) {
	byType := map[string]Object{}
	for _, record := range objects {
		if object, ok := publication.Read[Object](record.Published); ok && !record.Archived {
			byType["build."+object.Name] = object
		}
	}
	status := map[string]int{}
	ordered := []Object{}
	var visit func(string) error
	visit = func(typ string) error {
		if status[typ] == 2 {
			return nil
		}
		if status[typ] == 1 {
			return fmt.Errorf("business version pin definitions form a cycle")
		}
		object, known := byType[typ]
		if !known {
			return nil
		}
		status[typ] = 1
		for _, pin := range object.VersionPins {
			for _, field := range object.Fields {
				if field.Name == pin.Reference {
					if err := visit(field.Ref); err != nil {
						return err
					}
				}
			}
		}
		status[typ] = 2
		ordered = append(ordered, object)
		return nil
	}
	for _, record := range objects {
		if err := visit("build." + record.Name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func DefinitionBody(value Object, typ string) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for _, key := range []string{"id", "revision", "created", "changed", "archived", "state", "installed", "published"} {
		delete(body, key)
	}
	body["type"], err = json.Marshal(typ)
	if err != nil {
		return nil, err
	}
	return json.Marshal(body)
}

func ObjectSchemaBody(object Object) (json.RawMessage, error) {
	object.SchemaVersions = nil
	return DefinitionBody(object, AppID+"."+object.Name)
}

func CheckObjectSchemas(object Object) error {
	if len(object.SchemaVersions) == 0 {
		return nil
	}
	if len(object.SchemaVersions) > 64 {
		return fmt.Errorf("object schema version budget exceeded")
	}
	family := uint32(1)
	for index, binding := range object.SchemaVersions {
		if binding.Check() != nil || binding.Family < family || binding.Family > family+1 || index == 0 && binding.Family != 1 {
			return fmt.Errorf("object schema history is inconsistent")
		}
		family = binding.Family
	}
	body, err := ObjectSchemaBody(object)
	if err != nil {
		return err
	}
	version, err := platform.SchemaContentVersion(body)
	if err != nil || version != object.SchemaVersions[len(object.SchemaVersions)-1].Version {
		return fmt.Errorf("object schema version differs from its published definition")
	}
	return nil
}

func CompatibleObjectSchema(previous, next Object) bool {
	normalize := func(object Object) Object {
		object.Record = platform.Record{}
		object.Title, object.Plural, object.Description = "", "", ""
		object.Icon, object.Color, object.Display = "", "", ""
		object.ObjectView = nil
		object.State, object.Installed, object.Published = "", "", ""
		object.ExtensionSource, object.SchemaVersions = nil, nil
		object.ActionTypes, object.Actions = nil, nil
		object.Fields = nil
		object.States = slices.Clone(object.States)
		for index := range object.States {
			object.States[index].Title, object.States[index].Description, object.States[index].Tone = "", "", ""
		}
		return object
	}
	if !reflect.DeepEqual(normalize(previous), normalize(next)) {
		return false
	}
	fieldContract := func(field Field) Field {
		field.Title, field.SourceType = "", ""
		field.AIInput = nil
		field.Search = false
		field.Format = nil
		return field
	}
	for _, old := range previous.Fields {
		index := slices.IndexFunc(next.Fields, func(field Field) bool { return field.Name == old.Name })
		if index < 0 || !reflect.DeepEqual(fieldContract(old), fieldContract(next.Fields[index])) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.ValueType != nil || field.Formula != "" || field.FormulaExpr != nil || field.ReadOnly || field.AliasOf != "" || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}

func CompatibleObjectExpansion(previous, next Object) bool {
	if previous.Name != next.Name || len(next.Fields) <= len(previous.Fields) || len(previous.States) != len(next.States) || len(previous.Access) != len(next.Access) {
		return false
	}
	if !reflect.DeepEqual(previous.Constraints, next.Constraints) {
		return false
	}
	for i, state := range previous.States {
		if !reflect.DeepEqual(state, next.States[i]) {
			return false
		}
	}
	for i, access := range previous.Access {
		if !reflect.DeepEqual(access, next.Access[i]) {
			return false
		}
	}
	equal := func(a, b any) bool {
		x, xe := json.Marshal(a)
		y, ye := json.Marshal(b)
		return xe == nil && ye == nil && string(x) == string(y)
	}
	for _, field := range previous.Fields {
		index := slices.IndexFunc(next.Fields, func(other Field) bool { return other.Name == field.Name })
		if index < 0 || !equal(field, next.Fields[index]) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.ValueType != nil || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}

func MapObjectIntegrations(object *Object, resolve func(platform.AssetKind, string) (string, error)) error {
	var failure error
	mapTxn := func(txn *platform.BusinessTxn) {
		if txn == nil {
			return
		}
		platform.WalkBusinessTxn(txn, func(step platform.TxnStep) {
			if failure == nil && step.Effect != nil {
				step.Effect.Writeback, failure = resolve(platform.AssetWriteback, step.Effect.Writeback)
			}
		})
	}
	mapTxn(object.CreateTransaction)
	for i := range object.DispatchCommands {
		name, err := resolve(platform.AssetWriteback, object.DispatchCommands[i].Writeback)
		if err != nil {
			return err
		}
		object.DispatchCommands[i].Writeback = name
	}
	for _, action := range object.Actions {
		mapTxn(action.Transaction)
		if action.Transaction == nil || action.Transaction.Dispatch == nil {
			continue
		}
		dispatch := action.Transaction.Dispatch
		for _, item := range []struct {
			kind  platform.AssetKind
			value *string
		}{
			{platform.AssetConnection, &dispatch.Target},
			{platform.AssetSource, &dispatch.Readback.Source},
			{platform.AssetObservation, &dispatch.Readback.Observation},
		} {
			if *item.value == "" {
				continue
			}
			value, err := resolve(item.kind, *item.value)
			if err != nil {
				return err
			}
			*item.value = value
		}
	}
	return failure
}

func prepareObjectPublication(asset platform.ReleaseAsset, objects []Object, resolve func(platform.AssetKind, string) (string, error)) (Object, Object, error) {
	var frozen Object
	if err := json.Unmarshal(asset.Body, &frozen); err != nil || AppID+"."+frozen.Name != asset.Ref.Name {
		return frozen, Object{}, fmt.Errorf("invalid saved object %s", asset.Ref)
	}
	if err := MapObjectIntegrations(&frozen, resolve); err != nil {
		return frozen, Object{}, err
	}
	i := slices.IndexFunc(objects, func(object Object) bool { return !object.Archived && object.Name == frozen.Name })
	if i < 0 {
		return frozen, Object{}, fmt.Errorf("saved object %s has no matching draft; renamed or archived definitions need an upgrade plan", asset.Ref)
	}
	frozen.Record = objects[i].Record
	objects[i].State, objects[i].Installed = "published", AppID+"."+frozen.Name
	retained := frozen
	retained.Published = ""
	objects[i].Published = string(platform.Raw(retained))
	objects[i].ExtensionSource = frozen.ExtensionSource
	objects[i].SchemaVersions = frozen.SchemaVersions
	return frozen, objects[i], nil
}

func ReadObjectPublication(image []byte) (Object, error) {
	var record Object
	if err := json.Unmarshal(image, &record); err != nil {
		return Object{}, err
	}
	published, ok := publication.Read[Object](record.Published)
	if !ok || record.State != "published" || record.ID != published.ID || record.Name != published.Name || record.Installed != AppID+"."+published.Name {
		return Object{}, fmt.Errorf("accepted object has no matching published definition")
	}
	if published.ExtensionSource != nil {
		if err := published.ExtensionSource.Check(published.Extends); err != nil {
			return Object{}, err
		}
	}
	return published, nil
}

type ObjectDraftEnvironment struct {
	Host             platform.BuildHost
	Authority        platform.Authority
	BindActions      func(platform.Caller, *Object) error
	CaptureExtension func(*Object) error
	CaptureSchema    func(*Object) error
	Check            func(Object, string, platform.RuleEngine) error
	Validate         func(platform.Caller, Object) error
}

func ObjectDraftPublication(objects []Object, env ObjectDraftEnvironment) publication.Hooks {
	return publication.Hooks{Kind: platform.AssetObject, Closure: func(_ platform.Caller, id string, member platform.Member) (closure publication.DraftClosure, err error) {
		i := slices.IndexFunc(objects, func(object Object) bool { return object.ID == id && !object.Archived })
		if i < 0 {
			return closure, fmt.Errorf("draft object %q not found", id)
		}
		record := objects[i]
		if member.ID != "" {
			caller := env.Host.Caller(platform.Caller{}, member, AppID)
			submission := &pb.Submission{TenantId: caller.Tenant, PrincipalId: member.ID, Authority: AppID, Schema: &pb.SchemaRef{Name: SchemaPublish, Version: 1}, Target: &pb.EntityRef{Type: ObjectType, Id: record.ID}, Payload: platform.Raw(map[string]any{})}
			if decision := env.Authority.Catalog().DecideSubmission(caller, submission, time.Now()); !decision.Allow {
				return closure, fmt.Errorf("Publishing the target object: %s", decision.Reason)
			}
		}
		if was, ok := publication.Read[Object](record.Published); ok {
			closure.Prior = platform.AssetRef{App: AppID, Kind: platform.AssetObject, Name: AppID + "." + was.Name}
			closure.HasPrior = true
		}
		closure.Next = platform.AssetRef{App: AppID, Kind: platform.AssetObject, Name: AppID + "." + record.Name}
		if err = env.BindActions(env.Host.Automation(platform.Caller{}, AppID), &record); err != nil {
			return closure, err
		}
		if err = env.CaptureExtension(&record); err != nil {
			return closure, err
		}
		if err = env.CaptureSchema(&record); err != nil {
			return closure, err
		}
		if err = env.Check(record, record.ID, env.Host.Caller(platform.Caller{}, member, AppID).Rules()); err == nil {
			err = env.Validate(env.Host.Caller(platform.Caller{}, member, AppID), record)
		}
		if err == nil {
			record.Published = ""
			objects[i].Published = string(platform.Raw(record))
		}
		return closure, err
	}}
}

func AppendObjectAssets(objects []Object, sourceVersion string, add func(platform.ReleaseAsset, bool) error, withDependencies func(platform.ReleaseAsset) (platform.ReleaseAsset, error), actions func(Object) []platform.Action, page func(Object, string) (platform.ReleaseAsset, error)) error {
	for _, record := range objects {
		if record.Archived || record.Published == "" {
			continue
		}
		var saved Object
		if err := json.Unmarshal([]byte(record.Published), &saved); err != nil {
			return fmt.Errorf("invalid published object %s: %w", record.ID, err)
		}
		if saved.ID != record.ID {
			return fmt.Errorf("published object %s belongs to another record", record.ID)
		}
		ref := platform.AssetRef{App: AppID, Kind: platform.AssetObject, Name: AppID + "." + saved.Name}
		body, err := DefinitionBody(saved, ref.Name)
		if err != nil {
			return err
		}
		asset, err := withDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: sourceVersion, Body: body})
		if err != nil {
			return err
		}
		if err := add(asset, false); err != nil {
			return err
		}
		for _, action := range actions(saved) {
			actionBody, err := json.Marshal(struct {
				Schema string          `json:"schema"`
				Target string          `json:"target"`
				Roles  []string        `json:"roles"`
				Action platform.Action `json:"action"`
			}{Schema: action.Schema, Target: action.Target, Roles: action.Roles, Action: action})
			if err != nil {
				return fmt.Errorf("encode action %s: %w", action.Schema, err)
			}
			actionVersion := sourceVersion
			for _, binding := range saved.ActionTypes {
				if binding.Ref.Name == action.Schema {
					actionVersion += ".action-" + binding.SourceVersion
				}
			}
			asset, err := withDependencies(platform.ReleaseAsset{
				Ref:             platform.AssetRef{App: AppID, Kind: platform.AssetAction, Name: action.Schema},
				ContractVersion: 1, SourceVersion: actionVersion, Body: actionBody,
			})
			if err != nil {
				return err
			}
			if err := add(asset, false); err != nil {
				return err
			}
		}
		pageAsset, err := page(saved, sourceVersion)
		if err != nil {
			return err
		}
		if err := add(pageAsset, false); err != nil {
			return err
		}
	}
	return nil
}

type ObjectInstallationEnvironment struct {
	Host           platform.BuildHost
	Entity         func(Object) platform.Entity
	CheckProcesses func(platform.Caller, platform.Entity, platform.Entity, bool, bool) error
	Pages          func(Object) ([]platform.Page, string, error)
	Actions        func(Object, platform.Entity) []platform.Action
	JointMode      func() bool
}

func ValidateObjectInstallation(c platform.Caller, record Object, env ObjectInstallationEnvironment) error {
	entity := env.Entity(record)
	current, _ := platform.Get[Object](env.Host.Automation(platform.Caller{}, AppID), record.ID)
	previous, _ := publication.Read[Object](current.Published)
	changed, compatible := false, false
	if previous.Name != "" {
		oldBody, err := DefinitionBody(previous, AppID+"."+previous.Name)
		if err != nil {
			return err
		}
		newBody, err := DefinitionBody(record, AppID+"."+record.Name)
		if err != nil {
			return err
		}
		changed = string(oldBody) != string(newBody)
		compatible = CompatibleObjectExpansion(previous, record)
	}
	if err := env.CheckProcesses(c, entity, env.Entity(previous), changed, compatible); err != nil {
		return err
	}
	pages, generated, err := env.Pages(record)
	if err != nil {
		return err
	}
	if env.JointMode() {
		return nil
	}
	return env.Host.ValidateInstallDependents(c, entity, env.Actions(record, entity), generated, pages...)
}

func ObjectFreezePublication(objects []Object, retained func(platform.ReleaseAsset) (bool, error), resolve func(platform.AssetKind, string) (string, error)) publication.Hooks {
	return publication.Hooks{Kind: platform.AssetObject, FreezeOn: platform.AssetObject, Freeze: func(_ platform.Caller, _ string, asset platform.ReleaseAsset, _ []platform.ReleaseAsset) (publication.Frozen, error) {
		code, err := retained(asset)
		if err != nil {
			return publication.Frozen{}, err
		}
		if code {
			return publication.Frozen{Skip: true}, nil
		}
		frozen, record, err := prepareObjectPublication(asset, objects, resolve)
		if err != nil {
			return publication.Frozen{}, err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return publication.Frozen{}, fmt.Errorf("%s: %w", SchemaPublish, err)
		}
		asset.Body = platform.Raw(frozen)
		return publication.Frozen{Asset: asset, Publications: []platform.ReleasePublication{{Schema: SchemaPublish, Image: raw}}}, nil
	}}
}

type ObjectPublicationEnvironment struct {
	Host      func() platform.BuildHost
	Authority func() platform.Authority
	Entity    func(Object) platform.Entity
	Portable  func(Object) (Object, error)
	Actions   func(Object, platform.Entity) []platform.Action
	Page      func(Object) platform.Page
	Pages     func(Object) ([]platform.Page, string, error)
	Validate  func(platform.Caller, Object) error
	Installed func(platform.Entity)
}

func ObjectPublication(env ObjectPublicationEnvironment) publication.Hooks {
	image := func(raw json.RawMessage) (Object, platform.Entity, []platform.Action, []platform.Page, error) {
		published, err := ReadObjectPublication(raw)
		if err != nil {
			return Object{}, platform.Entity{}, nil, nil, err
		}
		entity := env.Entity(published)
		portable, err := env.Portable(published)
		if err != nil {
			return Object{}, platform.Entity{}, nil, nil, err
		}
		if err := CheckObjectSchemas(portable); err != nil {
			return Object{}, platform.Entity{}, nil, nil, err
		}
		actions := env.Actions(published, entity)
		return published, entity, actions, []platform.Page{env.Page(published)}, nil
	}
	return publication.Hooks{
		Kind:   platform.AssetObject,
		Schema: SchemaPublish,
		Validate: func(raw json.RawMessage) error {
			published, _, _, _, err := image(raw)
			if err != nil {
				return err
			}
			return env.Validate(platform.Caller{Replaying: true}, published)
		},
		Install: func(raw json.RawMessage) error {
			published, entity, actions, pages, err := image(raw)
			if err != nil {
				return err
			}
			pages, _, err = env.Pages(published)
			if err != nil {
				return err
			}
			if err := env.Authority().Extend([]string{entity.Type}, actions); err != nil {
				return fmt.Errorf("extend accepted object: %w", err)
			}
			env.Authority().Catalog().ReplaceTarget(entity.Type, actions)
			if err := env.Host().Install(platform.Caller{Replaying: true}, entity, actions, pages...); err != nil {
				return fmt.Errorf("install accepted object: %w", err)
			}
			env.Installed(entity)
			return nil
		},
		ReadFrozen: func(raw json.RawMessage) (json.RawMessage, error) {
			var record struct {
				Published string `json:"published"`
			}
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil, err
			}
			return json.RawMessage(record.Published), nil
		},
	}
}
