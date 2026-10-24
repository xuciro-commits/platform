package platform

import (
	"encoding/json"
	"fmt"
)

// RuleTablePlan is immutable operation data. The executor receives captured
// facts, never a Caller or a live record/query reader.
type RuleTablePlan struct {
	Reads   RuleReadSpec    `json:"reads"`
	Rows    []RuleTableRow  `json:"rows"`
	Default json.RawMessage `json:"default,omitempty"`
}
type RuleTableRow struct {
	Condition Expr            `json:"condition"`
	Result    json.RawMessage `json:"result"`
}

func (plan RuleTablePlan) Decide(facts ExprContext, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) (json.RawMessage, error) {
	schemas := []ExprSchema{}
	for _, row := range plan.Rows {
		schema, err := RuleSchema(row.Condition, plan.Reads, lookup, engines...)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return plan.DecideSchemas(facts, schemas, engines...)
}

func (plan RuleTablePlan) DecideSchemas(facts ExprContext, schemas []ExprSchema, engines ...RuleEngine) (json.RawMessage, error) {
	var engine RuleEngine = NativeRules{}
	if len(engines) > 0 {
		engine = engines[0]
	}
	if len(schemas) != len(plan.Rows) {
		return nil, fmt.Errorf("decision table schemas are not bound")
	}
	for i, row := range plan.Rows {
		p, err := engine.CompileExpr(row.Condition, schemas[i])
		if err != nil {
			return nil, err
		}
		r, err := p.Eval(facts)
		if err != nil {
			return nil, err
		}
		if string(r.Value.Data) == "true" {
			return row.Result, nil
		}
	}
	if len(plan.Default) > 0 {
		return plan.Default, nil
	}
	return nil, fmt.Errorf("no decision table row matches the captured input")
}
