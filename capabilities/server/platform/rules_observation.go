package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

type ObservationContext struct {
	Definition AssetBinding
	Record     string
	Intervals  *ObservationIntervalResult
}

type ObservationContextRuntime interface {
	ObservationContext(Caller) *ObservationContext
}

func (c Caller) ObservationContext() *ObservationContext {
	if c.Replaying || !c.Staging() {
		return nil
	}
	if runtime, ok := c.rt.(ObservationContextRuntime); ok {
		return runtime.ObservationContext(c)
	}
	return nil
}

func (c Caller) observationRuleFacts() map[string]ExprValue {
	out := map[string]ExprValue{"observation.ready": ExprPresent("boolean", false), "observation.source": {Kind: "text", Status: "missing"}, "observation.object": {Kind: "text", Status: "missing"}, "observation.subject": {Kind: "text", Status: "missing"}}
	if proof := c.ObservationContext(); proof != nil && proof.Intervals != nil && proof.Intervals.Ready {
		object, subject, ok := strings.Cut(proof.Record, "/")
		if !ok {
			return out
		}
		out["observation.ready"] = ExprPresent("boolean", true)
		out["observation.source"] = ExprPresent("text", proof.Definition.Ref.String())
		out["observation.object"] = ExprPresent("text", object)
		out["observation.subject"] = ExprPresent("text", subject)
	}
	return out
}

type ObservationSampleSource struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
	Generation string `json:"generation"`
	Sequence   string `json:"sequence,omitempty"`
	Clock      string `json:"clock,omitempty"`
}

type ObservationSample struct {
	Device     string                  `json:"device"`
	Signal     string                  `json:"signal"`
	Type       string                  `json:"type"`
	Value      any                     `json:"value,omitempty"`
	Unit       string                  `json:"unit,omitempty"`
	Quality    string                  `json:"quality"`
	At         time.Time               `json:"at"`
	ReceivedAt time.Time               `json:"receivedAt"`
	Reason     string                  `json:"reason,omitempty"`
	Source     ObservationSampleSource `json:"source"`
}

type ObservationCoverage struct {
	AvailableSince time.Time `json:"availableSince"`
	CompleteUntil  time.Time `json:"completeUntil"`
	Truncated      bool      `json:"truncated"`
	Good           int64     `json:"good"`
	Bad            int64     `json:"bad"`
	Uncertain      int64     `json:"uncertain"`
	Stale          int64     `json:"stale"`
}

const ObservationMaxWindowSeconds = 366 * 24 * 60 * 60

const ObservationMaxPoints = 5000

type ObservationDefinition struct {
	Name                       string                `json:"name"`
	Title                      string                `json:"title"`
	Description                string                `json:"description,omitempty"`
	Connection                 AssetRef              `json:"connection"`
	Object                     AssetRef              `json:"object"`
	DeviceField                string                `json:"deviceField"`
	Signal                     string                `json:"signal"`
	Unit                       string                `json:"unit"`
	Path                       string                `json:"path"`
	WindowSeconds              int                   `json:"windowSeconds"`
	ExpectedRetentionSeconds   int                   `json:"expectedRetentionSeconds"`
	ExpectedGranularitySeconds int                   `json:"expectedGranularitySeconds"`
	Roles                      []string              `json:"roles"`
	Scheduled                  bool                  `json:"scheduled,omitempty"`
	RequireComplete            bool                  `json:"requireComplete,omitempty"`
	Decision                   *ObservationDecision  `json:"decision,omitempty"`
	Intervals                  *ObservationIntervals `json:"intervals,omitempty"`
}

type ObservationDecision struct {
	Mode   string          `json:"mode,omitempty" enum:"summary,intervals"`
	When   Expr            `json:"when"`
	Action AssetRef        `json:"action"`
	Inputs map[string]Expr `json:"inputs"`
}

type ObservationWindow = ExprWindowSummary

type ObservationPoint struct {
	At    time.Time `json:"at"`
	Value string    `json:"value"`
}

type ObservationResult struct {
	Ref       AssetRef                   `json:"ref"`
	Version   string                     `json:"version"`
	Object    AssetRef                   `json:"object"`
	Record    string                     `json:"record"`
	Device    string                     `json:"device"`
	Signal    string                     `json:"signal"`
	Window    ObservationWindow          `json:"window"`
	Points    []ObservationPoint         `json:"points,omitempty"`
	Sources   []string                   `json:"sources"`
	Matched   *bool                      `json:"matched,omitempty"`
	Coverage  *ObservationCoverage       `json:"coverage,omitempty"`
	Latest    *ObservationSample         `json:"latest,omitempty"`
	Intervals *ObservationIntervalResult `json:"intervals,omitempty"`
}

func CheckObservationCoverage(coverage ObservationCoverage, window ObservationWindow) error {
	if coverage.AvailableSince.IsZero() || coverage.CompleteUntil.IsZero() || coverage.AvailableSince.After(window.Since) || coverage.CompleteUntil.Before(window.Until) || coverage.Truncated {
		return fmt.Errorf("observation window is not complete or is outside available storage")
	}
	if coverage.Good != window.Count || coverage.Bad != 0 || coverage.Uncertain != 0 || coverage.Stale != 0 {
		return fmt.Errorf("observation window contains missing, uncertain or stale measurements")
	}
	return nil
}

func (d *ObservationDefinition) UnmarshalJSON(raw []byte) error {
	type plain ObservationDefinition
	return strictTxn(raw, (*plain)(d))
}

func (d *ObservationDecision) UnmarshalJSON(raw []byte) error {
	type plain ObservationDecision
	return strictTxn(raw, (*plain)(d))
}

func (d ObservationDefinition) Check(engines ...RuleEngine) error {
	if !functionName.MatchString(d.Name) || strings.TrimSpace(d.Title) == "" || len(d.Title) > 256 || len(d.Description) > 4096 || d.Connection.Check() != nil || d.Connection.Kind != AssetConnection || d.Object.Check() != nil || d.Object.Kind != AssetObject || !functionFieldName.MatchString(d.DeviceField) || strings.TrimSpace(d.Signal) == "" || len(d.Signal) > 128 || strings.TrimSpace(d.Unit) == "" || len(d.Unit) > 64 || d.WindowSeconds < 1 || d.WindowSeconds > ObservationMaxWindowSeconds || d.ExpectedRetentionSeconds < d.WindowSeconds || d.ExpectedRetentionSeconds > 10*ObservationMaxWindowSeconds || d.ExpectedGranularitySeconds < 1 || d.ExpectedGranularitySeconds > d.WindowSeconds || len(d.Roles) == 0 || len(d.Roles) > 64 {
		return fmt.Errorf("observation needs a logical connection, object/device field, signal/unit, bounded window/retention/granularity and roles")
	}
	path, err := url.Parse(d.Path)
	if err != nil || !strings.HasPrefix(d.Path, "/") || strings.HasPrefix(d.Path, "//") || strings.ContainsAny(d.Path, "\\\r\n") || strings.Contains(strings.ToLower(d.Path), "%2f") || strings.Contains(strings.ToLower(d.Path), "%5c") || path.IsAbs() || path.Host != "" || path.User != nil || path.RawQuery != "" || path.Fragment != "" || len(d.Path) > 1024 {
		return fmt.Errorf("observation adapter path must be relative without addresses, credentials or query parameters")
	}
	for _, part := range strings.Split(path.Path, "/") {
		if part == "." || part == ".." {
			return fmt.Errorf("observation adapter path cannot traverse outside its relative endpoint")
		}
	}
	if strings.ContainsAny(path.Path, "\\\r\n") {
		return fmt.Errorf("observation adapter path contains an invalid separator")
	}
	for i, role := range d.Roles {
		if role == "" || slices.Contains(d.Roles[:i], role) {
			return fmt.Errorf("observation roles must be named and unique")
		}
	}
	if d.Intervals != nil {
		if err := d.Intervals.Check(d.WindowSeconds, engines...); err != nil {
			return err
		}
	}
	if d.Scheduled && d.Decision == nil {
		return fmt.Errorf("scheduled observation requires its declared business decision")
	}
	if d.Decision != nil {
		if !slices.Contains([]string{"", "summary", "intervals"}, d.Decision.Mode) || d.Decision.Mode == "intervals" && d.Intervals == nil {
			return fmt.Errorf("observation decision mode needs its declared source analysis")
		}
		if d.Decision.When.Kind != "boolean" || d.Decision.Action.Check() != nil || d.Decision.Action.Kind != AssetAction || d.Decision.Action.App != d.Object.App || len(d.Decision.Inputs) > 64 {
			return fmt.Errorf("observation decision needs a boolean condition and the original object's authorized action")
		}
		for name, expr := range d.Decision.Inputs {
			if !functionFieldName.MatchString(name) || expr.Kind == "set" {
				return fmt.Errorf("observation decision inputs must be named compact scalar values")
			}
		}
	}
	return nil
}

func CheckObservationDefinition(def ObservationDefinition, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) error {
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
	if _, err := ruleEngineOf(engines).CompileExpr(def.Decision.When, schema); err != nil {
		return fmt.Errorf("observation decision condition: %w", err)
	}
	for name, expr := range def.Decision.Inputs {
		if _, err := ruleEngineOf(engines).CompileExpr(expr, schema); err != nil {
			return fmt.Errorf("observation decision input %s: %w", name, err)
		}
	}
	return nil
}

func ObservationRuleSchema(def ObservationDefinition, entity EntityInfo) (ExprSchema, error) {
	schema := ExprSchema{Refs: map[string]ExprType{"record.id": {Kind: "text"}, "observation.latest": {Kind: "decimal"}, "observation.lastat": {Kind: "datetime"}}, Windows: map[string]ExprWindowType{"observation": {Unit: def.Unit}}}
	if def.Decision != nil && def.Decision.Mode == "intervals" {
		schema.Refs = map[string]ExprType{"record.id": {Kind: "text"}, "intervals.count": {Kind: "integer"}, "intervals.since": {Kind: "datetime"}, "intervals.until": {Kind: "datetime"}}
		schema.Windows = nil
	}
	if entity.Type != def.Object.Name || entity.App != def.Object.App {
		return schema, fmt.Errorf("observation rule object differs from its declaration")
	}
	for _, field := range entity.Fields {
		kind := RuleFieldKind(field.Type)
		if slices.Contains([]string{"text", "integer", "decimal", "number", "money", "boolean", "date", "datetime", "choice", "reference"}, kind) {
			schema.Refs["record."+field.Name] = ExprType{Kind: kind, Object: field.Ref}
		}
	}
	return schema, nil
}

func ObservationRuleContext(def ObservationDefinition, record json.RawMessage, window ObservationWindow) (ExprContext, error) {
	context := ExprContext{Refs: map[string]ExprValue{}, Windows: map[string]ExprWindowSummary{"observation": window}}
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
	var refs func(Expr)
	refs = func(expr Expr) {
		if strings.HasPrefix(expr.Ref, "record.") {
			name := strings.TrimPrefix(expr.Ref, "record.")
			if raw, exists := values[name]; exists {
				status := "value"
				if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
					status = "null"
				}
				context.Refs[expr.Ref] = ExprValue{Kind: expr.Kind, Status: status, Data: slices.Clone(raw)}
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
		context.Refs["observation.latest"] = ExprValue{Kind: "decimal", Status: "missing"}
		context.Refs["observation.lastat"] = ExprValue{Kind: "datetime", Status: "missing"}
	} else {
		context.Refs["observation.latest"] = ExprPresent("decimal", window.Last)
		context.Refs["observation.lastat"] = ExprPresent("datetime", window.LastAt.UTC().Format(time.RFC3339Nano))
	}
	return context, nil
}

func EvaluateObservation(def ObservationDefinition, entity EntityInfo, record json.RawMessage, window ObservationWindow, at time.Time, engines ...RuleEngine) (bool, map[string]json.RawMessage, error) {
	return evaluateObservationResult(ruleEngineOf(engines), def, entity, record, window, nil, at)
}

func EvaluateObservationIntervals(def ObservationDefinition, entity EntityInfo, record json.RawMessage, result ObservationIntervalResult, at time.Time, engines ...RuleEngine) (bool, map[string]json.RawMessage, error) {
	if err := result.Check(); err != nil {
		return false, nil, err
	}
	return evaluateObservationResult(ruleEngineOf(engines), def, entity, record, ObservationWindow{Since: result.Since, Until: result.Until, Unit: def.Unit}, &result, at)
}

func evaluateObservationResult(engine RuleEngine, def ObservationDefinition, entity EntityInfo, record json.RawMessage, window ObservationWindow, intervals *ObservationIntervalResult, at time.Time) (bool, map[string]json.RawMessage, error) {
	inputs := map[string]json.RawMessage{}
	if err := CheckObservationDefinition(def, func(typ string) (EntityInfo, bool) { return entity, typ == entity.Type }, engine); err != nil {
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
		context.Refs["intervals.count"] = ExprPresent("integer", len(intervals.Intervals))
		context.Refs["intervals.since"] = ExprPresent("datetime", intervals.Since.UTC().Format(time.RFC3339Nano))
		context.Refs["intervals.until"] = ExprPresent("datetime", intervals.Until.UTC().Format(time.RFC3339Nano))
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

func CheckObservationWindowRange(window ObservationWindow, since, until time.Time, unit string) error {
	if err := window.Check(); err != nil {
		return err
	}
	if !window.Since.Equal(since) || !window.Until.Equal(until) || window.Unit != unit {
		return fmt.Errorf("observation response differs from its requested window or unit")
	}
	return nil
}

func CheckObservationPoints(points []ObservationPoint, window ObservationWindow, limit int) error {
	if limit < 1 || limit > ObservationMaxPoints || len(points) > limit || int64(len(points)) > window.Count {
		return fmt.Errorf("observation points exceed the requested bounded read")
	}
	for i, point := range points {
		if point.At.Before(window.Since) || !point.At.Before(window.Until) || i > 0 && !points[i-1].At.Before(point.At) {
			return fmt.Errorf("observation points must be ordered within the requested window")
		}
		if err := CheckObservationDecimal(point.Value); err != nil {
			return err
		}
	}
	return nil
}

func CheckObservationAction(def ObservationDefinition, action Action) error {
	if def.Decision == nil {
		return nil
	}
	if action.Schema != def.Decision.Action.Name || action.Target != def.Object.Name || action.New || action.Automation {
		return fmt.Errorf("observation decision needs an existing-record action of its original object")
	}
	for name, expr := range def.Decision.Inputs {
		index := slices.IndexFunc(action.Payload, func(field Field) bool { return field.Name == name })
		if index < 0 {
			return fmt.Errorf("observation decision input %s is not declared by the original action", name)
		}
		want, got := RuleFieldKind(action.Payload[index].Type), RuleFieldKind(expr.Kind)
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
