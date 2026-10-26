package platform

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestActionInputSchemaPreservesOriginalInputs(t *testing.T) {
	min := 1.0
	info := EntityInfo{App: "orders", Type: "orders.order", Standard: []string{"orders.order.create", "orders.order.edit"}, Fields: []FieldInfo{
		{Name: "state", Type: "choice", Required: true, Choices: []string{"open", "done"}},
		{Name: "enabled", Type: "boolean"},
		{Name: "customer", Type: "reference", Ref: "orders.customer"},
		{Name: "total", Type: "money"},
		{Name: "lines", Type: "lines", Fields: []FieldInfo{
			{Name: "quantity", Type: "integer", Required: true, Constraints: &InputConstraints{Min: &min}},
			{Name: "selected", Type: "boolean"},
			{Name: "tags", Type: "tags", Choices: []string{"fragile", "heavy"}},
			{Name: "details", Type: "lines", Fields: []FieldInfo{{Name: "price", Type: "money"}}},
			{Name: "internal", Type: "text", Write: []string{"admin"}},
		}},
		{Name: "conditional", Type: "text", Required: true, When: &FieldCondition{Field: "enabled", In: []string{"true"}}},
		{Name: "metadata", Type: "json", Aside: true},
		{Name: "calculated", Type: "integer", ReadOnly: true},
		{Name: "secret", Type: "text", Read: []string{"admin"}},
	}}
	action := Action{Schema: "orders.order.create", Target: info.Type, Payload: []Field{
		{Name: "state", Type: "string", Required: true, Default: json.RawMessage(`"open"`)},
		{Name: "lines", Type: "string"},
	}}
	schema := ActionInputSchema(action, &info, []string{"clerk"})
	properties := schema["properties"].(map[string]any)
	field := func(name string) map[string]any { return properties[name].(map[string]any) }
	if field("enabled")["type"] != "boolean" || field("metadata")["type"] != nil || field("customer")["x-platform-reference"] != "orders.customer" ||
		!reflect.DeepEqual(field("state")["enum"], []string{"open", "done"}) || string(field("state")["default"].(json.RawMessage)) != `"open"` {
		t.Fatalf("original field shape lost: %+v", properties)
	}
	if properties["calculated"] != nil || properties["secret"] != nil {
		t.Fatalf("non-writable fields offered: %+v", properties)
	}
	money := field("total")["properties"].(map[string]any)
	if money["amount"].(map[string]any)["type"] != "integer" || money["currency"].(map[string]any)["type"] != "string" {
		t.Fatalf("money was flattened: %+v", field("total"))
	}
	row := field("lines")["items"].(map[string]any)
	columns := row["properties"].(map[string]any)
	if row["type"] != "object" || columns["quantity"].(map[string]any)["minimum"] != min || columns["internal"] != nil ||
		columns["details"].(map[string]any)["items"].(map[string]any)["type"] != "object" ||
		!reflect.DeepEqual(columns["tags"].(map[string]any)["items"].(map[string]any)["enum"], []string{"fragile", "heavy"}) {
		t.Fatalf("nested fields changed: %+v", row)
	}
	if !reflect.DeepEqual(row["required"], []string{"quantity"}) || slices.Contains(schema["required"].([]string), "conditional") || field("conditional")["x-platform-when"] == nil {
		t.Fatalf("conditional requiredness changed: %+v", schema)
	}
	action.Schema = "orders.order.edit"
	action.Payload[0].Required = false
	if required := ActionInputSchema(action, &info, []string{"clerk"})["required"].([]string); len(required) != 0 {
		t.Fatalf("partial edits now require unchanged fields: %v", required)
	}
	// A custom action keeps its own input list and requiredness. A declared
	// lines input can still use the original object's column descriptions.
	action.Schema = "orders.order.select"
	action.Payload = []Field{{Name: "lines", Type: "lines", Required: true}, {Name: "reason", Type: "string", Required: true}}
	custom := ActionInputSchema(action, &info, []string{"clerk"})
	customProperties := custom["properties"].(map[string]any)
	if len(customProperties) != 2 || customProperties["lines"].(map[string]any)["items"].(map[string]any)["type"] != "object" ||
		!reflect.DeepEqual(custom["required"], []string{"lines", "reason"}) {
		t.Fatalf("custom action was replaced by the record shape: %+v", custom)
	}
	if _, err := json.Marshal(custom); err != nil {
		t.Fatal(err)
	}
}
