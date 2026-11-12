package expr

import "encoding/json"

func EvalJSON(raw []byte) ([]byte, error) {
	if len(raw) > MaxContextBytes+2*MaxBytes {
		return nil, fail("budget", "$", "evaluation request budget exceeded")
	}
	var req struct {
		Expr    Expr    `json:"expr"`
		Schema  Schema  `json:"schema"`
		Context Context `json:"context"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	program, err := Compile(req.Expr, req.Schema)
	if err != nil {
		return nil, err
	}
	result, err := program.Eval(req.Context)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func CompileJSON(raw []byte) ([]byte, error) {
	if len(raw) > 4*MaxBytes {
		return nil, fail("budget", "$", "rule request exceeds its budget")
	}
	var req struct {
		Expr   Expr   `json:"expr"`
		Schema Schema `json:"schema"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if _, err := Compile(req.Expr, req.Schema); err != nil {
		return nil, err
	}
	return []byte(`{"ok":true}`), nil
}
