package records

import (
	"encoding/json"
	"platformserver/internal/rules"
	"platformserver/journal"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
)

type Plan struct {
	Match      func(reflect.Value) bool
	Pushdown   []journal.StateFilter
	Candidates func(IndexImage, ImageLookup) ([]string, bool)
}

func CompilePlan(info platform.EntityInfo, filter rules.Expr, search string) (Plan, error) {
	if filter.Op == "" && filter.Lit == nil && filter.Ref == "" {
		filter = boolLit(true)
	}
	row, err := CompileRowFilter(info, filter)
	if err != nil {
		return Plan{}, err
	}
	text := strings.ToLower(strings.TrimSpace(search))
	var searched []platform.FieldInfo
	for _, f := range info.Fields {
		if f.Search {
			searched = append(searched, f)
		}
	}
	match := row.Match
	if text != "" {
		match = func(v reflect.Value) bool {
			return row.Match(v) && (strings.Contains(strings.ToLower(Header(v).ID), text) || slices.ContainsFunc(searched, func(f platform.FieldInfo) bool {
				return strings.Contains(strings.ToLower(Searchable(v.FieldByIndex(f.Index).Interface())), text)
			}))
		}
	}
	return Plan{Match: match, Pushdown: row.Pushdown(), Candidates: func(image IndexImage, lookup ImageLookup) ([]string, bool) {
		return exprCandidates(image, lookup, filter)
	}}, nil
}

func (p Plan) Scoped(archived bool, effective func(reflect.Value) bool) Plan {
	if archived && effective == nil {
		return p
	}
	inner := p.Match
	p.Match = func(v reflect.Value) bool {
		if !archived && Header(v).Archived || effective != nil && !effective(v) {
			return false
		}
		return inner(v)
	}
	return p
}

func (p Plan) candidates(image IndexImage, lookup ImageLookup) ([]string, bool) {
	if p.Candidates == nil {
		return nil, false
	}
	return p.Candidates(image, lookup)
}

func Intersect(plans ...Plan) Plan {
	if len(plans) == 1 {
		return plans[0]
	}
	out := Plan{Match: func(v reflect.Value) bool {
		for _, p := range plans {
			if !p.Match(v) {
				return false
			}
		}
		return true
	}}
	for _, p := range plans {
		out.Pushdown = append(out.Pushdown, p.Pushdown...)
	}
	out.Candidates = func(image IndexImage, lookup ImageLookup) ([]string, bool) {
		var best []string
		found := false
		for _, p := range plans {
			if ids, ok := p.candidates(image, lookup); ok && (!found || len(ids) < len(best)) {
				best, found = ids, true
			}
		}
		return best, found
	}
	return out
}

func Union(plans ...Plan) Plan {
	if len(plans) == 1 {
		return plans[0]
	}
	return Plan{Match: func(v reflect.Value) bool {
		for _, p := range plans {
			if p.Match(v) {
				return true
			}
		}
		return false
	}, Candidates: func(image IndexImage, lookup ImageLookup) ([]string, bool) {
		var all []string
		for _, p := range plans {
			ids, ok := p.candidates(image, lookup)
			if !ok {
				return nil, false
			}
			all = append(all, ids...)
		}
		slices.Sort(all)
		return slices.Compact(all), true
	}}
}

func Subtract(base Plan, rest ...Plan) Plan {
	return Plan{Match: func(v reflect.Value) bool {
		if !base.Match(v) {
			return false
		}
		for _, p := range rest {
			if p.Match(v) {
				return false
			}
		}
		return true
	}, Pushdown: base.Pushdown, Candidates: base.Candidates}
}

type candidateSource struct {
	size int
	ids  []string
}

func exprCandidates(image IndexImage, lookup ImageLookup, expr rules.Expr) ([]string, bool) {
	var best *candidateSource
	var visit func(rules.Expr)
	visit = func(e rules.Expr) {
		if e.Op == "and" {
			for _, arg := range e.Args {
				visit(arg)
			}
			return
		}
		field, values, ok := membership(e)
		if !ok {
			return
		}
		if source, ok := indexedSource(image, lookup, field, values); ok && (best == nil || source.size < best.size) {
			best = &source
		}
	}
	visit(expr)
	if best == nil {
		return nil, false
	}
	return best.ids, true
}

func membership(e rules.Expr) (string, []string, bool) {
	switch e.Op {
	case "=", "in":
		if len(e.Args) != 2 || e.Args[0].Ref == "" || strings.Contains(e.Args[0].Ref, "#") || e.Args[1].Lit == nil {
			return "", nil, false
		}
		literal := e.Args[1]
		if e.Op == "=" {
			var value string
			if literal.Kind != "text" || json.Unmarshal(literal.Lit.Value, &value) != nil {
				return "", nil, false
			}
			return e.Args[0].Ref, []string{value}, true
		}
		var values []string
		if literal.Kind != "set" || literal.Lit.Element != "text" || json.Unmarshal(literal.Lit.Value, &values) != nil {
			return "", nil, false
		}
		return e.Args[0].Ref, values, true
	case "or":
		var field string
		var all []string
		for _, arg := range e.Args {
			name, values, ok := membership(arg)
			if !ok || arg.Op == "or" || field != "" && name != field {
				return "", nil, false
			}
			field = name
			all = append(all, values...)
		}
		return field, all, field != ""
	}
	return "", nil, false
}

func indexedSource(image IndexImage, lookup ImageLookup, field string, values []string) (candidateSource, bool) {
	if len(values) == 0 {
		return candidateSource{}, field == "id"
	}
	if field == "id" {
		ids := slices.Clone(values)
		slices.Sort(ids)
		ids = slices.Compact(ids)
		return candidateSource{size: len(ids), ids: ids}, true
	}
	if slices.Contains(values, "") {
		return candidateSource{}, false
	}
	info, ok := image.Info.Field(field)
	if !ok {
		return candidateSource{}, false
	}
	source, found := lookup(image.Info.Type)
	if !found {
		return candidateSource{}, false
	}
	var ids []string
	switch {
	case info.Type == "reference" && info.Ref != "" && info.Inverse != "":
		for _, target := range values {
			for id := range source.Indexes.ReferenceBucket(source, info, target).IDs.All() {
				ids = append(ids, id)
			}
		}
	case !source.Rows.Sourced() && slices.Contains([]string{"text", "choice", "reference", "date"}, info.Type):
		rule, ok := uniqueRule(image.Info, field)
		if !ok {
			return candidateSource{}, false
		}
		index := source.Indexes.CachedUnique(rule.Name)
		if index.Rule.Name == "" {
			return candidateSource{}, false
		}
		for _, value := range values {
			key, err := json.Marshal([]any{value})
			if err != nil {
				return candidateSource{}, false
			}
			for id := range index.Keys.Get(string(key)).All() {
				ids = append(ids, id)
			}
		}
	default:
		return candidateSource{}, false
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	return candidateSource{size: len(ids), ids: ids}, true
}

func uniqueRule(info platform.EntityInfo, field string) (platform.Constraint, bool) {
	for _, rule := range info.Constraints {
		if rule.Kind == "unique" && len(rule.Fields) == 1 && rule.Fields[0] == field && (rule.Scope == "" || rule.Scope == "tenant") {
			return rule, true
		}
	}
	return platform.Constraint{}, false
}
