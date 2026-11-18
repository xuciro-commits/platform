package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

func CheckObservationCoverage(coverage platform.ObservationCoverage, window platform.ObservationWindow) error {
	if coverage.AvailableSince.IsZero() || coverage.CompleteUntil.IsZero() || coverage.AvailableSince.After(window.Since) || coverage.CompleteUntil.Before(window.Until) || coverage.Truncated {
		return fmt.Errorf("observation window is not complete or is outside available storage")
	}
	if coverage.Good != window.Count || coverage.Bad != 0 || coverage.Uncertain != 0 || coverage.Stale != 0 {
		return fmt.Errorf("observation window contains missing, uncertain or stale measurements")
	}
	return nil
}

func CheckObservationDefinition(def platform.ObservationDefinition, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	if err := def.Check(engines...); err != nil {
		return err
	}
	entity, found := lookup(def.Object.Name)
	if !found || entity.App != def.Object.App {
		return fmt.Errorf("observation object must retain its original owner")
	}
	device, found := entity.Field(def.DeviceField)
	if !found || !slices.Contains([]string{"text", "choice", "reference"}, device.Type) {
		return fmt.Errorf("observation device field must be readable scalar text or reference")
	}
	schema, err := ObservationRuleSchema(def, entity)
	if err != nil {
		return err
	}
	if def.Decision == nil {
		return nil
	}
	if _, err := platform.RuleEngineOf(engines).CompileExpr(def.Decision.When, schema); err != nil {
		return fmt.Errorf("observation decision condition: %w", err)
	}
	for name, expr := range def.Decision.Inputs {
		if _, err := platform.RuleEngineOf(engines).CompileExpr(expr, schema); err != nil {
			return fmt.Errorf("observation decision input %s: %w", name, err)
		}
	}
	return nil
}

func ObservationRuleSchema(def platform.ObservationDefinition, entity platform.EntityInfo) (platform.ExprSchema, error) {
	schema := platform.ExprSchema{Refs: map[string]platform.ExprType{"record.id": {Kind: "text"}, "observation.latest": {Kind: "decimal"}, "observation.lastat": {Kind: "datetime"}}, Windows: map[string]platform.ExprWindowType{"observation": {Unit: def.Unit}}}
	if def.Decision != nil && def.Decision.Mode == "intervals" {
		schema.Refs = map[string]platform.ExprType{"record.id": {Kind: "text"}, "intervals.count": {Kind: "integer"}, "intervals.since": {Kind: "datetime"}, "intervals.until": {Kind: "datetime"}}
		schema.Windows = nil
	}
	if entity.Type != def.Object.Name || entity.App != def.Object.App {
		return schema, fmt.Errorf("observation rule object differs from its declaration")
	}
	for _, field := range entity.Fields {
		kind := platform.RuleFieldKind(field.Type)
		if slices.Contains([]string{"text", "integer", "decimal", "number", "money", "boolean", "date", "datetime", "choice", "reference"}, kind) {
			schema.Refs["record."+field.Name] = platform.ExprType{Kind: kind, Object: field.Ref}
		}
	}
	return schema, nil
}

func ObservationRuleContext(def platform.ObservationDefinition, record json.RawMessage, window platform.ObservationWindow) (platform.ExprContext, error) {
	context := platform.ExprContext{Refs: map[string]platform.ExprValue{}, Windows: map[string]platform.ExprWindowSummary{"observation": window}}
	if err := window.Check(); err != nil {
		return context, err
	}
	if window.Unit != def.Unit {
		return context, fmt.Errorf("observation window unit differs from its declaration")
	}
	if window.Until.Sub(window.Since) > time.Duration(def.WindowSeconds)*time.Second {
		return context, fmt.Errorf("observation window exceeds its declared bound")
	}
	var values map[string]json.RawMessage
	if len(record) == 0 || len(record) > 1<<20 || json.Unmarshal(record, &values) != nil || values == nil {
		return context, fmt.Errorf("observation rules need the original authorized record")
	}
	var refs func(platform.Expr)
	refs = func(expr platform.Expr) {
		if strings.HasPrefix(expr.Ref, "record.") {
			name := strings.TrimPrefix(expr.Ref, "record.")
			if raw, exists := values[name]; exists {
				status := "value"
				if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					status = "null"
				}
				context.Refs[expr.Ref] = platform.ExprValue{Kind: expr.Kind, Status: status, Data: slices.Clone(raw)}
			}
		}
		for _, child := range expr.Args {
			refs(child)
		}
		if expr.In != nil {
			refs(*expr.In)
		}
	}
	if def.Decision != nil {
		refs(def.Decision.When)
		for _, expr := range def.Decision.Inputs {
			refs(expr)
		}
	}
	if window.Count == 0 {
		context.Refs["observation.latest"] = platform.ExprValue{Kind: "decimal", Status: "missing"}
		context.Refs["observation.lastat"] = platform.ExprValue{Kind: "datetime", Status: "missing"}
	} else {
		context.Refs["observation.latest"] = platform.ExprPresent("decimal", window.Last)
		context.Refs["observation.lastat"] = platform.ExprPresent("datetime", window.LastAt.UTC().Format(time.RFC3339Nano))
	}
	return context, nil
}

func EvaluateObservation(def platform.ObservationDefinition, entity platform.EntityInfo, record json.RawMessage, window platform.ObservationWindow, at time.Time, engines ...platform.RuleEngine) (bool, map[string]json.RawMessage, error) {
	return evaluateObservationResult(platform.RuleEngineOf(engines), def, entity, record, window, nil, at)
}

func EvaluateObservationIntervals(def platform.ObservationDefinition, entity platform.EntityInfo, record json.RawMessage, result platform.ObservationIntervalResult, at time.Time, engines ...platform.RuleEngine) (bool, map[string]json.RawMessage, error) {
	if err := result.Check(); err != nil {
		return false, nil, err
	}
	return evaluateObservationResult(platform.RuleEngineOf(engines), def, entity, record, platform.ObservationWindow{Since: result.Since, Until: result.Until, Unit: def.Unit}, &result, at)
}

func evaluateObservationResult(engine platform.RuleEngine, def platform.ObservationDefinition, entity platform.EntityInfo, record json.RawMessage, window platform.ObservationWindow, intervals *platform.ObservationIntervalResult, at time.Time) (bool, map[string]json.RawMessage, error) {
	inputs := map[string]json.RawMessage{}
	if err := CheckObservationDefinition(def, func(typ string) (platform.EntityInfo, bool) { return entity, typ == entity.Type }, engine); err != nil {
		return false, inputs, err
	}
	if def.Decision == nil {
		return false, inputs, fmt.Errorf("observation has no declared business decision")
	}
	if (def.Decision.Mode == "intervals") != (intervals != nil) {
		return false, inputs, fmt.Errorf("observation decision result differs from its declared mode")
	}
	schema, err := ObservationRuleSchema(def, entity)
	if err != nil {
		return false, inputs, err
	}
	context, err := ObservationRuleContext(def, record, window)
	if err != nil {
		return false, inputs, err
	}
	context.Now = at.UTC()
	context.Today = at.UTC().Format(time.DateOnly)
	if intervals != nil {
		context.Refs["intervals.count"] = platform.ExprPresent("integer", len(intervals.Intervals))
		context.Refs["intervals.since"] = platform.ExprPresent("datetime", intervals.Since.UTC().Format(time.RFC3339Nano))
		context.Refs["intervals.until"] = platform.ExprPresent("datetime", intervals.Until.UTC().Format(time.RFC3339Nano))
	}
	condition, err := engine.CompileExpr(def.Decision.When, schema)
	if err != nil {
		return false, inputs, err
	}
	result, err := condition.Eval(context)
	if err != nil {
		return false, inputs, err
	}
	if !bytes.Equal(result.Value.Data, []byte("true")) {
		return false, inputs, nil
	}
	for name, expr := range def.Decision.Inputs {
		program, err := engine.CompileExpr(expr, schema)
		if err != nil {
			return false, inputs, err
		}
		result, err := program.Eval(context)
		if err != nil {
			return false, inputs, err
		}
		value, err := ExprGoValue(result.Value)
		if err != nil {
			return false, inputs, fmt.Errorf("observation decision input %s: %w", name, err)
		}
		raw, err := json.Marshal(value)
		if err != nil || len(raw) > 8192 || bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
			return false, inputs, fmt.Errorf("observation decision inputs require compact scalar results")
		}
		inputs[name] = raw
	}
	return true, inputs, nil
}

func CheckObservationWindowRange(window platform.ObservationWindow, since, until time.Time, unit string) error {
	if err := window.Check(); err != nil {
		return err
	}
	if !window.Since.Equal(since) || !window.Until.Equal(until) || window.Unit != unit {
		return fmt.Errorf("observation response differs from its requested window or unit")
	}
	return nil
}

func CheckObservationPoints(points []platform.ObservationPoint, window platform.ObservationWindow, limit int) error {
	if limit < 1 || limit > platform.ObservationMaxPoints || len(points) > limit || int64(len(points)) > window.Count {
		return fmt.Errorf("observation points exceed the requested bounded read")
	}
	for i, point := range points {
		if point.At.Before(window.Since) || !point.At.Before(window.Until) || i > 0 && !points[i-1].At.Before(point.At) {
			return fmt.Errorf("observation points must be ordered within the requested window")
		}
		if err := platform.CheckObservationDecimal(point.Value); err != nil {
			return err
		}
	}
	return nil
}

func CheckObservationAction(def platform.ObservationDefinition, action platform.Action) error {
	if def.Decision == nil {
		return nil
	}
	if action.Schema != def.Decision.Action.Name || action.Target != def.Object.Name || action.New || action.Automation {
		return fmt.Errorf("observation decision needs an existing-record action of its original object")
	}
	for name, expr := range def.Decision.Inputs {
		index := slices.IndexFunc(action.Payload, func(field platform.Field) bool { return field.Name == name })
		if index < 0 {
			return fmt.Errorf("observation decision input %s is not declared by the original action", name)
		}
		want, got := platform.RuleFieldKind(action.Payload[index].Type), platform.RuleFieldKind(expr.Kind)
		if want == "number" {
			want = "decimal"
		}
		if got == "number" {
			got = "decimal"
		}
		if want != got && !((want == "decimal" || want == "number") && got == "integer") {
			return fmt.Errorf("observation decision input %s differs from the original action type", name)
		}
	}
	for _, field := range action.Payload {
		if field.Required {
			if _, present := def.Decision.Inputs[field.Name]; !present {
				return fmt.Errorf("observation decision must supply required action input %s", field.Name)
			}
		}
	}
	return nil
}
