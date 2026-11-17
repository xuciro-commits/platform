package authoring

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ruleexpr "platformserver/expr"
	"platformserver/platform"
)

func conditionBinding(ref string) (platform.Binding, error) {
	parts := strings.Split(ref, ".")
	binding := platform.Binding{Source: parts[0], Path: parts[1:]}
	switch binding.Source {
	case "step":
		if len(parts) < 2 {
			return platform.Binding{}, fmt.Errorf("condition reference %s needs a node", ref)
		}
		binding.Step, binding.Path = parts[1], parts[2:]
	case "index", "answer":
		if len(binding.Path) > 0 {
			return platform.Binding{}, fmt.Errorf("condition reference %s has no fields", ref)
		}
	case "input", "subject", "item":
	default:
		return platform.Binding{}, fmt.Errorf("condition reference %s names no workflow value", ref)
	}
	if len(binding.Path) == 0 {
		binding.Path = nil
	}
	return binding, binding.Check()
}

func conditionBindings(expression platform.Expr) ([]platform.Binding, error) {
	var out []platform.Binding
	for _, ref := range ruleexpr.Refs(expression) {
		binding, err := conditionBinding(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, nil
}

func compileCondition(expression platform.Expr, engines ...platform.RuleEngine) (platform.PredicateProgram, error) {
	if expression.Kind != "boolean" {
		return nil, fmt.Errorf("condition must be a boolean expression")
	}
	var plain func(platform.Expr) bool
	plain = func(current platform.Expr) bool {
		if current.Agg != nil || current.Window != nil || current.In != nil || current.Op == "path" || strings.HasPrefix(current.Op, "$") {
			return false
		}
		for _, argument := range current.Args {
			if !plain(argument) {
				return false
			}
		}
		return true
	}
	if !plain(expression) {
		return nil, fmt.Errorf("a workflow condition compares workflow values only; it cannot read the clock, the caller or collections")
	}
	schema := ruleexpr.Schema{Refs: map[string]ruleexpr.Type{}}
	for _, ref := range ruleexpr.Refs(expression) {
		if _, err := conditionBinding(ref); err != nil {
			return nil, err
		}
		schema.Refs[ref] = ruleexpr.Type{Kind: "json"}
	}
	return platform.RuleEngineOf(engines).CompilePredicate(expression, schema)
}

func checkCondition(expression platform.Expr, engines ...platform.RuleEngine) error {
	_, err := compileCondition(expression, engines...)
	return err
}

type conditionValues struct {
	run     *platform.Run
	subject json.RawMessage
}

func (values conditionValues) Ref(name string) (ruleexpr.Value, bool) {
	binding, err := conditionBinding(name)
	if err != nil {
		return ruleexpr.Value{}, false
	}
	raw, err := binding.Resolve(values.run, values.subject)
	if err != nil {
		raw = json.RawMessage("null")
	}
	return ruleexpr.PresentRaw("json", raw), true
}

func testCondition(expression platform.Expr, run *platform.Run, subject json.RawMessage, engines ...platform.RuleEngine) (bool, error) {
	program, err := compileCondition(expression, engines...)
	if err != nil {
		return false, err
	}
	answer, err := program.EvalSource(conditionValues{run: run, subject: subject})
	var issue *ruleexpr.Issue
	if errors.As(err, &issue) {
		if issue.Message == "malformed typed value" {
			return false, fmt.Errorf("condition operands are invalid JSON")
		}
		return false, fmt.Errorf("%s", issue.Message)
	}
	return err == nil && string(answer.Value.Data) == "true", err
}
