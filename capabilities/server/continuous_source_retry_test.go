package platformserver

import (
	"testing"
	"time"

	"platformserver/apps/work"
	automation "platformserver/engine/automation"
	"platformserver/platform"
)

type retryIntakeSource struct {
	*frameSource
	latest bool
}

func (s *retryIntakeSource) Manifest() platform.Manifest {
	m := s.frameSource.Manifest()
	intake := &platform.StreamIntake{SourceRecord: "stream", Key: "eventId", Partition: []string{"plantId"}, EventTime: "eventAt", Value: "reading", Retry: &platform.StreamPreparationRetry{Attempts: 3, BackoffMS: 1}}
	if s.latest {
		intake.Offset = "latest"
	}
	m.Flows[0].Continuous.Intake = intake
	return m
}
func TestContinuousSourceWithoutControlledIntakeRefuses(t *testing.T) {
	const tenant, id = "no-controlled-intake", "frames.window:source"
	at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), automation.New(tenant), newFrameSource(tenant, 2<<20))
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	if _, refusal := tn.ConsumeFlowSource(memberOf(t, tn, "member"), id, at); refusal == nil {
		t.Fatal("a native batch-only instance accepted an uncontrolled source read")
	}
}
