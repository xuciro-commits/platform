package expr

import (
	"encoding/json"
)

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
