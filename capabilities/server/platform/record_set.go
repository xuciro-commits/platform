package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

const RecordSetMaxRows = 10000
const RecordSetMaxDeclarations = 64

// RecordSet is an object's bounded collection. From is a declared reference /
// inverse name, or Binding's retained query/link name. It is not a query string.
type RecordSet struct {
	Name      string          `json:"name"`
	From      string          `json:"from"`
	Binding   *AssetBinding   `json:"binding,omitempty"`
	Direction string          `json:"direction,omitempty"`
	Object    string          `json:"object"`
	Path      []string        `json:"path,omitempty"`
	Where     []Expr          `json:"where,omitempty"`
	Order     []RecordSetSort `json:"order"`
	Limit     int             `json:"limit"`
}
type RecordSetSort struct {
	Expr Expr `json:"expr"`
	Desc bool `json:"desc,omitempty"`
}

type RecordSetResult struct {
	Object  string            `json:"object"`
	Records []json.RawMessage `json:"records"`
	Partial bool              `json:"partial"`
	Sources []string          `json:"sources,omitempty"`
}

type RecordSetRuntime interface {
	ReadRecordSet(Caller, string, string, string, time.Time) (RecordSetResult, *kernel.Error)
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
			if _, err := RecordPathField(set.Object, set.Path, lookup); err != nil {
				return err
			}
			return fmt.Errorf("set %s filter paths must be expressed explicitly in Where expressions", set.Name)
		}
	}
	return nil
}

func RecordSetDependencies(info EntityInfo) []AssetRef {
	var refs []AssetRef
	for _, set := range info.Sets {
		if set.Binding != nil {
			refs = append(refs, set.Binding.Ref)
		}
		if app, _, ok := strings.Cut(set.Object, "."); ok && set.Object != info.Type {
			refs = append(refs, AssetRef{App: app, Kind: AssetObject, Name: set.Object})
		}
	}
	return refs
}
