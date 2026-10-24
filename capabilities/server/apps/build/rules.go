package build

import (
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func ruleLookup(o construction.Object, lookup func(string) (platform.EntityInfo, bool)) func(string) (platform.EntityInfo, bool) {
	return func(typ string) (platform.EntityInfo, bool) {
		if typ != construction.TypeOf(o.Name) {
			if lookup == nil {
				return platform.EntityInfo{}, false
			}
			return lookup(typ)
		}
		info := platform.EntityInfo{Type: typ, App: construction.ID, Sets: o.Sets}
		for _, f := range o.Fields {
			read := f.Read
			if len(read) > 0 {
				read = append([]string{construction.Builder}, read...)
			}
			info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type, Ref: f.Ref, Read: read})
		}
		if len(o.States) > 0 {
			info.Fields = append(info.Fields, platform.FieldInfo{Name: "state", Type: "choice"})
		}
		return info, true
	}
}
func ruleSpec(o construction.Object, a construction.Action, read *platform.RuleReadSpec) platform.RuleReadSpec {
	s := platform.RuleReadSpec{Object: construction.TypeOf(o.Name), Inputs: map[string]platform.ExprType{}}
	if a.Transaction != nil {
		s.Inputs["$subject"] = platform.ExprType{Kind: "reference", Object: s.Object}
	}
	if read != nil {
		s.Collections = read.Collections
	}
	for _, in := range a.Inputs {
		s.Inputs[in.Name] = platform.ExprType{Kind: platform.RuleFieldKind(in.Type), Object: in.Ref}
	}
	return s
}
func legacyOperand(o construction.Object, a construction.Action, name string, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
	f, err := conditionField(o, a, name, lookup)
	if err != nil {
		return platform.Expr{}, err
	}
	kind := platform.RuleFieldKind(f.Type)
	if strings.HasPrefix(name, "input.") || !strings.Contains(name, ".") {
		return platform.ExprReference(kind, name), nil
	}
	parts := strings.Split(name, ".")
	return platform.Expr{Op: "path", Kind: kind, Args: []platform.Expr{platform.ExprReference("reference", parts[0])}, Path: parts[1:]}, nil
}
func conditionExpr(o construction.Object, a construction.Action, cond construction.Condition, lookup func(string) (platform.EntityInfo, bool)) (platform.Expr, error) {
	if cond.Expr != nil {
		if cond.Field != "" || cond.Operator != "" || cond.Value != "" || cond.ValueField != "" || cond.When != nil {
			return platform.Expr{}, fmt.Errorf("use one expression condition")
		}
		return *cond.Expr, nil
	}
	left, err := legacyOperand(o, a, cond.Field, lookup)
	if err != nil {
		return left, err
	}
	op := cond.Operator
	if op == "not empty" {
		op = "not-empty"
	}
	e := platform.Expr{Op: op, Kind: "boolean", Args: []platform.Expr{left}}
	if op != "empty" && op != "not-empty" {
		var right platform.Expr
		if cond.ValueField != "" {
			right, err = legacyOperand(o, a, cond.ValueField, lookup)
		} else if cond.Value == "$me" {
			right = platform.ExprReference("text", "caller.id")
		} else {
			var x any
			x, err = literal(left.Kind, cond.Value)
			right = platform.ExprLiteralValue(left.Kind, x)
		}
		if err != nil {
			return e, err
		}
		e.Args = append(e.Args, right)
	}
	if cond.When != nil {
		when, err := conditionExpr(o, a, *cond.When, lookup)
		if err != nil {
			return e, err
		}
		e = platform.Expr{Op: "or", Kind: "boolean", Args: []platform.Expr{{Op: "not", Kind: "boolean", Args: []platform.Expr{when}}, e}}
	}
	return e, nil
}
func assignmentExpr(kind string, set construction.Set, spec platform.RuleReadSpec) (platform.Expr, error) {
	kind = platform.RuleFieldKind(kind)
	targetKind := kind
	if set.Expr != nil {
		if set.From != "" {
			return platform.Expr{}, fmt.Errorf("use one assignment expression")
		}
		if !ruleAssignmentFits(kind, set.Expr.Kind) {
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
		v, err := literal(kind, strings.TrimPrefix(set.From, "="))
		return platform.ExprLiteralValue(kind, v), err
	default:
		name := set.From
		if !strings.HasPrefix(name, "step.") && !strings.HasPrefix(name, "row.") {
			name = "input." + name
		}
		input := strings.TrimPrefix(name, "input.")
		if typ, ok := spec.Inputs[input]; ok {
			kind = typ.Kind
		}
		if !ruleAssignmentFits(targetKind, kind) {
			return platform.Expr{}, fmt.Errorf("assignment source differs")
		}
		return platform.ExprReference(kind, name), nil
	}
}
func evalRule(e platform.Expr, spec platform.RuleReadSpec, c platform.Caller, record any, inputs map[string]any, now time.Time, lookup func(string) (platform.EntityInfo, bool)) (platform.ExprResult, *kernel.Error) {
	return platform.EvaluateRule(e, spec, c, record, inputs, now, lookup)
}

func actionCriteria(o construction.Object, a construction.Action, lookup func(string) (platform.EntityInfo, bool)) (*platform.RuleCriteria, error) {
	reads, conditions := a.Reads, a.Conditions
	if txn := a.Transaction; txn != nil {
		reads, conditions = txn.Reads, nil
		if txn.Guard != nil {
			conditions = []construction.Condition{{Expr: txn.Guard, Message: txn.Message}}
		}
	}
	plan := &platform.RuleCriteria{Reads: ruleSpec(o, a, reads)}
	used := map[string]bool{}
	var visit func(platform.Expr)
	visit = func(expr platform.Expr) {
		if strings.HasPrefix(expr.Ref, "input.") {
			name, _, _ := strings.Cut(strings.TrimPrefix(expr.Ref, "input."), ".")
			used[name] = true
		}
		for _, arg := range expr.Args {
			visit(arg)
		}
		if expr.In != nil {
			visit(*expr.In)
		}
	}
	for _, condition := range conditions {
		expr, err := conditionExpr(o, a, condition, ruleLookup(o, lookup))
		if err != nil {
			return nil, err
		}
		plan.Conditions = append(plan.Conditions, platform.RuleCriterion{Expr: expr, Message: condition.Message})
		visit(expr)
	}
	for _, set := range plan.Reads.Collections {
		if strings.HasPrefix(set.Of, "input.") {
			name, _, _ := strings.Cut(strings.TrimPrefix(set.Of, "input."), ".")
			used[name] = true
		}
	}
	for name := range plan.Reads.Inputs {
		if !used[name] {
			delete(plan.Reads.Inputs, name)
		}
	}
	return plan, nil
}

func ruleAssignment(e platform.Expr, s platform.RuleReadSpec, c platform.Caller, record any, inputs map[string]any, now time.Time, lookup func(string) (platform.EntityInfo, bool), provenance ...*[]string) (any, *kernel.Error) {
	r, err := evalRule(e, s, c, record, inputs, now, lookup)
	for _, refs := range provenance {
		*refs = append(*refs, r.Sources...)
	}
	if err != nil {
		return nil, err
	}
	v, failure := platform.ExprGoValue(r.Value)
	if failure != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, failure.Error())
	}
	return v, nil
}

func (b *Build) normalizeObjectRules(o *construction.Object) error {
	lookup := ruleLookup(*o, b.conditionLookup)
	for i := range o.Fields {
		f := &o.Fields[i]
		if f.Formula != "" {
			node, parseErr := construction.ValueParseFormula(f.Formula)
			if parseErr != nil {
				return parseErr
			}
			used := map[string]bool{}
			node.Fields(used)
			for _, operand := range o.Fields {
				if operand.Type == "money" && used[operand.Name] {
					return fmt.Errorf("field %s legacy money formula needs an explicit currency-aware Expr", f.Name)
				}
			}
			e, err := formulaExpr(o.Fields, *f)
			if err != nil {
				return err
			}
			f.FormulaExpr = &e
			f.Formula = ""
		}
	}
	for i := range o.Actions {
		a := &o.Actions[i]
		s := ruleSpec(*o, *a, a.Reads)
		for j := range a.Conditions {
			cond := &a.Conditions[j]
			if cond.Expr == nil {
				if cond.When != nil {
					if cond.When.When != nil {
						return fmt.Errorf("nested legacy guards require Expr")
					}
					if err := checkActionCondition(*o, *a, *cond.When, lookup, false); err != nil {
						return err
					}
				}
				if err := checkActionCondition(*o, *a, *cond, lookup, true); err != nil {
					return err
				}
			}
			e, err := conditionExpr(*o, *a, *cond, lookup)
			if err != nil {
				return err
			}
			*cond = construction.Condition{Expr: &e, Message: cond.Message}
		}
		for j := range a.Sets {
			set := &a.Sets[j]
			f, err := conditionField(*o, *a, set.Field, lookup)
			if err != nil {
				return err
			}
			if set.Expr == nil {
				if !strings.HasPrefix(set.From, "=") && set.From != "$me" && set.From != "$now" {
					ix := -1
					for k, in := range a.Inputs {
						if in.Name == set.From {
							ix = k
							break
						}
					}
					if ix < 0 {
						return fmt.Errorf("action %s sets %s from %q, which is undeclared", a.Name, set.Field, set.From)
					}
					if !inputFits(a.Inputs[ix].Type, f.Type) {
						return fmt.Errorf("action %s assignment input %s does not fit %s", a.Name, set.From, f.Type)
					}
					if f.Type == "choice" {
						for _, option := range construction.ValueChoices(a.Inputs[ix].Choices) {
							found := false
							for _, allowed := range f.Choices {
								if option == allowed {
									found = true
								}
							}
							if !found {
								return fmt.Errorf("input choice %s is not one of the field's choices", option)
							}
						}
					}
				}
			}
			e, err := assignmentExpr(f.Type, *set, s)
			if err != nil {
				return err
			}
			*set = construction.Set{Field: set.Field, Expr: &e}
		}
	}
	if err := b.migrateObjectTransactions(o); err != nil {
		return err
	}
	for i := range o.Actions {
		if err := b.normalizeTransaction(*o, &o.Actions[i], lookup); err != nil {
			return err
		}
	}
	if o.CreateTransaction != nil {
		action := construction.Action{Name: "creation", Transaction: o.CreateTransaction}
		for _, field := range o.Fields {
			action.Inputs = append(action.Inputs, construction.Input{Name: field.Name, Title: field.Title, Type: field.Type, Ref: field.Ref, Choices: field.Choices})
		}
		if err := b.normalizeTransaction(*o, &action, lookup); err != nil {
			return err
		}
	}
	// Draft lowering changes representation only. Cross-object rule checking
	// belongs to Check, with the complete publication candidate available.
	return nil
}
func (b *Build) checkObjectRules(o construction.Object, engines ...platform.RuleEngine) error {
	if err := b.checkObjectProtocols(o); err != nil {
		return err
	}
	lookup := ruleLookup(o, b.conditionLookup)
	if _, err := formulaOrder(o.Fields); err != nil {
		return err
	}
	for _, f := range o.Fields {
		if strings.HasPrefix(f.Name, "rulesources") || strings.HasPrefix(f.Name, "aisources") || f.Name == "rulewithheld" {
			return fmt.Errorf("field %s is reserved for rule provenance", f.Name)
		}
	}
	check := func(e platform.Expr, s platform.RuleReadSpec, where string) error {
		if err := b.checkRuleCollections(s, where, lookup); err != nil {
			return err
		}
		_, err := platform.RuleSchema(e, s, lookup, engines...)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		return nil
	}
	for _, f := range o.Fields {
		if f.FormulaExpr != nil {
			if platform.RuleFieldKind(f.Type) != platform.RuleFieldKind(f.FormulaExpr.Kind) {
				return fmt.Errorf("field %s formula result type differs", f.Name)
			}
			if err := check(*f.FormulaExpr, ruleSpec(o, construction.Action{}, o.Reads), "field "+f.Name); err != nil {
				return err
			}
		}
	}
	for _, a := range o.Actions {
		s := ruleSpec(o, a, a.Reads)
		for _, cond := range a.Conditions {
			if cond.Expr != nil {
				if cond.Expr.Kind != "boolean" || cond.Message == "" {
					return fmt.Errorf("action %s guard needs boolean and a refusal message", a.Name)
				}
				if err := check(*cond.Expr, s, "action "+a.Name); err != nil {
					return err
				}
			}
		}
		for _, set := range a.Sets {
			if set.Expr != nil {
				f, err := conditionField(o, a, set.Field, lookup)
				if err != nil || !ruleAssignmentFits(f.Type, set.Expr.Kind) {
					return fmt.Errorf("action %s assignment type differs", a.Name)
				}
				if err := check(*set.Expr, s, "action "+a.Name+" set "+set.Field); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func ruleAssignmentFits(dst, src string) bool {
	dst = platform.RuleFieldKind(dst)
	src = platform.RuleFieldKind(src)
	return dst == src || dst == "text" && src == "choice" || (dst == "decimal" || dst == "number") && (src == "integer" || src == "decimal" || src == "number")
}

func (b *Build) checkRuleCollections(s platform.RuleReadSpec, where string, lookups ...func(string) (platform.EntityInfo, bool)) error {
	lookup := b.conditionLookup
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	for name, set := range s.Collections {
		if set.Set != "" {
			info, ok := lookup(s.Object)
			if !ok || set.App != info.App || set.Query != "" || set.Of != "" && set.Of != "$subject" || !slices.ContainsFunc(info.Sets, func(declaration platform.RecordSet) bool {
				return declaration.Name == set.Set && declaration.Object == set.Object
			}) {
				return fmt.Errorf("%s collection %s must bind a declared set of its object", where, name)
			}
			continue
		}
		if b.host == nil {
			return fmt.Errorf("%s collection %s is unavailable", where, name)
		}
		if set.Of != "" && set.Of != "$subject" {
			_, selected, err := platform.RuleCollectionSubjectField(s.Object, set.Of, lookup)
			if err != nil {
				return err
			}
			if _, ok := s.Inputs[set.Of]; !ok && !selected {
				return fmt.Errorf("%s collection %s needs a declared input", where, name)
			}
		}
		q, version, ok := b.host.RuleQuery(set.App, set.Query, set.Version)
		if set.App == construction.ID {
			if _, selected := b.jointDeclarations[platform.AssetRef{App: set.App, Kind: platform.AssetQuery, Name: set.Query}]; selected {
				q, version, ok = b.QueryDefinition(set.Query, set.Version)
			}
		}
		if !ok || version != set.Version || q.Object != set.Object || (q.By == "") != (set.Of == "") {
			return fmt.Errorf("%s collection %s does not bind its retained query/object/input", where, name)
		}
	}
	return nil
}
