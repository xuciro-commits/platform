package host

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/automate/runtime"
	"platformserver/services/resources/authoring"
)

func TestInvariantK13LaneWaitersLeaveAndDiagnosticsDoNotWait(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-lane-wait", []platform.App{automation.New("invariant-lane-wait")})
	if err := live.mu.Hold(context.Background(), "invariant-holder"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	answered := make(chan *kernel.Error, 1)
	go func() {
		_, refusal := coordinatedRecordRead(live.Tenant, ctx, func(t *Tenant) (struct{}, *kernel.Error) { return struct{}{}, nil })
		answered <- refusal
	}()
	deadline := time.Now().Add(2 * time.Second)
	for live.mu.State().Waiters != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	lane := live.laneHealth(time.Now(), true)
	if lane.Holder != "invariant-holder" || lane.Waiters != 1 {
		t.Fatalf("lane health while held: %+v; want the holder and one waiter", lane)
	}
	if queues, _ := live.queueHealth(time.Now()); queues == nil {
		t.Fatal("queue health waited for or failed under a held lane")
	}
	cancel()
	select {
	case refusal := <-answered:
		if refusal == nil {
			t.Fatal("a cancelled read was answered as if it had read")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled read stayed queued behind the lane")
	}
	if waiters := live.mu.State().Waiters; waiters != 0 {
		t.Fatalf("cancelled waiter still counted: %d", waiters)
	}
	live.mu.Unlock()
	if _, refusal := coordinatedRecordRead(live.Tenant, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) { return struct{}{}, nil }); refusal != nil {
		t.Fatalf("read after release: %v", refusal)
	}
}

func TestInvariantK13BackgroundRoundReleasesLaneBetweenTasks(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-lane-round", []platform.App{automation.New("invariant-lane-round")})
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, "build.process.create", build.ProcessType, "PROCESS", map[string]any{
		"name": "scheduled", "title": "Scheduled", "kind": "automation", "every": "1m",
		"steps": []build.ProcessStep{{Name: "create", Kind: "action", Act: "build.alpha.create",
			Target: &platform.Binding{Source: "input", Path: []string{"period"}},
			Inputs: map[string]platform.Binding{"note": {Source: "literal", Value: json.RawMessage(`"scheduled"`)}},
		}},
	})
	live.accept(t, build.SchemaProcess, build.ProcessType, "PROCESS", map[string]any{})
	var mu sync.Mutex
	holds := map[string]int{}
	live.mu.Observe(func(holder string, _, _ time.Duration) {
		mu.Lock()
		holds[holder]++
		mu.Unlock()
	})
	live.Work(invariantClock)
	if got := live.records.Type(platform.FlowInstanceType).Rows().Len(); got != 1 {
		t.Fatalf("scheduled instances: got %d; want 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if holds["work"] < 2 {
		t.Fatalf("background work held the lane %d times; want one hold per task and job: %v", holds["work"], holds)
	}
}

func TestInvariantK13ReadScopesKeepOneStateAndOwnContext(t *testing.T) {
	live := newInvariantTenant(t, "invariant-read-scopes")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "before"})
	member := live.member(t)
	leftContext, cancel := context.WithCancel(t.Context())
	defer cancel()
	live.mu.Lock()
	left, leftError := live.forkReadState(leftContext)
	right, rightError := live.forkReadState(t.Context())
	live.mu.Unlock()
	if leftError != nil || rightError != nil {
		t.Fatalf("prepare independent scopes: %v, %v", leftError, rightError)
	}
	var readers sync.WaitGroup
	started := make(chan struct{})
	for _, scope := range []*Tenant{left, right} {
		readers.Go(func() {
			<-started
			for range 32 {
				view, refusal := scope.RecordOf(scope.readContext(), member, "build.alpha", "A1", invariantClock)
				if refusal != nil {
					t.Errorf("read retained scope: %v", refusal)
					return
				}
				var record map[string]any
				if json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "before" {
					t.Errorf("read scope mixed current and retained state: %v", view.Record)
					return
				}
			}
		})
	}
	close(started)
	live.accept(t, "build.alpha.edit", "build.alpha", "A1", map[string]any{"note": "after"})
	readers.Wait()
	cancel()
	if _, refusal := left.RecordOf(leftContext, member, "build.alpha", "A1", invariantClock); refusal == nil {
		t.Fatal("cancelled scope continued reading")
	}
	if _, refusal := right.RecordOf(t.Context(), member, "build.alpha", "A1", invariantClock); refusal != nil || right.readContext().Err() != nil {
		t.Fatalf("one scope's cancellation affected another: %v", refusal)
	}
	view, refusal := live.RecordOf(t.Context(), member, "build.alpha", "A1", invariantClock)
	var record map[string]any
	if refusal != nil || json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "after" {
		t.Fatalf("current tenant lost its accepted change: %v, %v", view.Record, refusal)
	}
}
