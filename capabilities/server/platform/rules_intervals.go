package platform

import (
	"bytes"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"slices"
	"time"
)

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
