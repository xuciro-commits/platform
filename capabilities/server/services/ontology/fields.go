package ontology

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

func CheckPropertyFields(fields []Field, app string, host platform.BuildHost) error {
	for _, field := range fields {
		if field.Property == nil {
			continue
		}
		if host == nil || field.Property.Ref.App != app {
			return fmt.Errorf("shared property needs its canonical tenant owner")
		}
		property, ok := host.ResolvePropertyType(*field.Property)
		if !ok {
			return fmt.Errorf("shared property version is unavailable")
		}
		if err := CheckPropertyField(field, property); err != nil {
			return err
		}
	}
	return nil
}

func CheckValueTypeFields(fields []Field, app string, host platform.BuildHost) error {
	for _, field := range fields {
		if field.ValueType == nil {
			continue
		}
		if host == nil || field.ValueType.Ref.App != app {
			return fmt.Errorf("value type needs its canonical tenant owner")
		}
		valueType, ok := host.ResolveValueType(*field.ValueType)
		if !ok {
			return fmt.Errorf("value type version is unavailable")
		}
		if err := CheckValueTypeField(field, valueType); err != nil {
			return err
		}
	}
	return nil
}

func CheckDataSources(o Object) error {
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

func CheckPropertyField(f Field, property platform.PropertyType) error {
	if f.Choices != "" || f.Ref != "" || f.Inverse != "" {
		return fmt.Errorf("shared scalar property cannot declare another field shape")
	}
	return property.CheckField(platform.FieldInfo{Name: f.Name, Type: f.Type, Title: f.Title, Required: f.Required})
}

func CheckValueTypeField(f Field, valueType platform.ValueType) error {
	if f.Choices != "" || f.Ref != "" || f.Inverse != "" || f.Property != nil || f.Formula != "" || f.FormulaExpr != nil {
		return fmt.Errorf("a field bound to a value type cannot declare another field shape")
	}
	return valueType.CheckField(platform.FieldInfo{Name: f.Name, Type: f.Type, Title: f.Title})
}

func CheckDisplay(o Object) error {
	if o.Display == "" {
		return nil
	}
	i := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == o.Display })
	if i < 0 || !slices.Contains([]string{"text", "longtext", "choice"}, o.Fields[i].Type) {
		return fmt.Errorf("the title field %q must be a text-like field of this object", o.Display)
	}
	return nil
}

func CompileAccess(o Object, builderRole, userRole string) (platform.Standard, platform.Scope, []string) {
	std := platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{builderRole, userRole}, Capability: o.Name}
	if len(o.Access) == 0 {
		return std, platform.Scope{}, []string{builderRole, userRole}
	}
	std.Roles = []string{builderRole}
	std.CreateRoles, std.EditRoles, std.ArchiveRoles = []string{builderRole}, []string{builderRole}, []string{builderRole}
	scope := platform.Scope{Owner: platform.OwnerCreated, Levels: map[string]string{}, Default: platform.ScopeNone}
	if o.Scope != nil {
		if o.Scope.Owner != "" {
			scope.Owner = o.Scope.Owner
		}
		scope.Unit, scope.Structure = o.Scope.Unit, o.Scope.Structure
	}
	scope.Levels[builderRole] = platform.ScopeTenant
	roles := []string{builderRole}
	for _, a := range o.Access {
		roles = append(roles, a.Role)
		scope.Levels[a.Role] = readLevels[a.Read]
		if a.Create {
			std.CreateRoles = append(std.CreateRoles, a.Role)
		}
		if a.Edit {
			std.EditRoles = append(std.EditRoles, a.Role)
		}
		if a.Archive {
			std.ArchiveRoles = append(std.ArchiveRoles, a.Role)
		}
	}
	return std, scope, roles
}
