package rules

import (
	"encoding/json"
	"fmt"
)

// LowerPage compiles the old page operator graph. It never evaluates values.
// Resource presence uses the resource owner's status projection as a reference.
func LowerPage(raw json.RawMessage, kind string, schema Schema) (Expr, error) {
	var old struct {
		Op   string `json:"op"`
		Args []struct {
			Variable string          `json:"variable"`
			Literal  json.RawMessage `json:"literal"`
		} `json:"args"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return Expr{}, err
	}
	aliases := map[string]string{"equal": "=", "present": "not-empty", "decimal-add": "+", "decimal-subtract": "-", "decimal-less": "<", "parse-decimal": "parse-decimal", "parse-number": "parse-number", "not": "not", "and": "and", "or": "or", "concat": "concat"}
	op, ok := aliases[old.Op]
	if !ok {
		return Expr{}, fail("operator", "$.op", "unknown legacy page operator")
	}
	e := Expr{Op: op, Kind: kind}
	for i, arg := range old.Args {
		if (arg.Variable != "") == (len(arg.Literal) > 0) {
			return e, fail("shape", fmt.Sprintf("$.args[%d]", i), "exactly one variable or literal required")
		}
		if arg.Variable != "" {
			t, ok := schema.Refs[arg.Variable]
			if !ok {
				return e, fail("reference", "$", "page variable is undeclared")
			}
			if t.Resource != "" && (old.Op != "present" || t.Resource == "record-set") {
				return e, fail("type", "$", "legacy scalar operator cannot read a resource")
			}
			e.Args = append(e.Args, RefOf(t.Kind, arg.Variable))
			continue
		}
		v, err := decode(arg.Literal)
		if err != nil {
			return e, err
		}
		typ := ""
		switch x := v.(type) {
		case string:
			typ = "text"
		case bool:
			typ = "boolean"
		case map[string]any:
			tag, _ := x["kind"].(string)
			if tag == "decimal" || tag == "number" {
				typ = tag
				arg.Literal = Raw(x["value"])
			}
		}
		if typ == "" {
			return e, fail("type", "$", "legacy literal type is unsupported")
		}
		e.Args = append(e.Args, Expr{Kind: typ, Lit: &Literal{Value: arg.Literal}})
	}
	if _, err := Compile(e, schema); err != nil {
		return e, err
	}
	return e, nil
}
func CompileJSON(raw []byte) ([]byte, error) {
	if len(raw) > 4*MaxBytes {
		return nil, fail("budget", "$", "rule request exceeds its budget")
	}
	var r struct {
		Expr   Expr   `json:"expr"`
		Schema Schema `json:"schema"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	_, err := Compile(r.Expr, r.Schema)
	return []byte(`{"ok":true}`), err
}
func LowerPageJSON(raw []byte) ([]byte, error) {
	if len(raw) > 4*MaxBytes {
		return nil, fail("budget", "$", "page rule request exceeds its budget")
	}
	var r struct {
		Expression json.RawMessage `json:"expression"`
		Kind       string          `json:"kind"`
		Schema     Schema          `json:"schema"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	e, err := LowerPage(r.Expression, r.Kind, r.Schema)
	if err != nil {
		return nil, err
	}
	return json.Marshal(e)
}
