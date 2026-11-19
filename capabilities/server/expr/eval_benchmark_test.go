package expr

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func BenchmarkExprEval(b *testing.B) {
	truth := Type{Kind: "boolean"}
	ref := Expr{Ref: "flag", Kind: "boolean"}
	e := Expr{Op: "and", Kind: "boolean", Args: []Expr{ref, ref}}
	schema := Schema{Refs: map[string]Type{"flag": truth}}
	c := Context{Refs: map[string]Value{"flag": Present("boolean", true)}}
	for _, name := range []string{"simple", "aggregate1000"} {
		if name == "aggregate1000" {
			expr := Expr{Ref: "item.flag", Kind: "boolean"}
			e = Expr{Op: "all", Kind: "boolean", Agg: &Aggregate{Source: "rows"}, In: &expr}
			schema.Sets = map[string]SetType{"rows": {Element: Type{Kind: "object", Fields: map[string]Type{"flag": truth}}, Policy: "complete"}}
			items := make([]Item, 1000)
			for i := range items {
				items[i] = Item{ID: strconv.Itoa(i), Fields: c.Refs}
			}
			c.Sets = map[string]Collection{"rows": {Items: items, Complete: true}}
		}
		p, err := Compile(e, schema)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r, err := p.Eval(c)
				if err != nil || string(r.Value.Data) != "true" {
					b.Fatal(r, err)
				}
			}
		})
	}
}

func TestContextBudgetMatchesEncodedInput(t *testing.T) {
	p, err := Compile(Expr{Kind: "boolean", Lit: &Literal{Value: Raw(true)}}, Schema{})
	if err != nil {
		t.Fatal(err)
	}
	empty, _ := json.Marshal(Context{})
	for _, n := range []int{-1, 0, 1} {
		c := Context{Me: strings.Repeat("a", MaxContextBytes-len(empty)+n)}
		_, err := p.Eval(c)
		if (err == nil) != (n <= 0) {
			t.Fatalf("limit %+d: %v", n, err)
		}
	}
	for _, c := range []Context{
		{Refs: map[string]Value{"unused": {Data: json.RawMessage("{")}}},
		{Me: strings.Repeat("<", MaxContextBytes/6+1)},
		{Now: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)},
	} {
		if _, err := p.Eval(c); err == nil {
			t.Fatal("accepted unencodable or oversized context")
		}
	}
}
