package records

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	ruleprotocol "platformserver/kernel/protocol"
	"reflect"
	"slices"

	"platformserver/platform"
)

func decodeRecordImage(raw json.RawMessage, model reflect.Type) (platform.Record, error) {
	value := reflect.New(model)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Interface()); err != nil {
		return platform.Record{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return platform.Record{}, fmt.Errorf("record image has trailing data")
	}
	return value.Elem().Field(0).Interface().(platform.Record), nil
}

func DecodeRecordImage(info platform.RecordTypeInfo, format string, raw json.RawMessage) (json.RawMessage, error) {
	if format == info.RecordFormat {
		_, err := decodeRecordImage(raw, info.Go)
		return raw, err
	}
	for _, image := range info.RecordImages {
		if image.Format != format {
			continue
		}
		before, err := decodeRecordImage(raw, reflect.TypeOf(image.Model))
		if err != nil {
			return nil, fmt.Errorf("record format %q: %w", format, err)
		}
		converted, err := image.Convert(slices.Clone(raw))
		if err != nil {
			return nil, err
		}
		after, err := decodeRecordImage(converted, info.Go)
		if err != nil {
			return nil, fmt.Errorf("record format %q: %w", info.RecordFormat, err)
		}
		if !reflect.DeepEqual(before, after) {
			return nil, fmt.Errorf("record format conversion changed record identity")
		}
		return converted, nil
	}
	return nil, fmt.Errorf("entity %s: unknown record image format %q", info.Type, format)
}

func LowerRecordSetPredicate(set platform.RecordSet, expr platform.Expr, lookup func(string) (platform.RecordTypeInfo, bool), engines ...platform.RuleEngine) (platform.Expr, error) {
	if len(set.Path) == 0 {
		return expr, nil
	}
	field, err := platform.RecordPathField(set.Object, set.Path, lookup)
	if err != nil || field.Type != "reference" || field.Ref == "" {
		return platform.Expr{}, fmt.Errorf("set %s filter path must end at a declared record reference", set.Name)
	}
	schemaLookup := func(typ string) (platform.RecordTypeInfo, bool) {
		info, ok := lookup(typ)
		return ruleprotocol.ConstraintEntity(info), ok
	}
	if _, err := ruleprotocol.RuleSchema(expr, platform.RuleReadSpec{Object: field.Ref}, schemaLookup, engines...); err != nil {
		return platform.Expr{}, err
	}
	var lower func(platform.Expr) platform.Expr
	lower = func(node platform.Expr) platform.Expr {
		ref, tail := node.Ref, []string{}
		if node.Op == "path" && len(node.Args) == 1 {
			ref, tail = node.Args[0].Ref, node.Path
		}
		if ref != "" && ref != "caller.id" {
			path := append(slices.Clone(set.Path[1:]), ref)
			path = append(path, tail...)
			return platform.Expr{Op: "path", Kind: node.Kind, Args: []platform.Expr{platform.ExprReference("reference", set.Path[0])}, Path: path}
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
	_, err = ruleprotocol.RuleSchema(lowered, platform.RuleReadSpec{Object: set.Object}, schemaLookup, engines...)
	return lowered, err
}

func CheckRecordSets(info platform.RecordTypeInfo, lookup func(string) (platform.RecordTypeInfo, bool), engines ...platform.RuleEngine) error {
	if len(info.Sets) > platform.RecordSetMaxDeclarations {
		return fmt.Errorf("object has %d sets; limit is %d", len(info.Sets), platform.RecordSetMaxDeclarations)
	}
	seen := map[string]bool{}
	for _, set := range info.Sets {
		if !platform.PageNodeID.MatchString(set.Name) || seen[set.Name] || set.From == "" || set.Limit < 1 || set.Limit > platform.RecordSetMaxRows || len(set.Path) > 16 || len(set.Where) > 32 || len(set.Order) == 0 || len(set.Order) > 16 {
			return fmt.Errorf("set %s needs a unique name, declared source, stable order and limit 1-%d", set.Name, platform.RecordSetMaxRows)
		}
		seen[set.Name] = true
		target, ok := lookup(set.Object)
		if !ok {
			return fmt.Errorf("set %s object is undeclared", set.Name)
		}
		if set.Binding != nil {
			if set.Binding.Ref.Check() != nil || set.Binding.SourceVersion == "" || !slices.Contains([]platform.AssetKind{platform.AssetQuery, platform.AssetLinkType}, set.Binding.Ref.Kind) || set.Binding.Ref.Name != set.From || set.Direction != "forward" && set.Direction != "reverse" && set.Binding.Ref.Kind == platform.AssetLinkType {
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
			} else if !slices.ContainsFunc(target.Fields, func(field platform.FieldInfo) bool { return field.Ref == info.Type && field.Inverse == set.From }) {
				return fmt.Errorf("set %s inverse is undeclared", set.Name)
			}
		}
		schemaLookup := func(typ string) (platform.RecordTypeInfo, bool) {
			other, ok := lookup(typ)
			return ruleprotocol.ConstraintEntity(other), ok
		}
		check := func(expr platform.Expr) error {
			var visit func(platform.Expr) error
			visit = func(expr platform.Expr) error {
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
			_, err := ruleprotocol.RuleSchema(expr, platform.RuleReadSpec{Object: target.Type}, schemaLookup, engines...)
			return err
		}
		for _, where := range set.Where {
			if where.Kind != "boolean" {
				return fmt.Errorf("set predicate must be boolean")
			}
			where, err := LowerRecordSetPredicate(set, where, lookup, engines...)
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
			field, err := platform.RecordPathField(set.Object, set.Path, lookup)
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
