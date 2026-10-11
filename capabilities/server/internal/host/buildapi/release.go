package buildapi

import (
	"encoding/json"

	"reflect"
	"slices"
)

// Adding an optional scalar cannot change the meaning of an existing process:
// every old field, state, access rule and action remains exactly as declared.
// New actions may consume the new field; host activation still needs a plan
// when the object already stores rows.
func CompatibleObjectExpansion(previous, next Object) bool {
	if previous.Name != next.Name || len(next.Fields) <= len(previous.Fields) || len(previous.States) != len(next.States) || len(previous.Access) != len(next.Access) {
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
	for _, action := range previous.Actions {
		index := slices.IndexFunc(next.Actions, func(other Action) bool { return other.Name == action.Name })
		if index < 0 || !equal(action, next.Actions[index]) {
			return false
		}
	}
	for _, field := range next.Fields {
		if slices.ContainsFunc(previous.Fields, func(old Field) bool { return old.Name == field.Name }) {
			continue
		}
		if field.Required || field.Ref != "" || field.Property != nil || field.Choices != "" || !slices.Contains([]string{"text", "longtext", "integer", "decimal", "boolean", "date", "datetime"}, field.Type) {
			return false
		}
	}
	return true
}
