package platform

import (
	"bytes"
	"fmt"
	"slices"
	"time"
)

const ObservationIntervalVersion = "platform.interval.v1"
const ObservationMaxIntervals = 256

// ObservationIntervals evaluates the original rule language over a finite
// source window. A value expires at MaxGapSeconds; missing time is unknown.
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

// This is an external read, not a durable state format. Only its compact
// analysis may accompany an accepted business result.
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

// AnalyzeObservationIntervals retains no source points. It refuses malformed
// reads, and preserves uncertain segments rather than closing an event there.
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

// Check validates compact evidence eligible for business acceptance. Read-only
// partial analyses remain displayable but cannot authorize a complete result.
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
