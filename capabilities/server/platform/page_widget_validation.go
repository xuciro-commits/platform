package platform

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Section keeps its installed wire representation. Empty collections and
// zero value structs are absent configuration; non-nil pointers are explicit,
// including pointers to false, an empty string or an empty configuration.
func (s Section) configured(path string) bool {
	v := sectionJSONValue(reflect.ValueOf(s), strings.Split(path, "."))
	if !v.IsValid() {
		return false
	}
	if v.Kind() == reflect.Slice || v.Kind() == reflect.Map {
		return v.Len() != 0
	}
	return !v.IsZero()
}

// Configuration ownership is independent of the profile and presentation.
// Old documentless pages retain their original reader.
func (s Section) checkWidgetConfiguration() error {
	// Model validation materializes legacy ports temporarily. Ownership is
	// defined by the saved model binding, not those derived port values.
	s = CanonicalModelSection(s)
	w := pageWidget(s.Widget)
	if w == nil {
		return fmt.Errorf("widget %q is unavailable", s.Widget)
	}
	allowed := w.authoringFields()
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			if field.Anonymous {
				if err := walk(v.Field(i)); err != nil {
					return err
				}
				continue
			}
			key := strings.Split(field.Tag.Get("json"), ",")[0]
			if key != "" && key != "-" && s.configured(key) && !slices.Contains(allowed, key) {
				return fmt.Errorf("widget %s does not declare configuration %s", s.Widget, key)
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(s))
}

func sectionJSONValue(v reflect.Value, path []string) reflect.Value {
	if len(path) == 0 {
		return v
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		if field.Anonymous {
			if found := sectionJSONValue(v.Field(i), path); found.IsValid() {
				return found
			}
		}
		if strings.Split(field.Tag.Get("json"), ",")[0] == path[0] {
			return sectionJSONValue(v.Field(i), path[1:])
		}
	}
	return reflect.Value{}
}

func (d *PageDocument) checkDeclaredWidgetPorts(s Section, w *pageWidgetContract) error {
	for _, group := range w.PortGroups {
		count, seen := 0, map[string]bool{}
		for _, field := range group.Fields {
			id := sectionJSONText(reflect.ValueOf(s), strings.Split(field, "."))
			if id == "" {
				continue
			}
			if group.Distinct && seen[id] {
				return fmt.Errorf("widget %s needs distinct %s bindings", s.Widget, strings.Join(group.Fields, ", "))
			}
			seen[id], count = true, count+1
		}
		if count < group.Min || count > group.Max {
			return fmt.Errorf("widget %s needs %d to %d of %s", s.Widget, group.Min, group.Max, strings.Join(group.Fields, ", "))
		}
	}
	for _, ports := range [][]pageWidgetPort{w.InputPorts, w.OutputPorts} {
		for _, port := range ports {
			if port.BindingField == "enabledWhen" {
				continue
			}
			id := sectionJSONText(reflect.ValueOf(s), strings.Split(port.BindingField, "."))
			if id == "" {
				if port.Required {
					return fmt.Errorf("widget %s needs its %s port", s.Widget, port.BindingField)
				}
				continue
			}
			v, ok := d.Variables[id]
			if !ok || v.Type != port.Type {
				return fmt.Errorf("widget %s needs its original %s type", s.Widget, port.BindingField)
			}
			if len(port.Modes) > 0 && !slices.Contains(port.Modes, v.Mode) || len(port.Scopes) > 0 && !slices.Contains(port.Scopes, v.Scope) || port.Source != "" && (v.Source == nil || v.Source.Kind != port.Source) {
				return fmt.Errorf("widget %s has an invalid %s source or mode", s.Widget, port.BindingField)
			}
			if port.SameOwner != "" {
				other := sectionJSONText(reflect.ValueOf(s), strings.Split(port.SameOwner, "."))
				owner := d.Variables[other]
				if other == "" || v.Scope != owner.Scope || v.Owner != owner.Owner {
					return fmt.Errorf("widget %s %s must share its %s owner", s.Widget, port.BindingField, port.SameOwner)
				}
			}
			if plan := port.Plan; plan != nil {
				if v.Source == nil || v.Source.Kind != "plan" {
					return fmt.Errorf("widget %s needs an original plan", s.Widget)
				}
				q := d.Queries[v.Source.Query]
				if plan.Ordered && len(q.Sort) == 0 || plan.MaxLimit > 0 && (q.Limit < 1 || q.Limit > plan.MaxLimit) {
					return fmt.Errorf("widget %s needs its bounded ordered plan window", s.Widget)
				}
			}
		}
	}
	return nil
}

func (s Section) checkWidgetFieldRoles(info EntityInfo) error {
	w := pageWidget(s.Widget)
	if w == nil {
		return nil
	}
	for _, role := range w.FieldRoles {
		if role.When != "" && !s.configured(role.When) {
			continue
		}
		value := sectionJSONValue(reflect.ValueOf(s), strings.Split(role.Path, "."))
		var names []string
		if value.IsValid() {
			if value.Kind() == reflect.String {
				names = []string{value.String()}
			}
			if value.Kind() == reflect.Slice {
				names = value.Interface().([]string)
			}
		}
		if len(names) == 0 && !role.Optional {
			return fmt.Errorf("widget %s needs its original %s fields", s.Widget, role.Path)
		}
		seen := map[string]bool{}
		for _, name := range names {
			if name == "" && role.Optional && value.Kind() == reflect.String {
				continue
			}
			if name == "id" && role.AllowID {
				continue
			}
			field, ok := info.Field(name)
			if !ok || role.Distinct && seen[name] || !slices.Contains(role.Kinds, field.Type) {
				return fmt.Errorf("widget %s %s needs a visible original %s field", s.Widget, role.Path, strings.Join(role.Kinds, "/"))
			}
			seen[name] = true
		}
	}
	return nil
}

func (d *PageDocument) checkWidgetPortScopes(s Section, owner, overlay string) error {
	w := pageWidget(s.Widget)
	if w == nil {
		return nil
	}
	for _, ports := range [][]pageWidgetPort{w.InputPorts, w.OutputPorts} {
		for _, port := range ports {
			if port.ScopeRule == "" || port.BindingField == "enabledWhen" {
				continue
			}
			id := sectionJSONText(reflect.ValueOf(s), strings.Split(port.BindingField, "."))
			if id == "" {
				continue
			}
			v, ok := d.Variables[id]
			accessible := ok && (v.Scope == "page" || v.Scope == "application" || v.Scope == "loop-item" && v.Owner == owner && owner != "" || v.Scope == "overlay" && v.Owner == overlay && overlay != "")
			if !accessible || port.ScopeRule == "local" && owner != "" || port.ScopeRule == "exact" && (owner != "" || v.Scope == "page" && overlay != "" || v.Scope == "overlay" && v.Owner != overlay) {
				return fmt.Errorf("widget %s %s escapes its presentation owner", s.Widget, port.BindingField)
			}
		}
	}
	return nil
}
