package protocol

import (
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
)

func ConstraintSpec(info platform.RecordTypeInfo, constraint platform.Constraint) platform.RuleReadSpec {
	spec := platform.RuleReadSpec{Object: info.Type}
	if constraint.Reads != nil {
		spec.Collections = constraint.Reads.Collections
	}
	return spec
}

func ConstraintEntity(info platform.RecordTypeInfo) platform.RecordTypeInfo {
	info.Fields = slices.Clone(info.Fields)
	info.Fields = append(info.Fields, platform.FieldInfo{Name: "id", Type: "text"}, platform.FieldInfo{Name: "archived", Type: "boolean"})
	return info
}

func CheckConstraints(info platform.RecordTypeInfo, lookup func(string) (platform.RecordTypeInfo, bool), transactionBoundary bool, engines ...platform.RuleEngine) error {
	if len(info.Constraints) > 64 {
		return fmt.Errorf("object supports at most 64 constraints")
	}
	seen := map[string]bool{}
	ownerLookup := func(typ string) (platform.RecordTypeInfo, bool) {
		if typ == info.Type {
			return ConstraintEntity(info), true
		}
		other, ok := lookup(typ)
		return ConstraintEntity(other), ok && other.App == info.App
	}
	for _, c := range info.Constraints {
		if !platform.PageNodeID.MatchString(c.Name) || len(c.Name) > 64 || seen[c.Name] || strings.TrimSpace(c.Message) == "" || len(c.Message) > 512 || len(c.Fields) == 0 || len(c.Fields) > 16 {
			return fmt.Errorf("constraint needs a unique stable name, message and 1-16 fields")
		}
		seen[c.Name] = true
		if !slices.Contains([]string{"required", "unique", "range", "pattern", "expression", "cardinality", "immutable", "temporal"}, c.Kind) {
			return fmt.Errorf("constraint %s has unknown kind %s", c.Name, c.Kind)
		}
		if c.Level != "" && c.Level != "record" && c.Level != "txn" || c.Level == "txn" && !transactionBoundary {
			return fmt.Errorf("constraint %s needs a known accepted transaction boundary", c.Name)
		}
		for i, name := range c.Fields {
			if _, ok := ConstraintEntity(info).Field(name); !ok || slices.Contains(c.Fields[:i], name) {
				return fmt.Errorf("constraint %s names an unknown or repeated field %s", c.Name, name)
			}
		}
		for language, message := range c.Translations {
			if language != "zh-CN" || strings.TrimSpace(message) == "" || len(message) > 512 {
				return fmt.Errorf("constraint %s has an invalid message translation", c.Name)
			}
		}
		if c.Kind != "unique" && c.Scope != "" {
			return fmt.Errorf("only unique constraints have a scope")
		}
		if c.Kind == "unique" && c.Scope != "" && c.Scope != "tenant" {
			path, err := ConstraintScopePath(info, c.Scope)
			if err != nil {
				return err
			}
			if _, err := platform.RecordPathField(info.Type, path, ownerLookup); err != nil {
				return fmt.Errorf("constraint %s scope: %w", c.Name, err)
			}
		}
		if c.Kind == "immutable" && (c.Expr == nil && info.Lifecycle == nil || c.Level == "txn") {
			return fmt.Errorf("immutable constraint %s needs a lifecycle and record level", c.Name)
		}
		needsExpr := slices.Contains([]string{"range", "pattern", "expression", "cardinality", "temporal"}, c.Kind)
		if c.Kind != "immutable" && needsExpr != (c.Expr != nil) {
			return fmt.Errorf("constraint %s has an invalid expression shape", c.Name)
		}
		if c.Reads != nil {
			if c.Reads.Object != "" && c.Reads.Object != info.Type || len(c.Reads.Inputs) > 0 || c.Expr == nil {
				return fmt.Errorf("constraint reads cannot accept invocation inputs")
			}
			for _, set := range c.Reads.Collections {
				object, ok := ownerLookup(set.Object)
				if !ok || object.App != info.App || set.App != info.App || set.Policy != "complete" || set.Of != "" && set.Of != "$subject" {
					return fmt.Errorf("constraint collections must be complete and owned by the application")
				}
			}
		}
		if c.Expr != nil {
			if c.Expr.Kind != "boolean" {
				return fmt.Errorf("constraint %s must return boolean", c.Name)
			}
			if _, err := RuleSchema(*c.Expr, ConstraintSpec(info, c), ownerLookup, engines...); err != nil {
				return fmt.Errorf("constraint %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

func ConstraintScopePath(info platform.RecordTypeInfo, scope string) ([]string, error) {
	if scope == "parent" {
		var refs []string
		for _, field := range info.Fields {
			if field.Type == "reference" {
				refs = append(refs, field.Name)
			}
		}
		if len(refs) != 1 {
			return nil, fmt.Errorf("parent scope needs one reference; choose an explicit path")
		}
		return refs, nil
	}
	path := strings.Split(scope, ".")
	if len(path) > 16 {
		return nil, fmt.Errorf("constraint scope exceeds 16 segments")
	}
	return path, nil
}
