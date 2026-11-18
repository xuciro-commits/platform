package protocol

import (
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"reflect"
	"strings"
)

func OwnedRelationType(app string) string { return app + ".__relation" }

func RelationEntity(app string) platform.Entity {
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[platform.Record](), Anonymous: true, Tag: reflect.StructTag(fmt.Sprintf(`entity:"%s"`, OwnedRelationType(app)))}}
	for _, field := range []string{"binding", "version", "parent", "child"} {
		fields = append(fields, reflect.StructField{Name: strings.ToUpper(field[:1]) + field[1:], Type: reflect.TypeFor[string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s" field:"readonly"`, field))})
	}
	fields = append(fields,
		reflect.StructField{Name: "Migration", Type: reflect.TypeFor[bool](), Tag: `json:"migration,omitempty" field:"readonly"`},
		reflect.StructField{Name: "Legacy", Type: reflect.TypeFor[[]platform.RelationLink](), Tag: `json:"legacy,omitempty" type:"json" field:"readonly"`},
	)
	model := reflect.New(reflect.StructOf(fields)).Elem().Interface()
	return platform.Entity{Type: OwnedRelationType(app), Title: "Owned relationship", Description: "An immutable typed relation instance of this application's authority", Model: model, Scope: platform.Scope{Default: platform.ScopeNone}}
}

func RelationValue(entity platform.Entity, id, binding, version, parent, child string, archived bool, legacy ...platform.RelationLink) any {
	value := reflect.New(reflect.TypeOf(entity.Model))
	_ = json.Unmarshal(platform.Raw(map[string]any{"id": id, "binding": binding, "version": version, "parent": parent, "child": child, "archived": archived, "legacy": legacy}), value.Interface())
	return value.Elem().Interface()
}

type RelationMigrationPlan struct {
	Binding  platform.AssetBinding   `json:"binding"`
	Links    []platform.RelationLink `json:"links"`
	Partial  bool                    `json:"partial"`
	Migrated bool                    `json:"migrated"`
}

type LegacyRelationSource interface {
	LegacyRelations() []platform.RelationLink
}
