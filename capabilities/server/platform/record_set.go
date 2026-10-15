package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

const RecordSetMaxRows = ExprMaxSet
const RecordSetMaxDeclarations = 64

// RecordSet is an object's bounded collection. From is a declared reference /
// inverse name, or Binding's retained query/link name. It is not a query string.
type RecordSet struct {
	BusinessTime *BusinessTime   `json:"businessTime,omitempty"`
	Name         string          `json:"name"`
	From         string          `json:"from"`
	Binding      *AssetBinding   `json:"binding,omitempty"`
	Direction    string          `json:"direction,omitempty"`
	Object       string          `json:"object"`
	Path         []string        `json:"path,omitempty"`
	Where        []Expr          `json:"where,omitempty"`
	Order        []RecordSetSort `json:"order"`
	Limit        int             `json:"limit"`
}
type RecordSetSort struct {
	Expr Expr `json:"expr"`
	Desc bool `json:"desc,omitempty"`
}

type RecordSetResult struct {
	Authorization *AuthzCollection  `json:"authorization,omitempty"`
	Object        string            `json:"object"`
	Records       []json.RawMessage `json:"records"`
	Partial       bool              `json:"partial"`
	Sources       []string          `json:"sources,omitempty"`
	Definition    string            `json:"definition"`
}

type RecordSetRef struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

type RecordSetRuntime interface {
	ReadRecordSet(Caller, string, string, string, time.Time) (RecordSetResult, *kernel.Error)
}

type RecordSetCandidateRuntime interface {
	ReadRecordSetCandidate(Caller, string, string, string, json.RawMessage, time.Time) (RecordSetResult, *kernel.Error)
}

func (c Caller) ruleRecordSet(object, id, name string, record any, at time.Time) (RecordSetResult, *kernel.Error) {
	if runtime, ok := c.rt.(RecordSetCandidateRuntime); ok && c.Staging() {
		return runtime.ReadRecordSetCandidate(c, object, id, name, Raw(record), at)
	}
	return c.ReadRecordSet(object, id, name, at)
}

// RecordSetPredicate lowers an optional reference prefix into the same Expr
// path operation used everywhere else. Order always addresses the set element.
func LowerRecordSetPredicate(set RecordSet, expr Expr, lookup func(string) (EntityInfo, bool)) (Expr, error) {
	if len(set.Path) == 0 {
		return expr, nil
	}
	field, err := RecordPathField(set.Object, set.Path, lookup)
	if err != nil || field.Type != "reference" || field.Ref == "" {
		return Expr{}, fmt.Errorf("set %s filter path must end at a declared record reference", set.Name)
	}
	schemaLookup := func(typ string) (EntityInfo, bool) { info, ok := lookup(typ); return ConstraintEntity(info), ok }
	if _, err := RuleSchema(expr, RuleReadSpec{Object: field.Ref}, schemaLookup); err != nil {
		return Expr{}, err
	}
	var lower func(Expr) Expr
	lower = func(node Expr) Expr {
		ref, tail := node.Ref, []string{}
		if node.Op == "path" && len(node.Args) == 1 {
			ref, tail = node.Args[0].Ref, node.Path
		}
		if ref != "" && ref != "caller.id" {
			path := append(slices.Clone(set.Path[1:]), ref)
			path = append(path, tail...)
			return Expr{Op: "path", Kind: node.Kind, Args: []Expr{ExprReference("reference", set.Path[0])}, Path: path}
		}
		node.Args = slices.Clone(node.Args)
		for i := range node.Args {
			node.Args[i] = lower(node.Args[i])
		}
		if node.In != nil {
			value := lower(*node.In)
			node.In = &value
		}
		return node
	}
	lowered := lower(expr)
	_, err = RuleSchema(lowered, RuleReadSpec{Object: set.Object}, schemaLookup)
	return lowered, err
}

func (c Caller) ReadRecordSet(object, id, name string, at time.Time) (RecordSetResult, *kernel.Error) {
	runtime, ok := c.rt.(RecordSetRuntime)
	if !ok {
		return RecordSetResult{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Declared record sets require the host")
	}
	return runtime.ReadRecordSet(c, object, id, name, at)
}

func (s *RecordSet) UnmarshalJSON(raw []byte) error {
	type plain RecordSet
	return strictTxn(raw, (*plain)(s))
}

func CheckRecordSets(info EntityInfo, lookup func(string) (EntityInfo, bool)) error {
	if len(info.Sets) > RecordSetMaxDeclarations {
		return fmt.Errorf("object has %d sets; limit is %d", len(info.Sets), RecordSetMaxDeclarations)
	}
	seen := map[string]bool{}
	for _, set := range info.Sets {
		if !pageNodeID.MatchString(set.Name) || seen[set.Name] || set.From == "" || set.Limit < 1 || set.Limit > RecordSetMaxRows || len(set.Path) > 16 || len(set.Where) > 32 || len(set.Order) == 0 || len(set.Order) > 16 {
			return fmt.Errorf("set %s needs a unique name, declared source, stable order and limit 1-%d", set.Name, RecordSetMaxRows)
		}
		seen[set.Name] = true
		target, ok := lookup(set.Object)
		if !ok {
			return fmt.Errorf("set %s object is undeclared", set.Name)
		}
		if set.Binding != nil {
			if set.Binding.Ref.Check() != nil || set.Binding.SourceVersion == "" || !slices.Contains([]AssetKind{AssetQuery, AssetLinkType}, set.Binding.Ref.Kind) || set.Binding.Ref.Name != set.From || set.Direction != "forward" && set.Direction != "reverse" && set.Binding.Ref.Kind == AssetLinkType {
				return fmt.Errorf("set %s needs a retained query or single-hop link binding", set.Name)
			}
		} else {
			if set.Direction != "reference" && set.Direction != "inverse" {
				return fmt.Errorf("set %s needs a reference direction", set.Name)
			}
			if set.Direction == "reference" {
				field, found := info.Field(set.From)
				if !found || field.Type != "reference" || field.Ref != set.Object {
					return fmt.Errorf("set %s source is not its typed reference", set.Name)
				}
			} else if !slices.ContainsFunc(target.Fields, func(field FieldInfo) bool { return field.Ref == info.Type && field.Inverse == set.From }) {
				return fmt.Errorf("set %s inverse is undeclared", set.Name)
			}
		}
		schemaLookup := func(typ string) (EntityInfo, bool) { other, ok := lookup(typ); return ConstraintEntity(other), ok }
		check := func(expr Expr) error {
			var visit func(Expr) error
			visit = func(expr Expr) error {
				if expr.Agg != nil {
					return fmt.Errorf("record sets cannot nest collections")
				}
				for _, child := range expr.Args {
					if err := visit(child); err != nil {
						return err
					}
				}
				if expr.In != nil {
					return visit(*expr.In)
				}
				return nil
			}
			if err := visit(expr); err != nil {
				return err
			}
			_, err := RuleSchema(expr, RuleReadSpec{Object: target.Type}, schemaLookup)
			return err
		}
		for _, where := range set.Where {
			if where.Kind != "boolean" {
				return fmt.Errorf("set predicate must be boolean")
			}
			where, err := LowerRecordSetPredicate(set, where, lookup)
			if err != nil {
				return err
			}
			if err := check(where); err != nil {
				return err
			}
		}
		for _, order := range set.Order {
			if !slices.Contains([]string{"text", "choice", "integer", "decimal", "number", "date", "datetime", "boolean", "reference"}, order.Expr.Kind) {
				return fmt.Errorf("set order needs a scalar expression")
			}
			if err := check(order.Expr); err != nil {
				return err
			}
		}
		if len(set.Path) > 0 {
			field, err := RecordPathField(set.Object, set.Path, lookup)
			if err != nil {
				return err
			}
			if field.Type != "reference" || field.Ref == "" {
				return fmt.Errorf("set filter path must resolve a record")
			}
		}
	}
	return nil
}
