package ontology

import (
	"encoding/json"
	"fmt"
	"maps"
	ruleprotocol "platformserver/kernel/protocol"
	"slices"
	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

func literal(kind, value string) (any, error) {
	switch kind {
	case "integer":
		return strconv.Atoi(value)
	case "decimal":
		return strconv.ParseFloat(value, 64)
	case "boolean":
		return strconv.ParseBool(value)
	case "date":
		_, err := time.Parse(time.DateOnly, value)
		return value, err
	case "datetime":
		return time.Parse(time.RFC3339, value)
	case "money":
		return nil, fmt.Errorf("an amount is not set to a fixed value")
	}
	return value, nil
}

func AssignmentExpr(kind string, set platform.TxnSet, spec platform.RuleReadSpec) (platform.Expr, error) {
	kind = platform.RuleFieldKind(kind)
	targetKind := kind
	if set.Expr != nil {
		if set.From != "" {
			return platform.Expr{}, fmt.Errorf("use one assignment expression")
		}
		if !RuleAssignmentFits(kind, set.Expr.Kind) {
			return platform.Expr{}, fmt.Errorf("assignment result type differs from field")
		}
		return *set.Expr, nil
	}
	switch {
	case set.From == "$me":
		return platform.ExprReference("text", "caller.id"), nil
	case set.From == "$subject":
		return platform.ExprReference("reference", "input.$subject"), nil
	case set.From == "$now":
		op := "$now"
		if kind == "date" {
			op = "$today"
		}
		return platform.Expr{Op: op, Kind: kind}, nil
	case strings.HasPrefix(set.From, "="):
		value, err := literal(kind, strings.TrimPrefix(set.From, "="))
		return platform.ExprLiteralValue(kind, value), err
	default:
		name := set.From
		if !strings.HasPrefix(name, "step.") && !strings.HasPrefix(name, "row.") {
			name = "input." + name
		}
		input := strings.TrimPrefix(name, "input.")
		if typ, ok := spec.Inputs[input]; ok {
			kind = typ.Kind
		}
		if !RuleAssignmentFits(targetKind, kind) {
			return platform.Expr{}, fmt.Errorf("assignment source differs")
		}
		return platform.ExprReference(kind, name), nil
	}
}

func RuleAssignmentFits(dst, src string) bool {
	dst = platform.RuleFieldKind(dst)
	src = platform.RuleFieldKind(src)
	return dst == src || dst == "text" && src == "choice" || (dst == "decimal" || dst == "number") && (src == "integer" || src == "decimal" || src == "number")
}

func DispatchParameter(set platform.TxnSet, scope platform.RuleReadSpec) (platform.Expr, error) {
	kind := "text"
	if set.Expr != nil {
		kind = set.Expr.Kind
	} else if input, found := scope.Inputs[strings.TrimPrefix(set.From, "input.")]; found {
		kind = input.Kind
	}
	if set.Expr == nil && strings.HasPrefix(set.From, "input.") {
		set.From = strings.TrimPrefix(set.From, "input.")
	}
	if slices.Contains([]string{"set", "object", "json", ""}, kind) {
		return platform.Expr{}, fmt.Errorf("Dispatch parameters require scalar expressions")
	}
	return AssignmentExpr(kind, set, scope)
}

func WalkTxnSteps(steps []platform.TxnStep, visit func(platform.TxnStep)) {
	for _, step := range steps {
		visit(step)
		if step.ForEach != nil {
			WalkTxnSteps(step.ForEach.Steps, visit)
		}
		for _, refusal := range step.Refusals {
			if refusal.Record != nil {
				WalkTxnSteps([]platform.TxnStep{*refusal.Record}, visit)
			}
		}
	}
}

func WalkBusinessTxn(txn *platform.BusinessTxn, visit func(platform.TxnStep)) {
	WalkTxnSteps(txn.Steps, visit)
	for _, refusal := range txn.Refusals {
		if refusal.Record != nil {
			WalkTxnSteps([]platform.TxnStep{*refusal.Record}, visit)
		}
	}
}

func WithTxnRow(spec platform.RuleReadSpec, info platform.RecordTypeInfo) platform.RuleReadSpec {
	spec.Inputs = maps.Clone(spec.Inputs)
	for _, field := range ruleprotocol.ConstraintEntity(info).Fields {
		spec.Inputs["row."+field.Name] = platform.ExprType{Kind: platform.RuleFieldKind(field.Type), Object: field.Ref}
	}
	spec.Inputs["row.$subject"] = platform.ExprType{Kind: "reference", Object: info.Type}
	return spec
}

func TxnResult(spec *platform.RuleReadSpec, step platform.TxnStep) {
	if step.Call != nil {
		spec.Inputs["step."+step.Name] = step.Call.ResultType
	}
	if step.Kind == "sequence" {
		spec.Inputs["step."+step.Name] = platform.ExprType{Kind: "text"}
	}
	if step.Kind == "create" || step.Kind == "post" {
		spec.Inputs["step."+step.Name] = platform.ExprType{Kind: "reference", Object: step.Target.Type}
	}
}

func IntervalFields(fields platform.TxnIntervalFields) map[string]string {
	return map[string]string{fields.Subject: "reference", fields.Source: "text", fields.SourceVersion: "text", fields.SourceRevision: "text", fields.ProcessingVersion: "text", fields.Since: "datetime", fields.Until: "datetime", fields.OpenStart: "boolean", fields.OpenEnd: "boolean", fields.Retired: "boolean", fields.Review: "boolean"}
}

func WithTxnRange(scope platform.RuleReadSpec, kind string) platform.RuleReadSpec {
	scope.Inputs = maps.Clone(scope.Inputs)
	scope.Inputs["row.start"] = platform.ExprType{Kind: kind}
	scope.Inputs["row.end"] = platform.ExprType{Kind: kind}
	scope.Inputs["row.index"] = platform.ExprType{Kind: "integer"}
	return scope
}

func TxnIterationSource(owner, subject, of string, lookup func(string) (platform.RecordTypeInfo, bool)) (platform.RecordTypeInfo, error) {
	typ := subject
	if of != "" {
		field, err := platform.RecordPathField(subject, strings.Split(of, "."), lookup)
		if err != nil || field.Type != "reference" || field.Ref == "" {
			return platform.RecordTypeInfo{}, fmt.Errorf("Iteration source must be a declared record reference")
		}
		typ = field.Ref
	}
	info, ok := lookup(typ)
	if !ok || info.App != owner {
		return platform.RecordTypeInfo{}, fmt.Errorf("Iteration source must belong to its transaction owner")
	}
	return info, nil
}

func TransactionDestination(subject, field string, txn *platform.BusinessTxn, inputs []platform.Field) ([]string, string) {
	if txn == nil || txn.Subject != subject {
		return nil, ""
	}
	var assignment *platform.Expr
	writes, direct := 0, false
	var visit func([]platform.TxnStep, bool)
	visit = func(steps []platform.TxnStep, nested bool) {
		for _, step := range steps {
			if step.Kind == "update" && step.Target.Type == subject {
				for _, change := range step.Changes {
					if change.Field == field {
						writes++
						assignment = change.Expr
						direct = !nested && step.When == nil && step.Target.ID == "$subject" && step.Target.Via == ""
					}
				}
			}
			if step.ForEach != nil {
				visit(step.ForEach.Steps, true)
			}
		}
	}
	visit(txn.Steps, false)
	if writes != 1 || !direct || assignment == nil || assignment.Op != "" {
		return nil, ""
	}
	if assignment.Lit != nil {
		var state string
		if json.Unmarshal(assignment.Lit.Value, &state) == nil {
			return []string{state}, ""
		}
	}
	for _, input := range inputs {
		if assignment.Ref == "input."+input.Name && input.Type == "string" && input.Required && len(input.Choices) > 0 {
			return slices.Clone(input.Choices), input.Name
		}
	}
	return nil, ""
}
