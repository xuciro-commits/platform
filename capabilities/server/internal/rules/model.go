// Package rules is the one pure rule compiler/evaluator (ADR-0101). Host and
// browser use this same implementation; it has no tenant scripts or I/O.
package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"time"
)

const Version = "platform.expr.v1"
const MaxNodes = 512
const MaxDepth = 32
const MaxBytes = 64 << 10
const MaxSet = 10000
const MaxContextBytes = 8 << 20
const MaxText = 4096

type Expr struct {
	Op   string     `json:"op"`
	Kind string     `json:"kind"`
	Args []Expr     `json:"args,omitempty"`
	Lit  *Literal   `json:"lit,omitempty"`
	Ref  string     `json:"ref,omitempty"`
	Path []string   `json:"path,omitempty"`
	Agg  *Aggregate `json:"agg,omitempty"`
	In   *Expr      `json:"in,omitempty"`
}
type Literal struct {
	Value   json.RawMessage `json:"value"`
	Element string          `json:"element,omitempty"`
}
type Aggregate struct {
	Source string   `json:"source"`
	Field  []string `json:"field,omitempty"`
}
type Type struct {
	Object   string          `json:"object,omitempty"`
	Kind     string          `json:"kind"`
	Fields   map[string]Type `json:"fields,omitempty"`
	Element  *Type           `json:"element,omitempty"`
	Resource string          `json:"resource,omitempty"`
	Currency string          `json:"currency,omitempty"`
}
type SetType struct {
	Element Type   `json:"element"`
	Policy  string `json:"policy"`
}
type Schema struct {
	Refs map[string]Type    `json:"refs"`
	Sets map[string]SetType `json:"sets,omitempty"`
}

// Status distinguishes data that is absent, explicitly null, or withheld.
type Value struct {
	frozenNumber *float64
	Kind         string          `json:"kind"`
	Status       string          `json:"status"`
	Data         json.RawMessage `json:"value,omitempty"`
}
type Item struct {
	ID     string           `json:"id"`
	Fields map[string]Value `json:"fields"`
}
type Collection struct {
	Status   string `json:"status,omitempty"`
	Items    []Item `json:"items"`
	Complete bool   `json:"complete"`
	Withheld bool   `json:"withheld,omitempty"`
}
type Context struct {
	Sources []string              `json:"sources,omitempty"`
	Refs    map[string]Value      `json:"refs"`
	Sets    map[string]Collection `json:"sets,omitempty"`
	Me      string                `json:"me"`
	Now     time.Time             `json:"now"`
	Today   string                `json:"today"`
}
type Issue struct {
	Code    string `json:"code"`
	At      string `json:"at"`
	Message string `json:"message"`
}

func (i *Issue) Error() string { return fmt.Sprintf("%s: %s (%s)", i.At, i.Message, i.Code) }

type Result struct {
	Value   Value   `json:"value"`
	Reasons []Issue `json:"reasons,omitempty"`
}
type Program struct {
	patterns      map[string]*regexp.Regexp
	frozenFormula bool
	expr          Expr
	schema        Schema
}

func Raw(v any) json.RawMessage        { b, _ := json.Marshal(v); return b }
func Present(kind string, v any) Value { return Value{Kind: kind, Status: "value", Data: Raw(v)} }
func Missing(kind string) Value        { return Value{Kind: kind, Status: "missing"} }
func fail(code, at, msg string) *Issue { return &Issue{code, at, msg} }
func numeric(kind string) bool {
	return kind == "integer" || kind == "decimal" || kind == "number" || kind == "money"
}
func canonicalKind(k string) string {
	if k == "number" {
		return "decimal"
	}
	if k == "string" {
		return "text"
	}
	return k
}
func same(a, b string) bool { return canonicalKind(a) == canonicalKind(b) }
func decimalType(a, b string) string {
	if a == "money" || b == "money" {
		return "money"
	}
	if a == "decimal" || b == "decimal" || a == "number" || b == "number" {
		return "decimal"
	}
	return "integer"
}

func (e *Expr) UnmarshalJSON(raw []byte) error {
	if len(raw) > MaxBytes {
		return fail("budget", "$", "expression byte budget exceeded")
	}
	type plain Expr
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode((*plain)(e)); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fail("shape", "$", "one expression required")
	}
	return nil
}
