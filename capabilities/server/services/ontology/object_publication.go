package ontology

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

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

func PrepareObjectPublication(asset platform.ReleaseAsset, objects []Object, resolve func(platform.AssetKind, string) (string, error)) (Object, Object, error) {
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
