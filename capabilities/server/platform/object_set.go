package platform

import (
	"encoding/json"
	"fmt"
)

type ObjectSet struct {
	Object    string          `json:"object,omitempty"`
	Interface string          `json:"interface,omitempty"`
	IDs       []string        `json:"ids,omitempty"`
	Filter    *Expr           `json:"filter,omitempty"`
	Domain    json.RawMessage `json:"domain,omitempty"`
	Search    string          `json:"search,omitempty"`
	Around    *SearchAround   `json:"around,omitempty"`
	Union     []ObjectSet     `json:"union,omitempty"`
	Intersect []ObjectSet     `json:"intersect,omitempty"`
	Subtract  []ObjectSet     `json:"subtract,omitempty"`
	Archived  bool            `json:"archived,omitempty"`
	AsOf      *BusinessTime   `json:"asOf,omitempty"`
}

type SearchAround struct {
	From      ObjectSet    `json:"from"`
	Link      AssetBinding `json:"link"`
	Direction string       `json:"direction"`
}

const (
	ObjectSetMaxNodes    = QuerySetMaxNodes
	ObjectSetMaxDepth    = QuerySetMaxDepth
	ObjectSetMaxOperands = 16
	ObjectSetMaxIDs      = 10000
	ObjectSetMaxIDBytes  = 256
)

func (s ObjectSet) Operation() (string, []ObjectSet) {
	switch {
	case len(s.Union) > 0:
		return "union", s.Union
	case len(s.Intersect) > 0:
		return "intersect", s.Intersect
	case len(s.Subtract) > 0:
		return "subtract", s.Subtract
	}
	return "", nil
}

func (s ObjectSet) Check() error {
	nodes := 0
	var check func(ObjectSet, int, bool) error
	check = func(set ObjectSet, depth int, start bool) error {
		nodes++
		if nodes > ObjectSetMaxNodes || depth > ObjectSetMaxDepth {
			return fmt.Errorf("object set budget exceeded")
		}
		bases := 0
		for _, present := range []bool{set.Object != "", set.Interface != "", set.Around != nil, len(set.Union) > 0, len(set.Intersect) > 0, len(set.Subtract) > 0} {
			if present {
				bases++
			}
		}
		if bases == 0 && start && len(set.IDs) > 0 {
			bases = 1
		}
		if bases != 1 {
			return fmt.Errorf("object set needs exactly one of object, interface, around or a set operation")
		}
		if len(set.IDs) > ObjectSetMaxIDs {
			return fmt.Errorf("object set names at most %d ids", ObjectSetMaxIDs)
		}
		for _, id := range set.IDs {
			if id == "" || len(id) > ObjectSetMaxIDBytes {
				return fmt.Errorf("object set ids are nonempty and bounded")
			}
		}
		if len(set.Search) > QuerySetMaxSearchBytes {
			return fmt.Errorf("object set search budget exceeded")
		}
		if len(set.Domain) > 0 {
			var tokens []json.RawMessage
			if json.Unmarshal(set.Domain, &tokens) != nil || len(tokens) > QuerySetMaxConditions {
				return fmt.Errorf("object set domain budget exceeded")
			}
		}
		if set.Filter != nil && set.Filter.Kind != "boolean" {
			return fmt.Errorf("object set filter must return boolean")
		}
		if set.AsOf != nil && set.AsOf.All && set.AsOf.At != "" {
			return fmt.Errorf("object set time selects either an instant or all versions")
		}
		if _, operands := set.Operation(); operands != nil {
			if len(operands) < 2 || len(operands) > ObjectSetMaxOperands {
				return fmt.Errorf("object set operation needs 2 to %d operands", ObjectSetMaxOperands)
			}
			for _, operand := range operands {
				if err := check(operand, depth+1, false); err != nil {
					return err
				}
			}
		}
		if set.Around != nil {
			if set.Around.Link.Ref.Kind != AssetLinkType || set.Around.Link.SourceVersion == "" || set.Around.Direction != "forward" && set.Around.Direction != "reverse" {
				return fmt.Errorf("search around needs a link type version and a direction")
			}
			if err := check(set.Around.From, depth+1, true); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(s, 0, false); err != nil {
		return err
	}
	encoded, err := json.Marshal(s)
	if err != nil || len(encoded) > QuerySetMaxBytes {
		return fmt.Errorf("object set encoding budget exceeded")
	}
	return nil
}

func (q NamedQuery) ObjectSet(of string) ObjectSet {
	var terms []any
	if len(q.Domain) > 0 {
		_ = json.Unmarshal(q.Domain, &terms)
	}
	if q.By != "" {
		terms = append(terms, []any{q.By, "=", of})
	}
	set := ObjectSet{Object: q.Object}
	if q.Interface != "" {
		set = ObjectSet{Interface: q.Interface}
	}
	if len(terms) > 0 {
		set.Domain, _ = json.Marshal(terms)
	}
	return set
}

func (q Query) ObjectSet(object string, around *SearchAround) ObjectSet {
	var predicate func(RecordSetPredicate) ObjectSet
	predicate = func(p RecordSetPredicate) ObjectSet {
		set := ObjectSet{Domain: p.Domain, Search: p.Search}
		if p.Set == nil {
			set.Object = object
			return set
		}
		operands := make([]ObjectSet, 0, len(p.Set.Inputs))
		for _, input := range p.Set.Inputs {
			if input == nil {
				input = &RecordSetPredicate{}
			}
			operands = append(operands, predicate(*input))
		}
		switch p.Set.Op {
		case "union":
			set.Union = operands
		case "intersect":
			set.Intersect = operands
		case "subtract":
			set.Subtract = operands
		}
		return set
	}
	set := predicate(RecordSetPredicate{Domain: q.Domain, Search: q.Search, Set: q.Set})
	if around != nil {
		if q.Set == nil {
			set.Object, set.Around = "", around
		} else {
			set = ObjectSet{Intersect: []ObjectSet{{Around: around}, set}}
		}
	}
	set.Archived, set.AsOf = q.Archived, q.BusinessTime
	return set
}
