package pages

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

func CheckPageNavigation(source platform.Page, target platform.Page, nav platform.PageNavigation) error {
	sourceSchema, targetSchema := source.InputSchema, target.InputSchema
	version := 0
	inputs, outputs := map[string]platform.PagePort{}, map[string]platform.PagePort{}
	if targetSchema.Interface != nil {
		version = targetSchema.Interface.Version
		inputs = targetSchema.Interface.Inputs
		outputs = targetSchema.Interface.Outputs
	}
	if nav.InterfaceVersion != version {
		return fmt.Errorf("page navigation to %s interface version differs", nav.Page)
	}
	for id, port := range inputs {
		if _, ok := nav.Inputs[id]; port.Required && !ok {
			return fmt.Errorf("page navigation to %s misses input %s", nav.Page, id)
		}
	}
	for id, value := range nav.Inputs {
		port, ok := inputs[id]
		typ := platform.PageLiteralType(value.Literal)
		if value.Variable != "" {
			typ = sourceSchema.Variables[value.Variable].Type
		}
		if !ok || typ != port.Type || port.Type == "object-set" && (port.Object == nil || source.CollectionInputObject(value.Variable) != *port.Object || !platform.PageUIProfileSupports(sourceSchema.ProfileVersion(), "platform.page.v2.85")) || port.Type == "record" && (port.Object == nil || source.RecordVariableObject(value.Variable) != port.Object.Name) {
			return fmt.Errorf("page navigation input %s type or object differs", id)
		}
	}
	for id, variable := range nav.Results {
		port, ok := outputs[id]
		v := sourceSchema.Variables[variable]
		if !ok || !v.IsWritable() || v.Type != port.Type || v.Type == "record" {
			return fmt.Errorf("page navigation output %s type differs", id)
		}
	}
	return nil
}

func actionParameterCompatible(f platform.FieldInfo, p platform.Field) bool {
	if p.From != "" {
		return false
	}
	switch p.Type {
	case "string":
		if p.Ref != "" {
			return f.Type == "reference" && f.Ref == p.Ref
		}
		return slices.Contains([]string{"text", "longtext", "choice"}, f.Type)
	case "number":
		return f.Type == "decimal" || f.Type == "integer"
	case "integer":
		return f.Type == "integer"
	case "boolean", "date", "datetime":
		return f.Type == p.Type
	}
	return false
}
