package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"platformserver/platform"
	"platformserver/studio/build"
)

func TestObservationStateIntervalReadUsesPublishedSourceAndKeepsPointsOutsideTenantState(t *testing.T) {
	tn, _, ref, at := observationTenant(t)
	policy := platform.ObservationIntervals{ValueKind: "choice", When: platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("choice", "sample.value"), platform.ExprLiteralValue("choice", "down")}}, MaxGapSeconds: 10, LatePolicy: "correct", LatenessSeconds: 30, MaxIntervals: 16}
	decide(t, tn, "builder", build.ID, "build.observation.edit", build.ObservationType, "OBSERVATION", map[string]any{"signal": "state", "unit": "state", "decision": nil, "intervals": policy}, at)
	decide(t, tn, "builder", build.ID, build.SchemaObservation, build.ObservationType, "OBSERVATION", map[string]any{}, at)
	calls := 0
	tn.Outbound = func(request *http.Request, _ bool) (*http.Response, error) {
		calls++
		var q struct {
			Device, Signal, Mode string
			Since, Until         time.Time
			Limit                int
		}
		if json.NewDecoder(request.Body).Decode(&q) != nil || q.Device != "boiler-1" || q.Signal != "state" || q.Mode != "intervals" || q.Limit != 20 {
			t.Fatal("state read lost its original bounded source", q)
		}
		input := platform.ObservationIntervalInput{Since: q.Since, Until: q.Until, Revision: "collector-17", Coverage: &platform.ObservationCoverage{AvailableSince: q.Since, CompleteUntil: q.Until, Good: 4}}
		for i, value := range []string{"run", "down", "down", "run"} {
			sampleAt := q.Since.Add(time.Duration(i*10) * time.Second)
			input.Points = append(input.Points, platform.ObservationStatePoint{At: sampleAt, ReceivedAt: sampleAt, Quality: "good", Value: value})
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(platform.Raw(map[string]any{"intervals": input}))), Header: make(http.Header)}, nil
	}
	before := snapshot(tn)
	accepted := 0
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { accepted++; return e.Body, nil }
	q := ObservationRead{Record: "D1", Since: at.Add(-40 * time.Second), Until: at, Mode: "intervals", Limit: 20}
	result, problem := tn.ReadObservation(context.Background(), memberOf(t, tn, "operator"), ref, q, at)
	if problem != nil || result.Intervals == nil || !result.Intervals.Ready || len(result.Intervals.Intervals) != 1 || result.Intervals.Revision != "collector-17" || result.Version == "" {
		t.Fatalf("published source could not analyze its state window: %+v %v", result, problem)
	}
	raw := string(platform.Raw(result))
	if strings.Contains(raw, "receivedAt") || strings.Contains(raw, "\"points\"") {
		t.Fatalf("raw samples escaped: %s", raw)
	}
	if accepted != 0 || snapshot(tn) != before {
		t.Fatal("a read persisted samples or changed business state")
	}
	if _, problem = tn.ReadObservation(context.Background(), memberOf(t, tn, "outsider"), ref, q, at); problem == nil || calls != 1 {
		t.Fatal("an unauthorized reader contacted the state store")
	}
	// Editing a declaration cannot change the source's fixed execution version.
	policy.When = platform.ExprLiteralValue("boolean", false)
	decide(t, tn, "builder", build.ID, "build.observation.edit", build.ObservationType, "OBSERVATION", map[string]any{"intervals": policy}, at)
	result, problem = tn.ReadObservation(context.Background(), memberOf(t, tn, "operator"), ref, q, at)
	if problem != nil || len(result.Intervals.Intervals) != 1 {
		t.Fatalf("unpublished analysis replaced the original source: %+v %v", result, problem)
	}
}
