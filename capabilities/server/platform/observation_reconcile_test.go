package platform

import (
	"fmt"
	"testing"
	"time"
)

func TestObservationIntervalIdentityContinuityCorrectionsAndWindowEdges(t *testing.T) {
	at := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	span := func(from, until int, openStart, openEnd bool) ObservationInterval {
		return ObservationInterval{Since: at.Add(time.Duration(from) * time.Second), Until: at.Add(time.Duration(until) * time.Second), OpenStart: openStart, OpenEnd: openEnd}
	}
	result := func(from, until int, intervals ...ObservationInterval) ObservationIntervalResult {
		if intervals == nil {
			intervals = []ObservationInterval{}
		}
		return ObservationIntervalResult{Version: ObservationIntervalVersion, Revision: "r1", Since: at.Add(time.Duration(from) * time.Second), Until: at.Add(time.Duration(until) * time.Second), Ready: true, Count: 20, Intervals: intervals, Unknown: []ObservationUnknown{}}
	}
	fresh := func(i int) string { return fmt.Sprintf("new-%d", i) }
	old := []IntervalIdentity{{ID: "human-reason", Interval: span(10, 40, false, false)}}
	continued, err := ReconcileObservationIntervals("build.event", old, result(0, 60, span(12, 42, false, false)), fresh)
	if err != nil || len(continued.Events) != 1 || continued.Events[0].ID != "human-reason" || !continued.Events[0].Changed || continued.Events[0].Review || len(continued.Retired) != 0 {
		t.Fatalf("one-to-one identity lost: %+v %v", continued, err)
	}
	split, err := ReconcileObservationIntervals("build.event", old, result(0, 60, span(10, 20, false, false), span(30, 40, false, false)), fresh)
	if err != nil || len(split.Events) != 2 || !split.Events[0].Review || !split.Events[1].Review || len(split.Retired) != 1 || len(split.Redirects) != 1 || len(split.Redirects[0].To) != 2 {
		t.Fatalf("split lost predecessors: %+v %v", split, err)
	}
	merged, err := ReconcileObservationIntervals("build.event", []IntervalIdentity{{ID: "left", Interval: span(10, 20, false, false)}, {ID: "right", Interval: span(30, 40, false, false)}}, result(0, 60, span(10, 40, false, false)), fresh)
	if err != nil || len(merged.Events) != 1 || len(merged.Events[0].Predecessors) != 2 || !merged.Events[0].Review || len(merged.Redirects) != 2 {
		t.Fatalf("merge lost decisions: %+v %v", merged, err)
	}
	gone, err := ReconcileObservationIntervals("build.event", old, result(0, 60), fresh)
	if err != nil || len(gone.Retired) != 1 || len(gone.Events) != 0 || len(gone.Redirects) != 0 {
		t.Fatalf("disappeared identity retained: %+v %v", gone, err)
	}
	// A rolling window continues only an explicitly open original boundary.
	rolling, err := ReconcileObservationIntervals("build.event", []IntervalIdentity{{ID: "ongoing", Interval: span(10, 30, false, true)}}, result(30, 60, span(30, 50, true, false)), fresh)
	if err != nil || len(rolling.Events) != 1 || rolling.Events[0].ID != "ongoing" || rolling.Events[0].Interval != span(10, 50, false, false) {
		t.Fatalf("rolling event restarted: %+v %v", rolling, err)
	}
	closed, err := ReconcileObservationIntervals("build.event", []IntervalIdentity{{ID: "ongoing", Interval: span(10, 30, false, true)}}, result(30, 60), fresh)
	if err != nil || len(closed.Events) != 1 || closed.Events[0].ID != "ongoing" || closed.Events[0].Interval.OpenEnd {
		t.Fatalf("observed boundary remained open: %+v %v", closed, err)
	}
	cut, err := ReconcileObservationIntervals("build.event", old, result(20, 30), fresh)
	if err != nil || len(cut.Events) != 2 || !cut.Events[0].Interval.OpenEnd || !cut.Events[1].Interval.OpenStart || !cut.Events[0].Review || !cut.Events[1].Review {
		t.Fatalf("correction invented out-of-window boundaries: %+v %v", cut, err)
	}
	if _, err := ReconcileObservationIntervals("build.event", old, result(0, 60, span(10, 20, false, false), span(30, 40, false, false)), func(int) string { return "human-reason" }); err == nil {
		t.Fatal("K1 allowed a retired identity to be reused")
	}
}
