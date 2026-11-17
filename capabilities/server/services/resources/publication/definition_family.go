package publication

import (
	"reflect"

	"platformserver/platform"
)

type DefinitionFamily struct {
	platform.Record
	Name, State string
	Version     int
	Published   string
	Versions    []string
}

func DefinitionFamilyOf(record any) DefinitionFamily {
	value := reflect.ValueOf(record)
	version, ok := definitionVersionField(value)
	if !ok || value.Kind() != reflect.Struct || !value.FieldByName("Name").IsValid() {
		panic("unsupported retained definition")
	}
	return DefinitionFamily{
		Record:    value.FieldByName("Record").Interface().(platform.Record),
		Name:      value.FieldByName("Name").String(),
		State:     value.FieldByName("State").String(),
		Version:   int(version.Int()),
		Published: value.FieldByName("Published").String(),
		Versions:  value.FieldByName("Versions").Interface().([]string),
	}
}

func SetDefinitionFamily(record any, family DefinitionFamily) {
	value := reflect.ValueOf(record)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		panic("unsupported retained definition")
	}
	value = value.Elem()
	version, ok := definitionVersionField(value)
	if !ok {
		panic("unsupported retained definition")
	}
	value.FieldByName("Record").Set(reflect.ValueOf(family.Record))
	value.FieldByName("State").SetString(family.State)
	version.SetInt(int64(family.Version))
	value.FieldByName("Published").SetString(family.Published)
	value.FieldByName("Versions").Set(reflect.ValueOf(family.Versions))
}

func definitionVersionField(value reflect.Value) (reflect.Value, bool) {
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	if version := value.FieldByName("Version"); version.IsValid() {
		return version, true
	}
	if version := value.FieldByName("DefinitionVersion"); version.IsValid() {
		return version, true
	}
	return reflect.Value{}, false
}
