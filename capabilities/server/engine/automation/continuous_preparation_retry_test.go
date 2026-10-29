package automation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type retryFrameStore struct {
	objects                    map[string][]byte
	reads, seals, discards     int
	readFailures, sealFailures int
}

func (s *retryFrameStore) Read(ref platform.FlowStateArtifact) ([]byte, error) {
	s.reads++
	if s.reads <= s.readFailures {
		return nil, fmt.Errorf("temporary frame read failure")
	}
	return slices.Clone(s.objects[ref.Ticket]), nil
}

func (s *retryFrameStore) Seal(instance string, version int, raw []byte, _ int) (platform.FlowStateArtifact, error) {
	s.seals++
	ref := platform.FlowStateArtifact{Tenant: "retry", Instance: instance, Version: version, Ticket: fmt.Sprint(s.seals), Size: len(raw)}
	s.objects[ref.Ticket] = slices.Clone(raw)
	if s.seals <= s.sealFailures {
		return ref, fmt.Errorf("temporary Put response failure after storing bytes")
	}
	return ref, nil
}

func (s *retryFrameStore) Discard(ref platform.FlowStateArtifact) error {
	s.discards++
	delete(s.objects, ref.Ticket)
	return nil
}

func TestContinuousWindowPreparationRetriesOnlyFailedIO(t *testing.T) {
	for _, scenario := range []string{"read-recovers", "seal-recovers", "read-exhausted", "seal-exhausted", "bad-history", "over-budget", "policy-denied", "kernel-refused"} {
		t.Run(scenario, func(t *testing.T) {
			at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
			rule := platform.Continuous{Source: "frames", Batch: 2, State: 1 << 20, FrameBytes: 2 << 20, DeadLetter: true,
				Window: &platform.StreamWindow{Node: "window", WindowMS: 30000, SlideMS: 5000, WatermarkMS: 2000, MaxRecords: 10, LateEvents: "sideOutput", Retry: &platform.StreamPreparationRetry{Attempts: 3, BackoffMS: 1}}}
			frame := &BatchFrame{State: map[string]json.RawMessage{}}
			if _, _, err := foldBatch(frame, rule, Batch{ID: "before"}, at.Add(-5*time.Second)); err != nil {
				t.Fatal(err)
			}
			store := &retryFrameStore{objects: map[string][]byte{"original": platform.Raw(frame)}}
			ref := platform.FlowStateArtifact{Tenant: "retry", Instance: "instance", Version: 1, Ticket: "original"}
			summary := *frame
			summary.State, summary.Sealed = nil, &ref
			before := FlowInstance{Record: platform.Record{ID: "instance"}, Flow: "frames.window", Version: 1, OnBehalf: "member", State: "waiting", Batch: &summary}
			plan := &BatchPreparation{owner: New("retry"), before: before, rule: rule}
			batch := Batch{ID: "next", Predecessor: "before", Signals: []Signal{{Key: "ready", At: at.Add(-6 * time.Second), Value: platform.Raw(7)}}}
			wantReads, wantSeals := 1, 0
			success := false
			switch scenario {
			case "read-recovers":
				store.readFailures, wantReads, wantSeals, success = 2, 3, 1, true
			case "seal-recovers":
				store.sealFailures, wantReads, wantSeals, success = 2, 3, 3, true
			case "read-exhausted":
				store.readFailures, wantReads = 3, 3
			case "seal-exhausted":
				store.sealFailures, wantReads, wantSeals = 3, 3, 3
			case "bad-history":
				store.objects[ref.Ticket] = []byte("{")
			case "over-budget":
				plan.rule.State = 1
			case "policy-denied":
				plan.rule.Entry, plan.entry = "calculate", "calculate"
				plan.operation = &platform.OperationStep{Request: func(platform.Caller, *platform.Run) (platform.OperationRequest, *kernel.Error) {
					return platform.OperationRequest{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "revoked operation")
				}}
			case "kernel-refused":
				plan.rule.Aggregate = &platform.StreamAggregate{Node: "aggregate", Kernel: "wasm:f32-window-stats", Measures: []string{"mean"}}
				batch.Signals[0].Value = json.RawMessage("1e300")
			}
			original := platform.Raw(plan.before)
			prepared, refusal := plan.Prepare(batch, at, store)
			if (refusal == nil) != success || (prepared != nil) != success || store.reads != wantReads || store.seals != wantSeals {
				t.Fatalf("retry result=%v reads=%d seals=%d, want success=%v reads=%d seals=%d", refusal, store.reads, store.seals, success, wantReads, wantSeals)
			}
			if !bytes.Equal(original, platform.Raw(plan.before)) {
				t.Fatal("outside retry advanced the captured instance")
			}
			wantObjects := 1
			if success {
				wantObjects++
			}
			if len(store.objects) != wantObjects || store.discards != min(store.seals, store.sealFailures) {
				t.Fatalf("failed preparation retained orphan objects: count=%d discards=%d", len(store.objects), store.discards)
			}
			if success {
				var result BatchFrame
				if json.Unmarshal(store.objects[prepared.Artifact().Ticket], &result) != nil || result.Cursor != "next" || result.Consumed != 1 || !prepared.Matches(before) {
					t.Fatal("successful retry did not retain exactly one prepared successor")
				}
			}
		})
	}
}
