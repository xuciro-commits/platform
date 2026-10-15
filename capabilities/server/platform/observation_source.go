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

const ObservationMaxWindowSeconds = 366 * 24 * 60 * 60
const ObservationMaxPoints = 5000

// ObservationDefinition declares only the logical source and a bounded read.
// Addresses, credentials, retention and original points stay with the source.
type ObservationDefinition struct {
	Name                       string               `json:"name"`
	Title                      string               `json:"title"`
	Description                string               `json:"description,omitempty"`
	Connection                 AssetRef             `json:"connection"`
	Object                     AssetRef             `json:"object"`
	DeviceField                string               `json:"deviceField"`
	Signal                     string               `json:"signal"`
	Unit                       string               `json:"unit"`
	Path                       string               `json:"path"`
	WindowSeconds              int                  `json:"windowSeconds"`
	ExpectedRetentionSeconds   int                  `json:"expectedRetentionSeconds"`
	ExpectedGranularitySeconds int                  `json:"expectedGranularitySeconds"`
	Roles                      []string             `json:"roles"`
	Scheduled                  bool                 `json:"scheduled,omitempty"`
	RequireComplete            bool                 `json:"requireComplete,omitempty"`
	Decision                   *ObservationDecision `json:"decision,omitempty"`
}

type ObservationDecision struct {
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
	Ref      AssetRef           `json:"ref"`
	Version  string             `json:"version"`
	Object   AssetRef           `json:"object"`
	Record   string             `json:"record"`
	Device   string             `json:"device"`
	Signal   string             `json:"signal"`
	Window   ObservationWindow  `json:"window"`
	Points   []ObservationPoint `json:"points,omitempty"`
	Sources  []string           `json:"sources"`
	Matched  *bool              `json:"matched,omitempty"`
	Coverage *DriverCoverage    `json:"coverage,omitempty"`
	Latest   *DriverSample      `json:"latest,omitempty"`
}

// A current reading carries its quality and time. It is never a window summary
// and cannot enter the window decision evaluator.
func CheckObservationLatest(sample DriverSample, device, signal, unit string) error {
	if sample.Device != device || sample.Signal != signal || sample.Unit != unit || sample.At.IsZero() || sample.ReceivedAt.IsZero() || !slices.Contains([]string{"good", "bad", "uncertain", "stale"}, sample.Quality) || !slices.Contains([]string{"integer", "decimal", "number"}, sample.Type) {
		return fmt.Errorf("current observation differs from its declared numeric point, unit or quality")
	}
	if sample.Value == nil {
		if sample.Quality == "good" {
			return fmt.Errorf("a good current observation needs a value")
		}
		return nil
	}
	if sample.Quality == "bad" {
		return fmt.Errorf("a bad current observation cannot carry a valid value")
	}
	value, ok := sample.Value.(string)
	if !ok || len(value) > 256 {
		return fmt.Errorf("current numeric observations preserve their exact text value")
	}
	kind := sample.Type
	if kind == "number" {
		kind = "decimal"
	}
	program, err := CompileExpr(ExprLiteralValue(kind, value), ExprSchema{})
	if err != nil {
		return err
	}
	_, err = program.Eval(ExprContext{})
	return err
}

// CheckObservationCoverage validates the source's completeness claim without reconstructing limited points.
func CheckObservationCoverage(coverage DriverCoverage, window ObservationWindow) error {
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

func (d ObservationDefinition) Check() error {
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
	if d.Scheduled && d.Decision == nil {
		return fmt.Errorf("scheduled observation requires its declared business decision")
	}
	if d.Decision != nil {
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

func CheckObservationDefinition(def ObservationDefinition, lookup func(string) (EntityInfo, bool)) error {
	if err := def.Check(); err != nil {
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
	if _, err := CompileExpr(def.Decision.When, schema); err != nil {
		return fmt.Errorf("observation decision condition: %w", err)
	}
	for name, expr := range def.Decision.Inputs {
		if _, err := CompileExpr(expr, schema); err != nil {
			return fmt.Errorf("observation decision input %s: %w", name, err)
		}
	}
	return nil
}

func ObservationRuleSchema(def ObservationDefinition, entity EntityInfo) (ExprSchema, error) {
	schema := ExprSchema{Refs: map[string]ExprType{"record.id": {Kind: "text"}, "observation.latest": {Kind: "decimal"}, "observation.lastat": {Kind: "datetime"}}, Windows: map[string]ExprWindowType{"observation": {Unit: def.Unit}}}
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

// ObservationRuleContext copies only declared record scalars and one summary.
// There is no field through which raw observation points can enter the rule.
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

func EvaluateObservation(def ObservationDefinition, entity EntityInfo, record json.RawMessage, window ObservationWindow, at time.Time) (bool, map[string]json.RawMessage, error) {
	inputs := map[string]json.RawMessage{}
	if err := CheckObservationDefinition(def, func(typ string) (EntityInfo, bool) { return entity, typ == entity.Type }); err != nil {
		return false, inputs, err
	}
	if def.Decision == nil {
		return false, inputs, fmt.Errorf("observation has no declared business decision")
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
	condition, err := CompileExpr(def.Decision.When, schema)
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
		program, err := CompileExpr(expr, schema)
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

// CheckObservationAction preserves the original action and its typed payload.
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
