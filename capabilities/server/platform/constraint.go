package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Constraint is a versioned, pure write invariant of an object.
// It is part of the object's publication, not a second executable asset.
type Constraint struct {
	Name         string            `json:"name"`
	Kind         string            `json:"kind" enum:"required,unique,range,pattern,expression,cardinality,immutable,temporal"`
	Fields       []string          `json:"fields"`
	Expr         *Expr             `json:"expr,omitempty"`
	Message      string            `json:"message"`
	Translations map[string]string `json:"translations,omitempty"`
	Scope        string            `json:"scope,omitempty"`
	Level        string            `json:"level,omitempty" enum:"record,txn"`
	Reads        *RuleReadSpec     `json:"reads,omitempty"`
}

func (c *Constraint) UnmarshalJSON(raw []byte) error {
	type plain Constraint
	return strictTxn(raw, (*plain)(c))
}

// ConstraintSpec uses only declared same-owner paths and complete collections.
// Partial/visible sets cannot prove a storage invariant.
func ConstraintSpec(info EntityInfo, constraint Constraint) RuleReadSpec {
	spec := RuleReadSpec{Object: info.Type}
	if constraint.Reads != nil {
		spec.Collections = constraint.Reads.Collections
	}
	return spec
}

// ConstraintEntity includes existing Record metadata in the rule schema.
func ConstraintEntity(info EntityInfo) EntityInfo {
	info.Fields = slices.Clone(info.Fields)
	info.Fields = append(info.Fields, FieldInfo{Name: "id", Type: "text"}, FieldInfo{Name: "archived", Type: "boolean"})
	return info
}

// CheckConstraints is shared by native declarations and builder publication.
// A host must supply an explicit accepted transaction boundary for txn rules.
func CheckConstraints(info EntityInfo, lookup func(string) (EntityInfo, bool), transactionBoundary bool) error {
	if len(info.Constraints) > 64 {
		return fmt.Errorf("object supports at most 64 constraints")
	}
	seen := map[string]bool{}
	ownerLookup := func(typ string) (EntityInfo, bool) {
		if typ == info.Type {
			return ConstraintEntity(info), true
		}
		other, ok := lookup(typ)
		return ConstraintEntity(other), ok && other.App == info.App
	}
	for _, c := range info.Constraints {
		if !pageNodeID.MatchString(c.Name) || len(c.Name) > 64 || seen[c.Name] || strings.TrimSpace(c.Message) == "" || len(c.Message) > 512 || len(c.Fields) == 0 || len(c.Fields) > 16 {
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
			if _, err := RecordPathField(info.Type, path, ownerLookup); err != nil {
				return fmt.Errorf("constraint %s scope: %w", c.Name, err)
			}
		}
		if c.Kind == "immutable" && (info.Lifecycle == nil || c.Level == "txn") {
			return fmt.Errorf("immutable constraint %s needs a lifecycle and record level", c.Name)
		}
		needsExpr := slices.Contains([]string{"range", "pattern", "expression", "cardinality", "temporal"}, c.Kind)
		if needsExpr != (c.Expr != nil) {
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
			if _, err := RuleSchema(*c.Expr, ConstraintSpec(info, c), ownerLookup); err != nil {
				return fmt.Errorf("constraint %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

// parent is unambiguous only with one declared scalar reference. Otherwise the
// builder must choose an explicit reference path (at most 16 segments).
func ConstraintScopePath(info EntityInfo, scope string) ([]string, error) {
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

// RefuseConstraint preserves the original kernel error vocabulary and supplies
// a distinct host diagnostic code. Replay stores these facts, never the rule.
func (c Caller) RefuseConstraint(constraint Constraint, record string, values map[string]json.RawMessage, reason string) *kernel.Error {
	issue := FieldIssue{Code: "constraint_violation", Constraint: constraint.Name, Record: record, Message: constraint.Message, Translations: constraint.Translations, Values: values, Reason: reason, Path: []string{constraint.Fields[0]}}
	for _, field := range constraint.Fields[1:] {
		issue.RelatedPaths = append(issue.RelatedPaths, []string{field})
	}
	err := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Constraint {name}: {message}", constraint.Name, constraint.Message)
	if c.inputRefused != nil {
		c.inputRefused(err, []FieldIssue{issue})
	}
	if c.rt != nil {
		c.rt.InputRefused(err, []FieldIssue{issue})
	}
	return err
}
