package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/url"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/rules"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

type DecimalValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

var decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func ParseDecimal(text string) (DecimalValue, error) {
	if len(text) > pageWidgets.Runtime.Decimal.MaxBytes || !decimalText.MatchString(text) {
		return DecimalValue{}, fmt.Errorf("invalid bounded decimal")
	}
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	if text == "-0" {
		text = "0"
	}
	return DecimalValue{Kind: "decimal", Value: text}, nil
}
func (v DecimalValue) Check() error {
	parsed, err := ParseDecimal(v.Value)
	if err != nil || v.Kind != "decimal" || parsed != v {
		return fmt.Errorf("invalid canonical decimal")
	}
	return nil
}
func DecimalLiteral(raw json.RawMessage) (DecimalValue, bool) {
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 2 || shape["kind"] == nil || shape["value"] == nil {
		return DecimalValue{}, false
	}
	var value DecimalValue
	if json.Unmarshal(raw, &value) != nil || value.Check() != nil {
		return DecimalValue{}, false
	}
	return value, true
}
func (v DecimalValue) Rat() *big.Rat { value, _ := new(big.Rat).SetString(v.Value); return value }

func (value DecimalValue) Condition(field FieldInfo, op string, read func(reflect.Value) any) (func(reflect.Value) bool, error) {
	if field.Type != "integer" && field.Type != "decimal" {
		return nil, fmt.Errorf("decimal condition needs a numeric field")
	}
	if op != "=" && op != "!=" && op != "<" && op != "<=" && op != ">" && op != ">=" {
		return nil, fmt.Errorf("unsupported decimal condition")
	}
	right := value.Rat()
	return func(record reflect.Value) bool {
		raw, err := json.Marshal(read(record))
		if err != nil {
			return false
		}
		left, ok := new(big.Rat).SetString(string(raw))
		if !ok {
			return false
		}
		cmp := left.Cmp(right)
		switch op {
		case "=":
			return cmp == 0
		case "!=":
			return cmp != 0
		case "<":
			return cmp < 0
		case "<=":
			return cmp <= 0
		case ">":
			return cmp > 0
		case ">=":
			return cmp >= 0
		}
		return false
	}, nil
}

type NumberValue struct {
	Kind  string  `json:"kind"`
	Value float64 `json:"value"`
}

func NumberLiteral(raw json.RawMessage) (NumberValue, bool) {
	var shape map[string]json.RawMessage
	var v NumberValue
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 2 || shape["kind"] == nil || shape["value"] == nil || string(shape["value"]) == "null" || json.Unmarshal(raw, &v) != nil || v.Kind != "number" || math.IsNaN(v.Value) || math.IsInf(v.Value, 0) {
		return NumberValue{}, false
	}
	return v, true
}
func (d PageInputSchema) CheckAggregateScalar(v PageVariable, info EntityInfo) error {
	if v.Mode != "aggregate" || v.Source == nil || v.Source.Kind != "aggregate" && v.Source.Kind != "statistics" {
		return nil
	}
	_, field, ok := stringsCutMeasure(v.Source.Measure)
	if v.Source.Kind == "statistics" {
		field = v.Source.Measure
		ok = pageNodeID.MatchString(field)
	}
	f, found := info.Field(field)
	if !ok || !found || (f.Type != "integer" && f.Type != "decimal") {
		return fmt.Errorf("aggregate scalar needs an original visible numeric field")
	}
	return nil
}

func stringsCutMeasure(measure string) (string, string, bool) {
	op, field, ok := strings.Cut(measure, ":")
	return op, field, ok && (op == "sum" || op == "avg" || op == "min" || op == "max") && pageNodeID.MatchString(field)
}

func (d *PageDocument) CheckAggregateScalar(v PageVariable, info EntityInfo) error {
	return d.inputSchema().CheckAggregateScalar(v, info)
}

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

const ObservationIntervalVersion = "platform.interval.v1"
const ObservationMaxIntervals = 256

type ObservationIntervals struct {
	ValueKind       string `json:"valueKind" enum:"decimal,choice,boolean"`
	When            Expr   `json:"when"`
	MaxGapSeconds   int    `json:"maxGapSeconds"`
	LatePolicy      string `json:"latePolicy" enum:"reject,correct"`
	LatenessSeconds int    `json:"latenessSeconds"`
	MaxIntervals    int    `json:"maxIntervals"`
}

type ObservationStatePoint struct {
	At         time.Time `json:"at"`
	ReceivedAt time.Time `json:"receivedAt"`
	Quality    string    `json:"quality" enum:"good,bad,uncertain,stale"`
	Value      string    `json:"value,omitempty"`
}

type ObservationIntervalInput struct {
	Since    time.Time               `json:"since"`
	Until    time.Time               `json:"until"`
	Revision string                  `json:"revision"`
	Points   []ObservationStatePoint `json:"points"`
	Coverage *ObservationCoverage    `json:"coverage,omitempty"`
}

type ObservationInterval struct {
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
	OpenStart bool      `json:"openStart,omitempty"`
	OpenEnd   bool      `json:"openEnd,omitempty"`
}

type ObservationUnknown struct {
	Since  time.Time `json:"since"`
	Until  time.Time `json:"until"`
	Reason string    `json:"reason" enum:"coverage,gap,bad,uncertain,stale,late,rule"`
}

type ObservationIntervalResult struct {
	Version   string                `json:"version"`
	Revision  string                `json:"revision"`
	Since     time.Time             `json:"since"`
	Until     time.Time             `json:"until"`
	Ready     bool                  `json:"ready"`
	Count     int                   `json:"count"`
	Intervals []ObservationInterval `json:"intervals"`
	Unknown   []ObservationUnknown  `json:"unknown"`
}

func (d *ObservationIntervals) UnmarshalJSON(raw []byte) error {
	type plain ObservationIntervals
	return strictTxn(raw, (*plain)(d))
}

func intervalRuleSchema(kind string) ExprSchema {
	return ExprSchema{Refs: map[string]ExprType{"sample.value": {Kind: kind}, "sample.at": {Kind: "datetime"}, "sample.receivedAt": {Kind: "datetime"}}}
}

func (d ObservationIntervals) Check(windowSeconds int, engines ...RuleEngine) error {
	if !slices.Contains([]string{"decimal", "choice", "boolean"}, d.ValueKind) || d.When.Kind != "boolean" || d.MaxGapSeconds < 1 || d.MaxGapSeconds > windowSeconds || !slices.Contains([]string{"reject", "correct"}, d.LatePolicy) || d.LatenessSeconds < 0 || d.LatenessSeconds > windowSeconds || d.LatePolicy == "reject" && d.LatenessSeconds != 0 || d.MaxIntervals < 1 || d.MaxIntervals > ObservationMaxIntervals {
		return fmt.Errorf("state intervals need a typed condition, bounded gap/results and explicit late-data policy")
	}
	_, err := ruleEngineOf(engines).CompileExpr(d.When, intervalRuleSchema(d.ValueKind))
	return err
}

func intervalPointValue(kind, value string, engines ...RuleEngine) (ExprValue, error) {
	if kind == "decimal" {
		if err := CheckObservationDecimal(value); err != nil {
			return ExprValue{}, err
		}
	}
	var literal any = value
	if kind == "boolean" {
		if value != "true" && value != "false" {
			return ExprValue{}, fmt.Errorf("state observation boolean values must be true or false")
		}
		literal = value == "true"
	}
	if len(value) > 256 || kind == "choice" && value == "" {
		return ExprValue{}, fmt.Errorf("state observation value exceeds its declared scalar shape")
	}
	program, err := ruleEngineOf(engines).CompileExpr(ExprLiteralValue(kind, literal), ExprSchema{})
	if err != nil {
		return ExprValue{}, err
	}
	result, err := program.Eval(ExprContext{})
	return result.Value, err
}

func AnalyzeObservationIntervals(policy ObservationIntervals, input ObservationIntervalInput, now time.Time, engines ...RuleEngine) (ObservationIntervalResult, error) {
	out := ObservationIntervalResult{Version: ObservationIntervalVersion, Revision: input.Revision, Since: input.Since, Until: input.Until, Count: len(input.Points), Intervals: []ObservationInterval{}, Unknown: []ObservationUnknown{}}
	if input.Since.IsZero() || !input.Since.Before(input.Until) || input.Until.Sub(input.Since) > time.Duration(ObservationMaxWindowSeconds)*time.Second || input.Revision == "" || len(input.Revision) > 256 || len(input.Points) > ObservationMaxPoints {
		return out, fmt.Errorf("state observations need a bounded window and source revision")
	}
	if err := policy.Check(ObservationMaxWindowSeconds, engines...); err != nil {
		return out, err
	}
	condition, err := ruleEngineOf(engines).CompileExpr(policy.When, intervalRuleSchema(policy.ValueKind))
	if err != nil {
		return out, err
	}
	type segment struct {
		since, until time.Time
		matching     bool
		unknown      string
	}
	var segments []segment
	appendSegment := func(s segment) {
		if !s.since.Before(s.until) {
			return
		}
		if len(segments) > 0 {
			last := &segments[len(segments)-1]
			if last.until.Equal(s.since) && last.matching == s.matching && last.unknown == s.unknown {
				last.until = s.until
				return
			}
		}
		segments = append(segments, s)
	}
	counts := map[string]int64{}
	values := make([]ExprValue, len(input.Points))
	for i, point := range input.Points {
		if point.At.Before(input.Since) || !point.At.Before(input.Until) || i > 0 && !input.Points[i-1].At.Before(point.At) || point.ReceivedAt.IsZero() || point.ReceivedAt.Before(point.At) || point.ReceivedAt.After(now) || !slices.Contains([]string{"good", "bad", "uncertain", "stale"}, point.Quality) {
			return out, fmt.Errorf("state observation points must preserve ordered event time, arrival time and quality")
		}
		counts[point.Quality]++
		if point.Quality == "good" {
			value, err := intervalPointValue(policy.ValueKind, point.Value, engines...)
			if err != nil {
				return out, err
			}
			values[i] = value
		}
	}
	coverage := input.Coverage
	if coverage == nil || coverage.AvailableSince.IsZero() || coverage.AvailableSince.After(input.Since) || coverage.CompleteUntil.Before(input.Until) || coverage.Truncated || coverage.Good != counts["good"] || coverage.Bad != counts["bad"] || coverage.Uncertain != counts["uncertain"] || coverage.Stale != counts["stale"] {
		out.Unknown = append(out.Unknown, ObservationUnknown{Since: input.Since, Until: input.Until, Reason: "coverage"})
		return out, nil
	}
	cursor := input.Since
	for i, point := range input.Points {
		if cursor.Before(point.At) {
			appendSegment(segment{since: cursor, until: point.At, unknown: "gap"})
		}
		until := input.Until
		if i+1 < len(input.Points) {
			until = input.Points[i+1].At
		}
		expires := point.At.Add(time.Duration(policy.MaxGapSeconds) * time.Second)
		if expires.Before(until) {
			until = expires
		}
		s := segment{since: point.At, until: until}
		switch {
		case point.ReceivedAt.After(input.Until.Add(time.Duration(policy.LatenessSeconds) * time.Second)):
			s.unknown = "late"
		case point.Quality != "good":
			s.unknown = point.Quality
		default:
			context := ExprContext{Refs: map[string]ExprValue{"sample.value": values[i], "sample.at": {Kind: "datetime", Status: "value", Data: Raw(point.At.UTC())}, "sample.receivedAt": {Kind: "datetime", Status: "value", Data: Raw(point.ReceivedAt.UTC())}}, Now: now.UTC(), Today: now.UTC().Format(time.DateOnly)}
			evaluated, failure := condition.Eval(context)
			if failure != nil {
				return out, failure
			}
			if evaluated.Value.Status != "value" {
				s.unknown = "rule"
			} else {
				s.matching = bytes.Equal(evaluated.Value.Data, []byte("true"))
			}
		}
		appendSegment(s)
		cursor = until
	}
	if cursor.Before(input.Until) {
		appendSegment(segment{since: cursor, until: input.Until, unknown: "gap"})
	}
	for i, s := range segments {
		if s.unknown != "" {
			out.Unknown = append(out.Unknown, ObservationUnknown{Since: s.since, Until: s.until, Reason: s.unknown})
		} else if s.matching {
			out.Intervals = append(out.Intervals, ObservationInterval{Since: s.since, Until: s.until, OpenStart: i == 0 || segments[i-1].unknown != "", OpenEnd: i == len(segments)-1 || segments[i+1].unknown != ""})
		}
	}
	if len(out.Intervals)+len(out.Unknown) > policy.MaxIntervals {
		return out, fmt.Errorf("state observation intervals exceed their declared result bound")
	}
	out.Ready = len(out.Unknown) == 0
	return out, nil
}

func (r ObservationIntervalResult) Check() error {
	if r.Version != ObservationIntervalVersion || r.Revision == "" || len(r.Revision) > 256 || r.Since.IsZero() || !r.Since.Before(r.Until) || r.Until.Sub(r.Since) > time.Duration(ObservationMaxWindowSeconds)*time.Second || !r.Ready || r.Count < 1 || r.Count > ObservationMaxPoints || r.Intervals == nil || r.Unknown == nil || len(r.Unknown) != 0 || len(r.Intervals) > ObservationMaxIntervals || len(r.Intervals) > r.Count {
		return fmt.Errorf("accepted state intervals need complete bounded evidence and their processing version")
	}
	for i, event := range r.Intervals {
		if event.Since.Before(r.Since) || event.Until.After(r.Until) || !event.Since.Before(event.Until) || i > 0 && !r.Intervals[i-1].Until.Before(event.Since) || event.OpenStart != event.Since.Equal(r.Since) || event.OpenEnd != event.Until.Equal(r.Until) {
			return fmt.Errorf("accepted state interval bounds or open boundaries are invalid")
		}
	}
	return nil
}

func (r ObservationIntervalResult) Clone() ObservationIntervalResult {
	r.Intervals = slices.Clone(r.Intervals)
	r.Unknown = slices.Clone(r.Unknown)
	return r
}

type IntervalIdentity struct {
	ID       string
	Interval ObservationInterval
}
type IntervalChange struct {
	ID           string
	Interval     ObservationInterval
	New          bool
	Changed      bool
	Review       bool
	Predecessors []string
}
type IntervalReconciliation struct {
	Events    []IntervalChange
	Retired   []string
	Redirects []*pb.Redirect
}

func intervalOverlap(a, b ObservationInterval) bool {
	return a.Since.Before(b.Until) && b.Since.Before(a.Until) || a.OpenEnd && b.OpenStart && a.Until.Equal(b.Since) || b.OpenEnd && a.OpenStart && b.Until.Equal(a.Since)
}

func ReconcileObservationIntervals(object string, previous []IntervalIdentity, result ObservationIntervalResult, newID func(int) string, engines ...KernelFactory) (IntervalReconciliation, error) {
	out := IntervalReconciliation{Events: []IntervalChange{}, Retired: []string{}, Redirects: []*pb.Redirect{}}
	if err := result.Check(); err != nil {
		return out, err
	}
	if object == "" || newID == nil || len(previous) > RecordSetMaxRows {
		return out, fmt.Errorf("interval reconciliation needs its bounded original collection")
	}
	previous = slices.Clone(previous)
	slices.SortFunc(previous, func(a, b IntervalIdentity) int {
		if order := a.Interval.Since.Compare(b.Interval.Since); order != 0 {
			return order
		}
		return a.Interval.Until.Compare(b.Interval.Until)
	})
	entities := make([]*pb.EntityRef, 0, len(previous))
	seen := map[string]bool{}
	for i, old := range previous {
		if old.ID == "" || seen[old.ID] || old.Interval.Since.IsZero() || !old.Interval.Since.Before(old.Interval.Until) || i > 0 && previous[i-1].Interval.Until.After(old.Interval.Since) {
			return out, fmt.Errorf("current interval identities must be distinct ordered ranges")
		}
		seen[old.ID] = true
		entities = append(entities, &pb.EntityRef{Type: object, Id: old.ID})
	}
	var engine KernelFactory = NativeKernel{}
	if len(engines) != 0 {
		engine = engines[0]
	}
	identity := engine.Identity(entities)
	type candidate struct {
		interval ObservationInterval
		review   bool
	}
	candidates := make([]candidate, 0, len(result.Intervals)+2)
	for _, event := range result.Intervals {
		candidates = append(candidates, candidate{interval: event})
	}
	affected := []IntervalIdentity{}
	for _, old := range previous {
		event := old.Interval
		intersects := event.Since.Before(result.Until) && result.Since.Before(event.Until)
		touchesLeft := event.OpenEnd && event.Until.Equal(result.Since)
		touchesRight := event.OpenStart && event.Since.Equal(result.Until)
		if !intersects && !touchesLeft && !touchesRight {
			continue
		}
		affected = append(affected, old)
		if event.Since.Before(result.Since) || touchesLeft {
			left := -1
			for i, c := range candidates {
				if c.interval.OpenStart && c.interval.Since.Equal(result.Since) {
					left = i
					break
				}
			}
			if left >= 0 {
				candidates[left].interval.Since = event.Since
				candidates[left].interval.OpenStart = event.OpenStart
			} else {
				candidates = append(candidates, candidate{interval: ObservationInterval{Since: event.Since, Until: result.Since, OpenStart: event.OpenStart, OpenEnd: !touchesLeft}, review: !touchesLeft})
			}
		}
		if result.Until.Before(event.Until) || touchesRight {
			right := -1
			for i, c := range candidates {
				if c.interval.OpenEnd && c.interval.Until.Equal(result.Until) {
					right = i
					break
				}
			}
			if right >= 0 {
				candidates[right].interval.Until = event.Until
				candidates[right].interval.OpenEnd = event.OpenEnd
			} else {
				candidates = append(candidates, candidate{interval: ObservationInterval{Since: result.Until, Until: event.Until, OpenStart: true, OpenEnd: event.OpenEnd}, review: true})
			}
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int { return a.interval.Since.Compare(b.interval.Since) })
	merged := make([]candidate, 0, len(candidates))
	for _, next := range candidates {
		if !next.interval.Since.Before(next.interval.Until) {
			continue
		}
		if len(merged) > 0 && !merged[len(merged)-1].interval.Until.Before(next.interval.Since) {
			last := &merged[len(merged)-1]
			last.review = last.review || next.review
			if last.interval.Until.Before(next.interval.Until) {
				last.interval.Until = next.interval.Until
				last.interval.OpenEnd = next.interval.OpenEnd
			}
			continue
		}
		merged = append(merged, next)
	}
	if len(merged) > ObservationMaxIntervals {
		return out, fmt.Errorf("interval reconciliation exceeds its result bound")
	}
	predecessors := make([][]int, len(merged))
	successors := make([][]int, len(affected))
	for i, event := range merged {
		for j, old := range affected {
			if intervalOverlap(event.interval, old.Interval) {
				predecessors[i] = append(predecessors[i], j)
				successors[j] = append(successors[j], i)
			}
		}
	}
	for i, event := range merged {
		change := IntervalChange{Interval: event.interval, Review: event.review, Predecessors: []string{}}
		if len(predecessors[i]) == 1 && len(successors[predecessors[i][0]]) == 1 {
			old := affected[predecessors[i][0]]
			change.ID = old.ID
			change.Changed = old.Interval != event.interval
		} else {
			change.ID = newID(i)
			change.New = true
			change.Changed = true
			if refusal := identity.Create(&pb.EntityRef{Type: object, Id: change.ID}); refusal != nil {
				return out, fmt.Errorf("interval identity is not fresh: %v", refusal)
			}
			for _, index := range predecessors[i] {
				change.Predecessors = append(change.Predecessors, affected[index].ID)
			}
			slices.Sort(change.Predecessors)
			change.Review = change.Review || len(change.Predecessors) > 0
		}
		out.Events = append(out.Events, change)
	}
	for i, old := range affected {
		kept := false
		for _, event := range out.Events {
			kept = kept || event.ID == old.ID
		}
		if kept {
			continue
		}
		out.Retired = append(out.Retired, old.ID)
		if len(successors[i]) == 0 {
			continue
		}
		redirect := &pb.Redirect{From: &pb.EntityRef{Type: object, Id: old.ID}, Kind: pb.RedirectKind_REDIRECT_KIND_MERGE}
		for _, index := range successors[i] {
			redirect.To = append(redirect.To, &pb.EntityRef{Type: object, Id: out.Events[index].ID})
		}
		if len(redirect.To) > 1 {
			redirect.Kind = pb.RedirectKind_REDIRECT_KIND_SPLIT
		}
		if refusal := identity.AddRedirect(redirect); refusal != nil {
			return out, fmt.Errorf("interval redirect violates K1: %v", refusal)
		}
		out.Redirects = append(out.Redirects, redirect)
	}
	return out, nil
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

type RuleCriterion struct {
	Expr    Expr   `json:"expr"`
	Message string `json:"message"`
}

type RuleCriteria struct {
	Reads      RuleReadSpec    `json:"reads"`
	Conditions []RuleCriterion `json:"conditions"`
}

func EvaluateRule(expr Expr, reads RuleReadSpec, caller Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (EntityInfo, bool)) (result ExprResult, refusal *kernel.Error) {
	var observed *ExprContext
	if caller.TraceEnabled() {
		defer func() {
			event := DecisionTrace{Kind: "rule", Type: reads.Object, Expression: &expr, Reads: observed, Result: &result}
			if refusal != nil {
				event.Refusal = refusal.Message
			}
			caller.Trace(event)
		}()
	}
	if _, supported := caller.rt.(AuthorizationRuntime); supported && !caller.Replaying && reads.Object != "" {
		var subject struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(Raw(record), &subject)
		var check func(Expr) *kernel.Error
		check = func(value Expr) *kernel.Error {
			if value.Ref != "" && !strings.Contains(value.Ref, ".") && subject.ID != "" {
				fields := []string{value.Ref}
				if value.Ref == "id" {
					fields = nil
				}
				decision := caller.Authorize(AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: "read", Type: reads.Object, Record: subject.ID, Fields: fields}, at)
				if !decision.Allow || len(decision.Withheld) > 0 {
					return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule field is outside the caller's permissions")
				}
			}
			for _, arg := range value.Args {
				if err := check(arg); err != nil {
					return err
				}
			}
			if value.In != nil {
				return check(*value.In)
			}
			return nil
		}
		if err := check(expr); err != nil {
			return ExprResult{}, err
		}
	}
	schema, err := RuleSchema(expr, reads, lookup, caller.Rules())
	if err != nil {
		return ExprResult{}, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Rule: {reason} / 规则：{reason}", err.Error(), err.Error())
	}
	context, err := caller.RuleContext(expr, reads, lookup, record, inputs, at)
	if err != nil {
		return ExprResult{}, Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule input: {reason} / 规则输入：{reason}", err.Error(), err.Error())
	}
	if caller.TraceEnabled() {
		filtered := ruleTraceContext(expr, context)
		observed = &filtered
	}
	program, err := caller.Rules().CompileExpr(expr, schema)
	if err != nil {
		return ExprResult{}, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	value, err := program.Eval(context)
	value.Sources = slices.Clone(context.Sources)
	if err != nil {
		code := pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
		if issue, ok := err.(*ExprIssue); ok && issue.Code == "withheld" {
			code = pb.ErrorCode_ERROR_CODE_POLICY_DENIED
		}
		return value, Refuse(code, "Rule: {reason} / 规则：{reason}", err.Error(), err.Error())
	}
	return value, nil
}

func (plan RuleCriteria) Check(c Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (EntityInfo, bool)) *kernel.Error {
	matched, reason, err := plan.Evaluate(c, record, inputs, at, lookup)
	if err != nil {
		return err
	}
	if !matched {
		return c.RefuseBusinessCondition("CRITERIA_NOT_SATISFIED", reason, record, inputs, at)
	}
	return nil
}

func (plan RuleCriteria) Evaluate(c Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (EntityInfo, bool)) (bool, string, *kernel.Error) {
	for _, condition := range plan.Conditions {
		value, err := EvaluateRule(condition.Expr, plan.Reads, c, record, inputs, at, lookup)
		if err != nil {
			return false, "", err
		}
		if string(value.Value.Data) != "true" {
			reason := condition.Message
			for _, issue := range value.Reasons {
				reason += " · " + issue.At + ": " + issue.Message
			}
			return false, strings.TrimSpace(reason), nil
		}
	}
	return true, "", nil
}

type RuleReadSpec struct {
	Object      string                    `json:"object,omitempty"`
	Inputs      map[string]ExprType       `json:"inputs,omitempty"`
	Collections map[string]RuleCollection `json:"collections,omitempty"`
}
type RuleCollection struct {
	Set     string `json:"set,omitempty"`
	App     string `json:"app"`
	Query   string `json:"query"`
	Version int    `json:"version"`
	Object  string `json:"object"`
	Of      string `json:"of,omitempty"`
	Policy  string `json:"policy"`
}

func RuleCollectionSubjectField(object, of string, lookup func(string) (EntityInfo, bool)) (FieldInfo, bool, error) {
	name, selected := strings.CutPrefix(of, "$subject.")
	if !selected {
		return FieldInfo{}, false, nil
	}
	info, ok := lookup(object)
	field, found := info.Field(name)
	if name == "id" && ok {
		field, found = FieldInfo{Name: "id", Type: "text", ReadOnly: true}, true
	}
	if !ok || !found || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, field.Type) {
		return FieldInfo{}, true, fmt.Errorf("collection subject key must be a declared text or record-reference field")
	}
	return field, true, nil
}

func RuleFieldKind(kind string) string {
	switch kind {
	case "longtext", "text", "string":
		return "text"
	case "number":
		return "number"
	default:
		return kind
	}
}

func RuleSchema(expr Expr, spec RuleReadSpec, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) (ExprSchema, error) {
	schema := ExprSchema{Refs: map[string]ExprType{"caller.id": {Kind: "text"}, "caller.tenant": {Kind: "text"}, "observation.ready": {Kind: "boolean"}, "observation.source": {Kind: "text"}, "observation.object": {Kind: "text"}, "observation.subject": {Kind: "text"}}, Sets: map[string]ExprSetType{}}
	for key, typ := range spec.Inputs {
		name := "input." + key
		if strings.HasPrefix(key, "step.") || strings.HasPrefix(key, "row.") {
			name = key
		}
		schema.Refs[name] = typ
		if spec.Object == "" {
			schema.Refs[key] = typ
		}
	}
	var record EntityInfo
	if spec.Object != "" {
		var ok bool
		record, ok = lookup(spec.Object)
		if !ok {
			return schema, fmt.Errorf("rule object %s is undeclared", spec.Object)
		}
		for _, f := range record.Fields {
			schema.Refs[f.Name] = ExprType{Kind: RuleFieldKind(f.Type), Object: f.Ref}
		}
		schema.Refs["id"] = ExprType{Kind: "text"}
	}
	for name, set := range spec.Collections {
		if _, _, err := RuleCollectionSubjectField(spec.Object, set.Of, lookup); err != nil {
			return schema, err
		}
		if set.App == "" || set.Query == "" && set.Set == "" || set.Set != "" && set.Query != "" || set.Version < 0 || set.Policy != "visible" && set.Policy != "complete" {
			return schema, fmt.Errorf("rule collection %s needs a declared query/version and scope policy", name)
		}
		if set.Set != "" && (set.App != record.App || set.Version != 0 || set.Of != "" && set.Of != "$subject" || !slices.ContainsFunc(record.Sets, func(declaration RecordSet) bool {
			return declaration.Name == set.Set && declaration.Object == set.Object
		})) {
			return schema, fmt.Errorf("rule collection %s must bind its object's declared set", name)
		}
		info, ok := lookup(set.Object)
		if !ok {
			return schema, fmt.Errorf("rule collection object %s is undeclared", set.Object)
		}
		fields := map[string]ExprType{}
		for _, f := range info.Fields {
			fields[f.Name] = ExprType{Kind: RuleFieldKind(f.Type), Object: f.Ref}
		}
		schema.Sets[name] = ExprSetType{Element: ExprType{Kind: "reference", Object: set.Object, Fields: fields}, Policy: set.Policy}
	}
	var visit func(Expr, string) error
	merge := func(dst, src ExprType) ExprType { return mergeRuleTypes(dst, src) }
	visit = func(e Expr, itemSet string) error {
		child := itemSet
		if e.Agg != nil {
			if _, ok := spec.Collections[e.Agg.Source]; !ok {
				return fmt.Errorf("rule collection %s is undeclared", e.Agg.Source)
			}
			child = e.Agg.Source
		}
		expand := func(ref string, path []string) error {
			typ := spec.Object
			fieldName := ref
			if strings.HasPrefix(ref, "item.") {
				typ = spec.Collections[child].Object
				fieldName = strings.TrimPrefix(ref, "item.")
			}
			if strings.HasPrefix(ref, "input.") || strings.HasPrefix(ref, "step.") || strings.HasPrefix(ref, "row.") {
				input := spec.Inputs[strings.TrimPrefix(ref, "input.")]
				if input.Kind != "reference" || input.Object == "" {
					return fmt.Errorf("rule path input is not a declared reference")
				}
				final, err := RecordPathField(input.Object, path, lookup)
				if err != nil {
					return err
				}
				leaf := ExprType{Kind: RuleFieldKind(final.Type), Object: final.Ref}
				for i := len(path) - 1; i >= 0; i-- {
					leaf = ExprType{Kind: "reference", Fields: map[string]ExprType{path[i]: leaf}}
				}
				schema.Refs[ref] = merge(schema.Refs[ref], leaf)
				return nil
			}
			info, ok := lookup(typ)
			field, found := info.Field(fieldName)
			if !ok || !found || field.Type != "reference" || field.Ref == "" {
				return fmt.Errorf("rule path base %s is not a declared reference", ref)
			}
			final, err := RecordPathField(field.Ref, path, lookup)
			if err != nil {
				return err
			}
			leaf := ExprType{Kind: RuleFieldKind(final.Type)}
			for i := len(path) - 1; i >= 0; i-- {
				leaf = ExprType{Kind: "reference", Fields: map[string]ExprType{path[i]: leaf}}
			}
			if strings.HasPrefix(ref, "item.") {
				d := schema.Sets[child]
				d.Element.Fields[fieldName] = merge(d.Element.Fields[fieldName], leaf)
				schema.Sets[child] = d
			} else {
				schema.Refs[ref] = merge(schema.Refs[ref], leaf)
			}
			return nil
		}
		if e.Op == "path" && len(e.Args) == 1 {
			if err := expand(e.Args[0].Ref, e.Path); err != nil {
				return err
			}
		}
		if e.Agg != nil && len(e.Agg.Field) > 1 {
			if err := expand("item."+e.Agg.Field[0], e.Agg.Field[1:]); err != nil {
				return err
			}
		}
		for _, arg := range e.Args {
			if err := visit(arg, child); err != nil {
				return err
			}
		}
		if e.In != nil {
			return visit(*e.In, child)
		}
		return nil
	}
	if err := visit(expr, ""); err != nil {
		return schema, err
	}
	if _, err := ruleEngineOf(engines).CompileExpr(expr, schema); err != nil {
		return schema, err
	}
	return schema, nil
}

func mergeRuleTypes(dst, src ExprType) ExprType {
	if dst.Kind == "" {
		return src
	}
	if dst.Fields == nil {
		dst.Fields = map[string]ExprType{}
	}
	for name, t := range src.Fields {
		dst.Fields[name] = mergeRuleTypes(dst.Fields[name], t)
	}
	return dst
}

func (c Caller) RuleContext(expr Expr, spec RuleReadSpec, lookup func(string) (EntityInfo, bool), record any, inputs map[string]any, now time.Time) (ExprContext, error) {
	ctx := ExprContext{Refs: map[string]ExprValue{"caller.id": ExprPresent("text", c.ID), "caller.tenant": ExprPresent("text", c.Tenant)}, Sets: map[string]ExprCollection{}, Me: c.ID, Now: now.UTC(), Today: c.Today(now)}
	for name, fact := range c.observationRuleFacts() {
		ctx.Refs[name] = fact
	}
	for name, typ := range spec.Inputs {
		v, ok := inputs[name]
		if ok {
			if fact, captured := v.(ExprValue); captured && strings.HasPrefix(name, "row.") {
				ctx.Refs[ruleInputName(name)] = fact
			} else {
				ctx.Refs[ruleInputName(name)] = ExprPresent(typ.Kind, v)
			}
			if spec.Object == "" {
				ctx.Refs[name] = ctx.Refs[ruleInputName(name)]
			}
		} else {
			ctx.Refs[ruleInputName(name)] = ExprValue{Kind: typ.Kind, Status: "missing"}
			if spec.Object == "" {
				ctx.Refs[name] = ctx.Refs[ruleInputName(name)]
			}
		}
	}
	var values map[string]json.RawMessage
	if record != nil {
		raw, err := json.Marshal(record)
		if err != nil {
			return ctx, err
		}
		if json.Unmarshal(raw, &values) != nil {
			return ctx, fmt.Errorf("rule record must be an object")
		}
	}
	info, known := lookup(spec.Object)
	if spec.Object != "" && !known {
		return ctx, fmt.Errorf("rule record object is undeclared")
	}
	rv := reflect.ValueOf(record)
	if rv.IsValid() && rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	if rv.IsValid() && rv.Kind() == reflect.Struct {
		for i := 0; i < rv.NumField(); i++ {
			sf := rv.Type().Field(i)
			name := strings.Split(sf.Tag.Get("json"), ",")[0]
			if name == "" {
				name = strings.ToLower(sf.Name[:1]) + sf.Name[1:]
			}
			if _, known := info.Field(name); known {
				fv := rv.Field(i)
				if fv.CanInterface() {
					raw, err := json.Marshal(fv.Interface())
					if err != nil {
						return ctx, err
					}
					values[name] = raw
				}
			}
		}
	}
	for _, field := range info.Fields {
		value := ExprValue{Kind: RuleFieldKind(field.Type), Status: "missing"}
		if !field.ReadsAny(c.RolesIn(info.App)) {
			value.Status = "withheld"
		} else if raw, ok := values[field.Name]; ok {
			value.Status = "value"
			value.Data = raw
			if string(raw) == "null" {
				value.Status = "null"
			}
		}
		ctx.Refs[field.Name] = value
	}
	if spec.Object != "" {
		ctx.Refs["id"] = ExprValue{Kind: "text", Status: "missing"}
		if raw, present := values["id"]; present {
			ctx.Refs["id"] = ExprValue{Kind: "text", Status: "value", Data: raw}
		}
	}
	var visit func(Expr) error
	visit = func(e Expr) error {
		if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") && !strings.HasPrefix(e.Ref, "input.") && e.Ref != "caller.id" && e.Ref != "caller.tenant" && !strings.HasPrefix(e.Ref, "step.") && !strings.HasPrefix(e.Ref, "row.") {
			var id string
			_ = json.Unmarshal(values["id"], &id)
			if id != "" {
				ctx.Sources = append(ctx.Sources, spec.Object+"/"+id)
				if e.Ref != "id" {
					ctx.Sources = append(ctx.Sources, spec.Object+"/"+id+"#"+e.Ref)
				}
			}
		}
		if e.Op == "path" && len(e.Args) == 1 && !strings.HasPrefix(e.Args[0].Ref, "item.") {
			name := e.Args[0].Ref
			field, ok := info.Field(name)
			if strings.HasPrefix(name, "input.") || strings.HasPrefix(name, "step.") || strings.HasPrefix(name, "row.") {
				t := spec.Inputs[strings.TrimPrefix(name, "input.")]
				field = FieldInfo{Type: t.Kind, Ref: t.Object}
				ok = t.Kind == "reference" && t.Object != ""
			}
			if !ok {
				return fmt.Errorf("rule reference %s is undeclared", name)
			}
			value := ExprValue{Kind: e.Kind, Status: "missing"}
			base := ctx.Refs[name]
			if base.Status == "withheld" {
				value.Status = "withheld"
			} else {
				var id string
				if json.Unmarshal(base.Data, &id) == nil && id != "" {
					raw, sources, err := c.ReadRecordPath(field.Ref, id, e.Path, now)
					if err != nil {
						value.Status = rulePathStatus(err)
						if value.Status == "missing" {
							ctx.Sources = append(ctx.Sources, sources...)
						}
					} else {
						value.Status = "value"
						value.Data = raw
						ctx.Sources = append(ctx.Sources, sources...)
					}
				}
			}
			ctx.Refs[name+"."+strings.Join(e.Path, ".")] = value
		}
		for _, arg := range e.Args {
			if err := visit(arg); err != nil {
				return err
			}
		}
		if e.In != nil {
			return visit(*e.In)
		}
		return nil
	}
	if err := visit(expr); err != nil {
		return ctx, err
	}
	used := map[string][]Expr{}
	needed := map[string]map[string]bool{}
	need := func(source, field string) {
		if needed[source] == nil {
			needed[source] = map[string]bool{}
		}
		needed[source][field] = true
	}
	var collect func(Expr, string)
	collect = func(e Expr, scope string) {
		if e.Agg != nil {
			scope = e.Agg.Source
			if _, ok := used[scope]; !ok {
				used[scope] = nil
			}
			if len(e.Agg.Field) > 1 {
				used[scope] = append(used[scope], Expr{Op: "path", Kind: e.Kind, Args: []Expr{ExprReference("reference", "item."+e.Agg.Field[0])}, Path: e.Agg.Field[1:]})
			}
			if len(e.Agg.Field) > 0 {
				need(scope, e.Agg.Field[0])
			}
		}
		if strings.HasPrefix(e.Ref, "item.") {
			need(scope, strings.TrimPrefix(e.Ref, "item."))
		}
		if e.Op == "path" && len(e.Args) == 1 && strings.HasPrefix(e.Args[0].Ref, "item.") {
			used[scope] = append(used[scope], e)
		}
		for _, a := range e.Args {
			collect(a, scope)
		}
		if e.In != nil {
			collect(*e.In, scope)
		}
	}
	collect(expr, "")
	for name, paths := range used {
		set := spec.Collections[name]
		of := ""
		if set.Of != "" {
			if set.Of == "$subject" {
				var id string
				_ = json.Unmarshal(values["id"], &id)
				of = id
			} else if field, selected, err := RuleCollectionSubjectField(spec.Object, set.Of, lookup); selected {
				if err != nil {
					return ctx, err
				}
				value := ctx.Refs[field.Name]
				if value.Status != "value" || json.Unmarshal(value.Data, &of) != nil || of == "" {
					return ctx, fmt.Errorf("collection %s subject key is unavailable", name)
				}
				var id string
				_ = json.Unmarshal(values["id"], &id)
				if id != "" {
					ctx.Sources = append(ctx.Sources, spec.Object+"/"+id)
					if field.Name != "id" {
						ctx.Sources = append(ctx.Sources, spec.Object+"/"+id+"#"+field.Name)
					}
				}
			} else if value, ok := inputs[set.Of].(string); ok {
				of = value
			} else {
				return ctx, fmt.Errorf("collection %s input is unavailable", name)
			}
		}
		queryInputs := map[string]any{}
		if of != "" {
			queryInputs["for"] = of
		}
		var raw json.RawMessage
		var readErr *kernel.Error
		if set.Set != "" {
			var subjectID string
			_ = json.Unmarshal(values["id"], &subjectID)
			page, err := c.ruleRecordSet(spec.Object, subjectID, set.Set, record, now)
			readErr = err
			if err == nil {
				raw = Raw(map[string]any{"records": page.Records, "total": len(page.Records), "sources": page.Sources, "partial": page.Partial})
			}
		} else {
			raw, readErr = c.readRuleQuery(set, Raw(queryInputs), now)
		}
		if readErr != nil {
			if readErr.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED && readErr.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
				return ctx, fmt.Errorf("collection %s: %s", name, readErr.Message)
			}
			ctx.Sets[name] = ExprCollection{Status: "withheld"}
			continue
		}
		var page struct {
			Records []map[string]json.RawMessage `json:"records"`
			Total   int                          `json:"total"`
			Sources []string                     `json:"sources"`
			Partial bool                         `json:"partial"`
		}
		if json.Unmarshal(raw, &page) != nil {
			return ctx, fmt.Errorf("collection %s returned an invalid record page", name)
		}
		ctx.Sources = append(ctx.Sources, page.Sources...)
		collection := ExprCollection{Complete: page.Total == len(page.Records), Withheld: page.Partial, Items: []ExprItem{}}
		object, ok := lookup(set.Object)
		if !ok {
			return ctx, fmt.Errorf("collection object is unavailable")
		}
		for _, row := range page.Records {
			var id string
			_ = json.Unmarshal(row["id"], &id)
			item := ExprItem{ID: id, Fields: map[string]ExprValue{}}
			for _, field := range object.Fields {
				if !needed[name][field.Name] {
					continue
				}
				v := ExprValue{Kind: RuleFieldKind(field.Type), Status: "missing"}
				if !field.ReadsAny(c.RolesIn(object.App)) {
					v.Status = "withheld"
					collection.Withheld = true
				} else if raw, ok := row[field.Name]; ok {
					v.Status = "value"
					v.Data = raw
				}
				item.Fields[field.Name] = v
			}
			for _, path := range paths {
				fieldName := strings.TrimPrefix(path.Args[0].Ref, "item.")
				field, _ := object.Field(fieldName)
				v := ExprValue{Kind: path.Kind, Status: "missing"}
				base := item.Fields[fieldName]
				if base.Status == "withheld" {
					v.Status = "withheld"
					collection.Withheld = true
				} else {
					var ref string
					if json.Unmarshal(base.Data, &ref) == nil && ref != "" {
						raw, sources, err := c.ReadRecordPath(field.Ref, ref, path.Path, now)
						if err != nil {
							v.Status = rulePathStatus(err)
							collection.Withheld = collection.Withheld || v.Status == "withheld"
							if v.Status == "missing" {
								ctx.Sources = append(ctx.Sources, sources...)
							}
						} else {
							v.Status = "value"
							v.Data = raw
							ctx.Sources = append(ctx.Sources, sources...)
						}
					}
				}
				item.Fields[fieldName+"."+strings.Join(path.Path, ".")] = v
			}
			collection.Items = append(collection.Items, item)
		}
		ctx.Sets[name] = collection
		raw, budgetErr := json.Marshal(ctx)
		if budgetErr != nil || len(raw) > ExprMaxContextBytes {
			return ctx, fmt.Errorf("captured rule context is %d bytes; limit is %d", len(raw), ExprMaxContextBytes)
		}
	}
	return ctx, nil
}

func rulePathStatus(err *kernel.Error) string {
	if err.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		return "missing"
	}
	return "withheld"
}

func ExprGoValue(v ExprValue) (any, error) {
	if v.Status != "value" {
		return nil, fmt.Errorf("rule value is %s", v.Status)
	}
	if v.Kind == "integer" || v.Kind == "decimal" || v.Kind == "number" {
		var text string
		if json.Unmarshal(v.Data, &text) == nil {
			return json.Number(text), nil
		}
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(v.Data)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if v.Kind == "money" {
		if m, ok := value.(map[string]any); ok {
			if amount, ok := m["amount"].(string); ok {
				m["amount"] = json.Number(amount)
			}
		}
	}
	return value, nil
}

func ruleInputName(name string) string {
	if strings.HasPrefix(name, "step.") || strings.HasPrefix(name, "row.") {
		return name
	}
	return "input." + name
}

func (c Caller) readRuleQuery(set RuleCollection, inputs json.RawMessage, now time.Time) (json.RawMessage, *kernel.Error) {
	reader, ok := c.rt.(RuleReadRuntime)
	if !ok {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Scoped rule collections require the host")
	}
	return reader.ReadRuleQuery(c, set, inputs, now)
}

type RuleTablePlan struct {
	Reads   RuleReadSpec    `json:"reads"`
	Rows    []RuleTableRow  `json:"rows"`
	Default json.RawMessage `json:"default,omitempty"`
}
type RuleTableRow struct {
	Condition Expr            `json:"condition"`
	Result    json.RawMessage `json:"result"`
}

func (plan RuleTablePlan) Decide(facts ExprContext, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) (json.RawMessage, error) {
	schemas := []ExprSchema{}
	for _, row := range plan.Rows {
		schema, err := RuleSchema(row.Condition, plan.Reads, lookup, engines...)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return plan.DecideSchemas(facts, schemas, engines...)
}

func (plan RuleTablePlan) DecideSchemas(facts ExprContext, schemas []ExprSchema, engines ...RuleEngine) (json.RawMessage, error) {
	var engine RuleEngine = NativeRules{}
	if len(engines) > 0 {
		engine = engines[0]
	}
	if len(schemas) != len(plan.Rows) {
		return nil, fmt.Errorf("decision table schemas are not bound")
	}
	for i, row := range plan.Rows {
		p, err := engine.CompileExpr(row.Condition, schemas[i])
		if err != nil {
			return nil, err
		}
		r, err := p.Eval(facts)
		if err != nil {
			return nil, err
		}
		if string(r.Value.Data) == "true" {
			return row.Result, nil
		}
	}
	if len(plan.Default) > 0 {
		return plan.Default, nil
	}
	return nil, fmt.Errorf("no decision table row matches the captured input")
}

type Expr = rules.Expr
type ExprLiteral = rules.Literal
type ExprAggregate = rules.Aggregate
type ExprWindowReference = rules.WindowReference
type ExprWindowType = rules.WindowType
type ExprWindowSummary = rules.WindowSummary
type ExprType = rules.Type
type ExprSetType = rules.SetType
type ExprSchema = rules.Schema
type ExprValue = rules.Value
type ExprContext = rules.Context
type ExprItem = rules.Item
type ExprCollection = rules.Collection
type ExprResult = rules.Result
type ExprIssue = rules.Issue
type ExprProgram interface {
	Eval(ExprContext) (ExprResult, error)
	EvalFrozenFormula(ExprContext) (ExprResult, error)
}
type ExprNumericAggregate interface {
	Add(ExprValue) error
	Result() (ExprValue, error)
}

type RuleEngine interface {
	CompileExpr(Expr, ExprSchema) (ExprProgram, error)
	CompileFrozenFormula(Expr, ExprSchema) (ExprProgram, error)
	CompareExprValues(ExprValue, ExprValue) (int, error)
	NewExprNumericAggregate(string, string, string) (ExprNumericAggregate, error)
}

type NativeRules struct{}

type RuleRuntime interface{ Rules() RuleEngine }

func (c Caller) Rules() RuleEngine {
	if runtime, ok := c.rt.(RuleRuntime); ok {
		return runtime.Rules()
	}
	return NativeRules{}
}

func (NativeRules) CompileExpr(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.Compile(expr, schema)
}
func (NativeRules) CompileFrozenFormula(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.CompileFrozenFormula(expr, schema)
}
func (NativeRules) CompareExprValues(a, b ExprValue) (int, error) {
	return rules.CompareValues(a, b)
}
func (NativeRules) NewExprNumericAggregate(op, kind, currency string) (ExprNumericAggregate, error) {
	return rules.NewNumericAggregate(op, kind, currency)
}

const ExprVersion = rules.Version
const ExprMaxDepth = rules.MaxDepth
const ExprMaxSet = rules.MaxSet
const ExprMaxContextBytes = rules.MaxContextBytes

func CompileExpr(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.Compile(expr, schema)
}

func LowerFormula(text string, fields map[string]string) (Expr, error) {
	kinds := make(map[string]string, len(fields))
	for name, kind := range fields {
		kinds[name] = RuleFieldKind(kind)
	}
	return rules.LowerFormula(text, kinds)
}
func FormulaFields(text string) (map[string]bool, error) { return rules.FormulaFields(text) }

func ExprLiteralValue(kind string, value any) Expr { return rules.LiteralOf(kind, value) }
func ExprReference(kind, name string) Expr         { return rules.RefOf(kind, name) }
func ExprPresent(kind string, value any) ExprValue { return rules.Present(kind, value) }

func CheckObservationDecimal(value string) error { return rules.CheckWindowDecimal(value) }

func LowerPageExpr(raw json.RawMessage, kind string, schema ExprSchema) (Expr, error) {
	return rules.LowerPage(raw, kind, schema)
}

func ruleEngineOf(engines []RuleEngine) RuleEngine {
	if len(engines) > 0 {
		return engines[0]
	}
	return NativeRules{}
}
