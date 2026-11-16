package pages

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

func CheckFilterSchema(source platform.PageResourceSource, object platform.EntityInfo) error {
	if source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App {
		return fmt.Errorf("filter object is unavailable")
	}
	for _, name := range source.Fields {
		field, ok := object.Field(name)
		if !ok || field.Type != "choice" && field.Type != "boolean" && field.Type != "reference" {
			return fmt.Errorf("filter field %s is unavailable or unsupported", name)
		}
	}
	return nil
}

func CheckComputeResource(p platform.Page, c platform.PageComputeResource, object platform.EntityInfo, op platform.Operation, valueType string, lookups ...func(string) (platform.EntityInfo, bool)) error {
	lookup := func(typ string) (platform.EntityInfo, bool) {
		return object, typ == object.Type
	}
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	ref := p.RecordResourceObject(c.RecordVariable)
	if ref.App != object.App || ref.Name != object.Type || ref.Kind != platform.AssetObject || op.Check() != nil || op.Name != c.Operation.Ref.Name || op.Input.Type != "object" || op.Input.Nullable {
		return fmt.Errorf("compute resource original object or operation differs")
	}
	for _, name := range op.Input.Required {
		if _, ok := c.Inputs[name]; !ok {
			return fmt.Errorf("compute input %s is missing", name)
		}
	}
	for name, b := range c.Inputs {
		schema, ok := op.Input.Properties[name]
		if !ok {
			return fmt.Errorf("compute input %s is unknown", name)
		}
		if b.Source == "literal" {
			if schema.Validate(b.Value, 64<<10) != nil {
				return fmt.Errorf("compute literal input differs")
			}
			continue
		}
		if b.Source != "subject" || len(b.Path) < 1 || len(b.Path) > 16 {
			return fmt.Errorf("compute source binding differs")
		}
		f, err := platform.RecordPathField(object.Type, b.Path, lookup)
		if err != nil {
			return fmt.Errorf("compute source field is unavailable")
		}
		typ := map[string]string{"text": "string", "longtext": "string", "choice": "string", "boolean": "boolean", "integer": "integer", "decimal": "number", "date": "string", "datetime": "string"}[f.Type]
		if typ == "" || schema.Type != typ && !(typ == "integer" && schema.Type == "number") {
			return fmt.Errorf("compute source field and input type differ")
		}
	}
	output := op.Output
	if c.OutputField != "" {
		if output.Type != "object" || output.Nullable || !slices.Contains(output.Required, c.OutputField) {
			return fmt.Errorf("compute scalar output must be required")
		}
		output = output.Properties[c.OutputField]
	}
	if !slices.Contains([]string{"number", "integer"}, output.Type) || output.Nullable || valueType == "decimal" && output.Type != "integer" {
		return fmt.Errorf("compute output needs a nonnullable numeric value")
	}
	return nil
}

func CheckBoundQuery(p platform.Page, id string, named *platform.Definition) error {
	if named == nil || named.Query == nil {
		return nil
	}
	q, exists := p.InputSchema.Queries[id]
	if !exists {
		return fmt.Errorf("page query %s is unavailable", id)
	}
	if len(named.Query.Sort) > 0 && len(q.Sort) > 0 && !slices.Equal(named.Query.Sort, q.Sort) || named.Query.Limit > 0 && named.Query.Limit < q.Limit {
		return fmt.Errorf("page query %s source ordering or capacity differs from its declaration", id)
	}
	return nil
}

func CheckQuerySetConditions(p platform.Page, named map[platform.AssetBinding]platform.NamedQuery) error {
	schema := p.InputSchema
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		q := schema.Queries[id]
		count := 0
		if q.Query != nil {
			decl := named[*q.Query]
			var terms []json.RawMessage
			if json.Unmarshal(decl.Domain, &terms) == nil {
				count += len(terms)
			}
			if decl.By != "" {
				count++
			}
		}
		if count > platform.QuerySetMaxConditions {
			return fmt.Errorf("query set source %s exceeds its condition budget", id)
		}
		if q.Set != nil {
			for _, input := range q.Set.Inputs {
				if err := visit(input); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for id, q := range schema.Queries {
		if q.Set != nil {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func CheckTableEdit(edit *platform.PageInlineEdit, info platform.EntityInfo, action platform.Action) error {
	if edit == nil {
		return nil
	}
	if edit.Action.App != info.App || edit.Action.Name != info.Type+".edit" || action.Schema != edit.Action.Name || action.Target != info.Type || action.New || action.NeedsApproval || action.Approval != nil || !slices.Contains(info.Standard, action.Schema) || slices.ContainsFunc(action.Payload, func(f platform.Field) bool {
		return f.Required
	}) {
		return fmt.Errorf("table editing requires its original standard field patch action without extra required inputs or approval")
	}
	for _, name := range edit.Fields {
		f, ok := info.Field(name)
		if !ok || f.ReadOnly || f.Aside || info.Lifecycle != nil && info.Lifecycle.Field == name || !slices.Contains(platform.PageWidgets.Runtime.TableEditing.FieldTypes, f.Type) || !slices.ContainsFunc(action.Payload, func(p platform.Field) bool {
			return p.Name == name
		}) {
			return fmt.Errorf("table edit field is unavailable or not accepted by its original action")
		}
	}
	return nil
}
