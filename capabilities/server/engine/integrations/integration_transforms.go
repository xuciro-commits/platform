package integrations

import (
	"fmt"
	"platformserver/platform"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

func Transform(engine platform.RuleEngine, steps []platform.TransformStep, expectations []platform.TransformExpectation, input []map[string]any, read func(dataset string) ([]map[string]any, error), rowLimit ...int) ([]map[string]any, []platform.QuarantinedRow, error) {
	if len(rowLimit) > 0 && rowLimit[0] > 0 && len(input) > rowLimit[0] {
		return nil, nil, fmt.Errorf("pipeline input exceeds the %d row limit", rowLimit[0])
	}
	rows := make([]map[string]any, len(input))
	for i, r := range input {
		rows[i] = make(map[string]any, len(r))
		for k, v := range r {
			rows[i][k] = v
		}
	}
	var err error
	for i, s := range steps {
		rows, err = ApplyStep(engine, s, rows, read, rowLimit...)
		if err != nil {
			return nil, nil, fmt.Errorf("step %d (%s): %v", i+1, s.Kind, err)
		}
	}
	kept, quarantined := Expect(expectations, rows)
	return kept, quarantined, nil
}

func ApplyStep(engine platform.RuleEngine, s platform.TransformStep, rows []map[string]any, read func(string) ([]map[string]any, error), rowLimit ...int) ([]map[string]any, error) {
	switch s.Kind {
	case "select":
		out := make([]map[string]any, len(rows))
		for i, r := range rows {
			n := map[string]any{}
			for _, c := range s.Columns {
				if v, ok := r[c]; ok {
					n[c] = v
				}
			}
			out[i] = n
		}
		return out, nil
	case "rename":
		for _, r := range rows {
			if v, ok := r[s.From]; ok {
				r[s.To] = v
				delete(r, s.From)
			}
		}
		return rows, nil
	case "cast":
		for _, r := range rows {
			v, err := platform.IntegrationConvert(r[s.Column], s.Type)
			if err != nil {
				r[s.Column] = nil
			} else {
				r[s.Column] = v
			}
		}
		return rows, nil
	case "filter":
		out := rows[:0]
		for _, r := range rows {
			if platform.IntegrationMatches(r[s.Column], s.Op, s.Value) {
				out = append(out, r)
			}
		}
		return out, nil
	case "compute":
		expr := s.FormulaExpr
		if expr == nil {
			used, err := platform.FormulaFields(s.Formula)
			if err != nil {
				return nil, err
			}
			types := map[string]string{}
			for name := range used {
				types[name] = "decimal"
			}
			e, err := platform.LowerFormula(s.Formula, types)
			if err != nil {
				return nil, err
			}
			expr = &e
		}
		schema := platform.ExprSchema{Refs: map[string]platform.ExprType{}}
		var refs func(platform.Expr)
		refs = func(e platform.Expr) {
			if e.Ref != "" {
				schema.Refs[e.Ref] = platform.ExprType{Kind: e.Kind}
			}
			for _, arg := range e.Args {
				refs(arg)
			}
			if e.In != nil {
				refs(*e.In)
			}
		}
		refs(*expr)
		var program platform.ExprProgram
		var err error
		if s.FormulaExpr == nil {
			program, err = engine.CompileFrozenFormula(*expr, schema)
		} else {
			program, err = engine.CompileExpr(*expr, schema)
		}
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			ctx := platform.ExprContext{Refs: map[string]platform.ExprValue{}}
			for name, typ := range schema.Refs {
				if s.FormulaExpr == nil {
					ctx.Refs[name] = platform.ExprPresent(typ.Kind, platform.IntegrationNumeric(r[name]))
				} else if v, ok := r[name]; ok && v != nil {
					ctx.Refs[name] = platform.ExprPresent(typ.Kind, v)
				}
			}
			var result platform.ExprResult
			if s.FormulaExpr == nil {
				result, err = program.EvalFrozenFormula(ctx)
			} else {
				result, err = program.Eval(ctx)
			}
			if err != nil {
				return nil, err
			}
			v, err := platform.ExprGoValue(result.Value)
			if err != nil {
				return nil, err
			}
			r[s.To] = v
		}
		return rows, nil

	case "lookup", "join":
		other, err := read(s.Dataset)
		if err != nil {
			return nil, err
		}
		index := map[string][]map[string]any{}
		for _, o := range other {
			k := platform.IntegrationScalar(o[s.Match])
			index[k] = append(index[k], o)
		}
		prefix := s.As
		if prefix == "" && s.Kind == "join" {
			prefix = s.Dataset + "."
		}
		take := func(into map[string]any, from map[string]any) {
			cols := s.Columns
			if len(cols) == 0 {
				cols = make([]string, 0, len(from))
				for k := range from {
					cols = append(cols, k)
				}
			}
			for _, c := range cols {
				if v, ok := from[c]; ok {
					into[prefix+c] = v
				}
			}
		}
		if s.Kind == "lookup" {
			for _, r := range rows {
				if hits := index[platform.IntegrationScalar(r[s.Column])]; len(hits) > 0 {
					take(r, hits[0])
				}
			}
			return rows, nil
		}
		var out []map[string]any
		for _, r := range rows {
			hits := index[platform.IntegrationScalar(r[s.Column])]
			if len(hits) == 0 {
				if s.Outer {
					out = append(out, r)
				}
				continue
			}
			for _, h := range hits {
				if len(rowLimit) > 0 && rowLimit[0] > 0 && len(out) >= rowLimit[0] {
					return nil, fmt.Errorf("join exceeds the %d row limit", rowLimit[0])
				}
				n := make(map[string]any, len(r)+len(h))
				for k, v := range r {
					n[k] = v
				}
				take(n, h)
				out = append(out, n)
			}
		}
		return out, nil
	case "dedupe":
		seen := map[string]bool{}
		out := rows[:0]
		for _, r := range rows {
			k := platform.IntegrationKeyOf(r, s.Columns)
			if !seen[k] {
				seen[k] = true
				out = append(out, r)
			}
		}
		return out, nil
	case "aggregate":
		groups := map[string]map[string]any{}
		counts := map[string]map[string]float64{}
		var order []string
		for _, r := range rows {
			k := platform.IntegrationKeyOf(r, s.Columns)
			g, ok := groups[k]
			if !ok {
				g = map[string]any{}
				for _, c := range s.Columns {
					g[c] = r[c]
				}
				groups[k], counts[k] = g, map[string]float64{}
				order = append(order, k)
			}
			for _, m := range s.Measures {
				v := platform.IntegrationNumeric(r[m.Column])
				cur, has := g[m.To].(float64)
				switch m.Fn {
				case "count":
					g[m.To] = cur + 1
				case "sum", "avg":
					g[m.To] = cur + v
					counts[k][m.To]++
				case "min":
					if !has || v < cur {
						g[m.To] = v
					}
				case "max":
					if !has || v > cur {
						g[m.To] = v
					}
				}
			}
		}
		out := make([]map[string]any, 0, len(order))
		for _, k := range order {
			g := groups[k]
			for _, m := range s.Measures {
				if m.Fn == "avg" && counts[k][m.To] > 0 {
					g[m.To] = g[m.To].(float64) / counts[k][m.To]
				}
			}
			out = append(out, g)
		}
		return out, nil
	case "sort":
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i][s.Column], rows[j][s.Column]
			less := false
			if fa, fb := platform.IntegrationNumeric(a), platform.IntegrationNumeric(b); platform.IntegrationTypeOf(a) == "number" && platform.IntegrationTypeOf(b) == "number" {
				less = fa < fb
			} else {
				less = platform.IntegrationScalar(a) < platform.IntegrationScalar(b)
			}
			if s.Desc {
				return !less && platform.IntegrationScalar(a) != platform.IntegrationScalar(b)
			}
			return less
		})
		return rows, nil
	}
	return nil, fmt.Errorf("unknown step")
}

func Expect(expectations []platform.TransformExpectation, rows []map[string]any) ([]map[string]any, []platform.QuarantinedRow) {
	if len(expectations) == 0 {
		return rows, nil
	}
	seen := map[string]map[string]bool{}
	var kept []map[string]any
	var bad []platform.QuarantinedRow
	for _, r := range rows {
		reason := ""
		for _, e := range expectations {
			v := r[e.Column]
			text := platform.IntegrationScalar(v)
			switch e.Rule {
			case "notnull":
				if v == nil || text == "" {
					reason = e.Column + " is empty"
				}
			case "unique":
				if seen[e.Column] == nil {
					seen[e.Column] = map[string]bool{}
				}
				if seen[e.Column][text] {
					reason = e.Column + " repeats " + text
				}
				seen[e.Column][text] = true
			case "in":
				if !slices.Contains(strings.Split(e.Value, ","), text) {
					reason = e.Column + " is not one of " + e.Value
				}
			case "matches":
				if re, err := regexp.Compile(e.Value); err == nil && !re.MatchString(text) {
					reason = e.Column + " does not match " + e.Value
				}
			case "range":
				lo, hi, _ := strings.Cut(e.Value, "..")
				n := platform.IntegrationNumeric(v)
				if l, err := strconv.ParseFloat(lo, 64); lo != "" && (err != nil || n < l) {
					reason = e.Column + " is below " + lo
				} else if h, err := strconv.ParseFloat(hi, 64); hi != "" && (err != nil || n > h) {
					reason = e.Column + " is above " + hi
				}
			}
			if reason != "" {
				break
			}
		}
		if reason != "" {
			bad = append(bad, platform.QuarantinedRow{Reason: reason, Row: r})
		} else {
			kept = append(kept, r)
		}
	}
	return kept, bad
}
