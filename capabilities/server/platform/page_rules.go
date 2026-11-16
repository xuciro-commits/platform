package platform

func PageRuleKind(kind string) string {
	switch kind {
	case "string":
		return "text"
	case "record", "filter", "object-set", "record-set":
		return "reference"
	case "string-set":
		return "set"
	default:
		return kind
	}
}

func (d PageInputSchema) PageRuleSchema() ExprSchema {
	schema := ExprSchema{Refs: map[string]ExprType{}, Sets: map[string]ExprSetType{}}
	for name, v := range d.Variables {
		t := ExprType{Kind: PageRuleKind(v.Type)}
		if t.Kind == "reference" {
			t.Resource = v.Type
		}
		if t.Kind == "set" {
			t.Element = &ExprType{Kind: "text"}
		}
		if v.RuleType != nil {
			if v.Mode == "property" {
				t.Kind = v.RuleType.Kind
			}
			t.Fields = v.RuleType.Fields
		}
		if v.Rule != nil {
			t.Kind = v.Rule.Kind
		}
		schema.Refs[name] = t
		if v.Type == "object-set" && v.RuleType != nil {
			schema.Sets[name] = ExprSetType{Element: *v.RuleType, Policy: "visible"}
		}
	}
	return schema
}

func PageRuleFits(carrier, kind string) bool {
	if PageRuleKind(carrier) == kind {
		return true
	}
	if carrier == "decimal" && kind == "integer" {
		return true
	}
	if carrier == "string" {
		return kind == "date" || kind == "datetime" || kind == "choice" || kind == "reference"
	}
	return false
}
