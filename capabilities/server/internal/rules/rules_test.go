package rules

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func op(name, kind string, args ...Expr) Expr { return Expr{Op: name, Kind: kind, Args: args} }
func run(t *testing.T, e Expr, s Schema, c Context) Result {
	t.Helper()
	p, err := Compile(e, s)
	if err != nil {
		t.Fatal(err)
	}
	r, err := p.Eval(c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func data(t *testing.T, r Result) any {
	t.Helper()
	v, err := decode(r.Value.Data)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestRuleOperatorFamilies(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 30, 0, 0, time.UTC)
	ctx := Context{Me: "member", Now: now, Today: "2026-10-12"}
	integer := func(n int) Expr { return LiteralOf("integer", n) }
	dec := func(s string) Expr { return LiteralOf("decimal", s) }
	text := func(s string) Expr { return LiteralOf("text", s) }
	date := func(s string) Expr { return LiteralOf("date", s) }
	cases := []struct {
		e    Expr
		want any
	}{
		{op("+", "decimal", dec("0.1"), dec("0.2")), "0.3"}, {op("-", "integer", integer(5), integer(2)), "3"},
		{op("*", "decimal", dec("1.25"), integer(4)), "5"}, {op("/", "decimal", integer(1), integer(4)), "0.25"},
		{op("round", "integer", dec("-1.5")), "-2"}, {op("abs", "integer", integer(-2)), "2"}, {op("negate", "decimal", dec("1.2")), "-1.2"},
		{op("min", "decimal", dec("2"), integer(1)), "1"}, {op("max", "integer", integer(1), integer(2)), "2"},
		{op("=", "boolean", integer(2), dec("2")), true}, {op("!=", "boolean", text("a"), text("b")), true},
		{op("<", "boolean", integer(2), integer(3)), true}, {op("<=", "boolean", integer(3), integer(3)), true},
		{op(">", "boolean", integer(4), integer(3)), true}, {op(">=", "boolean", integer(4), integer(4)), true},
		{op("and", "boolean", LiteralOf("boolean", true), LiteralOf("boolean", false)), false},
		{op("or", "boolean", LiteralOf("boolean", false), LiteralOf("boolean", true)), true}, {op("not", "boolean", LiteralOf("boolean", false)), true},
		{op("empty", "boolean", text("")), true}, {op("not-empty", "boolean", text("x")), true},
		{op("is-null", "boolean", LiteralOf("text", nil)), true}, {op("is-not-null", "boolean", text("")), true},
		{op("concat", "text", text("a"), text("b")), "ab"}, {op("lower", "text", text("ABC")), "abc"},
		{op("upper", "text", text("a")), "A"}, {op("trim", "text", text(" a ")), "a"},
		{op("contains", "boolean", text("abc"), text("b")), true}, {op("starts-with", "boolean", text("abc"), text("a")), true},
		{op("ends-with", "boolean", text("abc"), text("c")), true}, {op("length", "integer", text("字a")), json.Number("2")},
		{op("replace", "text", text("abc"), text("b"), text("d")), "adc"},
		{op("matches", "boolean", text("AB-12"), text("[A-Z]{2}-[0-9]{2}")), true},
		{op("matches", "boolean", text("xAB-12x"), text("[A-Z]{2}-[0-9]{2}")), false},
		{op("parse-decimal", "decimal", text(".10")), "0.1"}, {op("parse-number", "number", text("12.50")), "12.5"},
		{op("add-days", "date", date("2026-02-28"), integer(1)), "2026-03-01"},
		{op("add-months", "date", date("2026-01-31"), integer(1)), "2026-02-28"},
		{op("diff-days", "integer", date("2026-03-01"), date("2026-02-28")), json.Number("1")},
		{op("date-part", "integer", date("2026-02-28"), text("month")), json.Number("2")},
		{op("before", "boolean", date("2026-02-28"), date("2026-03-01")), true},
		{op("after", "boolean", date("2026-03-01"), date("2026-02-28")), true},
		{op("within-period", "boolean", date("2026-02-15"), date("2026-02-01"), date("2026-02-28")), true},
		{op("range", "boolean", integer(2), integer(1), integer(3)), true},
		{op("$me", "reference"), "member"}, {op("$today", "date"), "2026-10-12"}, {op("$now", "datetime"), "2026-10-12T12:30:00Z"},
	}
	for _, c := range cases {
		t.Run(c.e.Op, func(t *testing.T) {
			got := data(t, run(t, c.e, Schema{}, ctx))
			if got != c.want {
				t.Fatalf("want %#v, got %#v", c.want, got)
			}
		})
	}
	set := Expr{Kind: "set", Lit: &Literal{Value: Raw([]string{"a", "b"}), Element: "text"}}
	if data(t, run(t, op("in", "boolean", text("b"), set), Schema{}, ctx)) != true {
		t.Fatal("membership failed")
	}
}
func TestRuleMissingWithheldAndShortCircuit(t *testing.T) {
	s := Schema{Refs: map[string]Type{"amount": {Kind: "decimal"}, "private": {Kind: "decimal"}}}
	c := Context{Refs: map[string]Value{"private": {Kind: "decimal", Status: "withheld"}}}
	r := run(t, op("=", "boolean", RefOf("decimal", "amount"), LiteralOf("decimal", "0")), s, c)
	if data(t, r) != false || len(r.Reasons) != 1 || r.Reasons[0].Code != "missing" {
		t.Fatal("missing comparison became zero or lost reason")
	}
	for _, e := range []Expr{op("+", "decimal", RefOf("decimal", "amount"), LiteralOf("decimal", "1")), op("/", "decimal", LiteralOf("decimal", "1"), LiteralOf("decimal", "0")), op("=", "boolean", RefOf("decimal", "private"), LiteralOf("decimal", "1")), op("parse-decimal", "decimal", LiteralOf("text", "NaN"))} {
		p, err := Compile(e, s)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Eval(c); err == nil {
			t.Fatal("invalid operand silently produced a value")
		}
	}
	r = run(t, op("and", "boolean", LiteralOf("boolean", false), op("=", "boolean", RefOf("decimal", "private"), LiteralOf("decimal", "1"))), s, c)
	if data(t, r) != false {
		t.Fatal("and failed to short circuit")
	}
	// Compilation still visits a branch that would not execute.
	if _, err := Compile(op("or", "boolean", LiteralOf("boolean", true), op("unknown", "boolean")), s); err == nil {
		t.Fatal("unknown operator hidden in short circuit")
	}
}
func TestPartialCollectionCannotProveAllOrNone(t *testing.T) {
	schema := Schema{Sets: map[string]SetType{"items": {Element: Type{Kind: "reference", Fields: map[string]Type{"enabled": {Kind: "boolean"}}}, Policy: "visible"}}}
	context := Context{Sets: map[string]Collection{"items": {Complete: true, Withheld: true, Items: []Item{{ID: "visible", Fields: map[string]Value{"enabled": Present("boolean", true)}}}}}}
	for _, which := range []string{"all", "none"} {
		expr := Expr{Op: which, Kind: "boolean", Agg: &Aggregate{Source: "items"}, In: ptr(RefOf("boolean", "item.enabled"))}
		program, err := Compile(expr, schema)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := program.Eval(context); err == nil || !strings.Contains(err.Error(), "withheld") {
			t.Fatalf("%s proved a partial collection: %v", which, err)
		}
	}
	count := run(t, Expr{Op: "count", Kind: "integer", Agg: &Aggregate{Source: "items"}}, schema, context)
	if string(count.Value.Data) != `"1"` {
		t.Fatalf("count included hidden rows: %+v", count)
	}
}

func TestRuleDeclaredCollectionsAndPaths(t *testing.T) {
	s := Schema{Refs: map[string]Type{"parent": {Kind: "reference", Fields: map[string]Type{"enabled": {Kind: "boolean"}}}}, Sets: map[string]SetType{"items": {Element: Type{Kind: "reference", Fields: map[string]Type{"enabled": {Kind: "boolean"}, "amount": {Kind: "integer"}}}, Policy: "visible"}}}
	c := Context{
		Refs: map[string]Value{"parent.enabled": Present("boolean", true)},
		Sets: map[string]Collection{"items": {Complete: true, Items: []Item{
			{ID: "b", Fields: map[string]Value{"enabled": Present("boolean", true), "amount": Present("integer", 2)}},
			{ID: "a", Fields: map[string]Value{"enabled": Present("boolean", false), "amount": Present("integer", 1)}},
		}}},
	}

	for _, which := range []string{"all", "any", "none"} {
		e := Expr{Op: which, Kind: "boolean", Agg: &Aggregate{Source: "items"}, In: ptr(RefOf("boolean", "item.enabled"))}
		r := run(t, e, s, c)
		want := which == "any"
		if data(t, r) != want || len(r.Reasons) == 0 {
			t.Fatal("quantifier or visible-scope reason differs")
		}
	}
	for which, want := range map[string]string{"count": "2", "sum": "3", "min": "1", "max": "2", "avg": "1.5"} {
		kind := "integer"
		field := []string{"amount"}
		if which == "count" {
			field = nil
		}
		if which == "avg" {
			kind = "decimal"
		}
		r := run(t, Expr{Op: which, Kind: kind, Agg: &Aggregate{Source: "items", Field: field}}, s, c)
		if data(t, r) != want {
			t.Fatalf("%s: %v", which, data(t, r))
		}
	}
	path := Expr{Op: "path", Kind: "boolean", Args: []Expr{RefOf("reference", "parent")}, Path: []string{"enabled"}}
	if data(t, run(t, path, s, c)) != true {
		t.Fatal("declared path differs")
	}
	for _, e := range []Expr{{Op: "count", Kind: "integer", Agg: &Aggregate{Source: "absent"}}, {Op: "path", Kind: "boolean", Args: []Expr{RefOf("reference", "parent")}, Path: strings.Split(strings.Repeat("x.", 16)+"x", ".")}, op("=", "boolean", LiteralOf("text", "1"), LiteralOf("integer", 1))} {
		if _, err := Compile(e, s); err == nil {
			t.Fatal("publication accepted an invalid type/path/collection")
		}
	}
	c.Sets["items"] = Collection{Complete: false}
	p, err := Compile(Expr{Op: "count", Kind: "integer", Agg: &Aggregate{Source: "items"}}, s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Eval(c); err == nil {
		t.Fatal("incomplete set counted as complete")
	}
}
func ptr(e Expr) *Expr { return &e }

func TestRuleConformance(t *testing.T) {
	raw, err := os.ReadFile("conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name    string          `json:"name"`
		Request json.RawMessage `json:"request"`
		Value   json.RawMessage `json:"value"`
		Error   string          `json:"error"`
		Reason  string          `json:"reason"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			out, err := EvalJSON(v.Request)
			if v.Error != "" {
				issue, ok := err.(*Issue)
				if !ok || issue.Code != v.Error {
					t.Fatalf("want %s, got %v", v.Error, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var answer Result
			json.Unmarshal(out, &answer)
			var want, got any
			json.Unmarshal(v.Value, &want)
			json.Unmarshal(answer.Value.Data, &got)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("want %s, got %s", v.Value, out)
			}
			if v.Reason != "" && (len(answer.Reasons) == 0 || answer.Reasons[0].Code != v.Reason) {
				t.Fatalf("missing diagnostic: %s", out)
			}
		})
	}
}
