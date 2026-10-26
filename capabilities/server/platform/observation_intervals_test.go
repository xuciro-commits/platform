package platform

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func intervalFixture(kind string, values ...string) (ObservationIntervals, ObservationIntervalInput, time.Time) {
	since := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	policy := ObservationIntervals{ValueKind: kind, When: Expr{Op: "=", Kind: "boolean", Args: []Expr{ExprReference(kind, "sample.value"), ExprLiteralValue(kind, "down")}}, MaxGapSeconds: 10, LatePolicy: "correct", LatenessSeconds: 30, MaxIntervals: 32}
	if kind == "decimal" {
		policy.When = Expr{Op: ">", Kind: "boolean", Args: []Expr{ExprReference(kind, "sample.value"), ExprLiteralValue(kind, "35.00000000000000000001")}}
	}
	if kind == "boolean" {
		policy.When = ExprReference("boolean", "sample.value")
	}
	input := ObservationIntervalInput{Since: since, Until: since.Add(time.Duration(len(values)*10) * time.Second), Revision: "source-revision-1", Coverage: &ObservationCoverage{AvailableSince: since, CompleteUntil: since.Add(time.Duration(len(values)*10) * time.Second), Good: int64(len(values))}}
	for i, value := range values {
		at := since.Add(time.Duration(i*10) * time.Second)
		input.Points = append(input.Points, ObservationStatePoint{At: at, ReceivedAt: at, Quality: "good", Value: value})
	}
	return policy, input, input.Until.Add(time.Minute)
}

func TestObservationStateIntervalsUseOneEvaluatorForManufacturingAndHeating(t *testing.T) {
	for _, scenario := range []struct {
		kind   string
		values []string
	}{{"choice", []string{"run", "down", "down", "run"}}, {"decimal", []string{"35.00000000000000000001", "35.00000000000000000002", "36", "34"}}, {"boolean", []string{"false", "true", "true", "false"}}} {
		t.Run(scenario.kind, func(t *testing.T) {
			policy, input, now := intervalFixture(scenario.kind, scenario.values...)
			out, err := AnalyzeObservationIntervals(policy, input, now)
			want := []ObservationInterval{{Since: input.Since.Add(10 * time.Second), Until: input.Since.Add(30 * time.Second)}}
			if err != nil || !out.Ready || !reflect.DeepEqual(out.Intervals, want) || len(out.Unknown) != 0 {
				t.Fatalf("state interval differs: %+v %v", out, err)
			}
			encoded, _ := json.Marshal(out)
			if strings.Contains(string(encoded), "receivedAt") || strings.Contains(string(encoded), "points") || strings.Contains(string(encoded), "sample.value") {
				t.Fatalf("raw samples escaped into compact result: %s", encoded)
			}
		})
	}
}

func TestObservationStateIntervalsNeverCloseEventsAcrossUnknownTime(t *testing.T) {
	policy, input, now := intervalFixture("choice", "down", "down", "down", "run")
	input.Points[1].Quality = "uncertain"
	input.Coverage.Good--
	input.Coverage.Uncertain++
	out, err := AnalyzeObservationIntervals(policy, input, now)
	if err != nil || out.Ready || len(out.Unknown) != 1 || out.Unknown[0].Reason != "uncertain" || len(out.Intervals) != 2 || !out.Intervals[0].OpenEnd || !out.Intervals[1].OpenStart || out.Intervals[1].OpenEnd {
		t.Fatalf("unknown time became a normal boundary: %+v %v", out, err)
	}
	input.Points = append(input.Points[:1], input.Points[2:]...)
	input.Coverage.Good = 3
	input.Coverage.Uncertain = 0
	out, err = AnalyzeObservationIntervals(policy, input, now)
	if err != nil || out.Ready || out.Unknown[0].Reason != "gap" || !out.Intervals[0].OpenEnd {
		t.Fatalf("a missing sample became continuous: %+v %v", out, err)
	}
}

func TestObservationStateIntervalsRetainOpenWindowBoundaries(t *testing.T) {
	policy, input, now := intervalFixture("choice", "down", "down")
	out, err := AnalyzeObservationIntervals(policy, input, now)
	if err != nil || !out.Ready || len(out.Intervals) != 1 || !out.Intervals[0].OpenStart || !out.Intervals[0].OpenEnd || !out.Intervals[0].Until.Equal(input.Until) {
		t.Fatalf("a window boundary became an event start/end: %+v %v", out, err)
	}
	// A shorter manual read still uses the same declared expiry, clipped to its window.
	input.Until = input.Since.Add(5 * time.Second)
	input.Points = input.Points[:1]
	input.Coverage.Good = 1
	if out, err = AnalyzeObservationIntervals(policy, input, now); err != nil || !out.Ready || !out.Intervals[0].Until.Equal(input.Until) {
		t.Fatalf("bounded shorter window refused: %+v %v", out, err)
	}
}

func TestObservationStateIntervalsRequireCoverageAndEveryReturnedPoint(t *testing.T) {
	for _, damage := range []func(*ObservationIntervalInput){func(in *ObservationIntervalInput) { in.Coverage = nil }, func(in *ObservationIntervalInput) { in.Coverage.Truncated = true }, func(in *ObservationIntervalInput) { in.Coverage.CompleteUntil = in.Until.Add(-time.Second) }, func(in *ObservationIntervalInput) { in.Coverage.AvailableSince = in.Since.Add(time.Second) }, func(in *ObservationIntervalInput) { in.Coverage.Good++ }, func(in *ObservationIntervalInput) { in.Points = nil; in.Coverage.Good = 0 }} {
		policy, input, now := intervalFixture("choice", "down", "run")
		damage(&input)
		out, err := AnalyzeObservationIntervals(policy, input, now)
		if err != nil || out.Ready || len(out.Unknown) != 1 || len(out.Intervals) != 0 {
			t.Fatalf("incomplete input became a business conclusion: %+v %v", out, err)
		}
	}
}

func TestObservationStateIntervalsRespectDeclaredLatePolicyAndResultBounds(t *testing.T) {
	policy, input, now := intervalFixture("choice", "run", "down", "run")
	input.Points[1].ReceivedAt = input.Until.Add(time.Second)
	out, err := AnalyzeObservationIntervals(policy, input, now)
	if err != nil || !out.Ready || len(out.Intervals) != 1 {
		t.Fatalf("legal late correction refused: %+v %v", out, err)
	}
	policy.LatePolicy = "reject"
	policy.LatenessSeconds = 0
	out, err = AnalyzeObservationIntervals(policy, input, now)
	if err != nil || out.Ready || len(out.Intervals) != 0 || out.Unknown[0].Reason != "late" {
		t.Fatalf("late rejection became a normal result: %+v %v", out, err)
	}
	policy, input, now = intervalFixture("choice", "run", "down", "run", "down")
	policy.MaxIntervals = 1
	if _, err = AnalyzeObservationIntervals(policy, input, now); err == nil {
		t.Fatal("interval results were silently truncated")
	}
}

func TestObservationStateIntervalsRejectMalformedAndUnboundedDeclarations(t *testing.T) {
	decimal, input, now := intervalFixture("decimal", "1/3", "40")
	if _, err := AnalyzeObservationIntervals(decimal, input, now); err == nil {
		t.Fatal("nondecimal external value accepted")
	}
	for _, damage := range []func(*ObservationIntervals, *ObservationIntervalInput){func(p *ObservationIntervals, _ *ObservationIntervalInput) { p.MaxGapSeconds = 0 }, func(p *ObservationIntervals, _ *ObservationIntervalInput) { p.MaxIntervals = 257 }, func(p *ObservationIntervals, _ *ObservationIntervalInput) {
		p.When = ExprReference("boolean", "undeclared")
	}, func(_ *ObservationIntervals, in *ObservationIntervalInput) { in.Points[1].At = in.Points[0].At }, func(_ *ObservationIntervals, in *ObservationIntervalInput) { in.Points[0].ReceivedAt = time.Time{} }, func(_ *ObservationIntervals, in *ObservationIntervalInput) { in.Points[0].Quality = "invented" }} {
		policy, input, now := intervalFixture("choice", "down", "run")
		damage(&policy, &input)
		if _, err := AnalyzeObservationIntervals(policy, input, now); err == nil {
			t.Fatal("invalid state evidence accepted")
		}
	}
	var policy ObservationIntervals
	if json.Unmarshal([]byte(`{"valueKind":"choice","when":{},"maxGapSeconds":1,"latePolicy":"correct","latenessSeconds":1,"maxIntervals":1,"industryCode":"execute"}`), &policy) == nil {
		t.Fatal("unknown declaration fields accepted")
	}
}
