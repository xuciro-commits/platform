package host

import (
	"context"
	"encoding/json"
	"platformserver/services/objects"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/automate/runtime"
	"platformserver/services/resources/authoring"
)

type readImageSQLState struct {
	sqlShapedState
	position   atomic.Int64
	mismatches chan int64
}

func (s *readImageSQLState) BeginStateRead(ctx context.Context, tenant string, applied int64) (persistence.StateReadView, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	position := s.position.Load()
	if position != applied {
		select {
		case s.mismatches <- applied:
		default:
		}
		return nil, &persistence.StatePositionError{Tenant: tenant, Position: position, Applied: applied}
	}
	return readImageSQLView{sqlShapedView: sqlShapedView{reads: &sqlShapedReads{}}, position: position}, nil
}

type readImageSQLView struct {
	sqlShapedView
	position int64
}

func (v readImageSQLView) Position() int64 { return v.position }

func TestInvariantK13LaneWaitersLeaveAndDiagnosticsDoNotWait(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-lane-wait", []platform.App{automation.New("invariant-lane-wait")})
	if err := live.Lane.Hold(context.Background(), "invariant-holder"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	answered := make(chan *kernel.Error, 1)
	go func() {
		_, refusal := lockedRecordRead(live.Tenant, ctx, func(t *Tenant) (struct{}, *kernel.Error) { return struct{}{}, nil })
		answered <- refusal
	}()
	deadline := time.Now().Add(2 * time.Second)
	for live.Lane.State().Waiters != 1 && time.Now().Before(deadline) {
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
	if waiters := live.Lane.State().Waiters; waiters != 0 {
		t.Fatalf("cancelled waiter still counted: %d", waiters)
	}
	live.Lane.Unlock()
	if _, refusal := lockedRecordRead(live.Tenant, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) { return struct{}{}, nil }); refusal != nil {
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
	live.Lane.Observe(func(holder string, _, _ time.Duration) {
		mu.Lock()
		holds[holder]++
		mu.Unlock()
	})
	live.Work(invariantClock)
	if got := live.Projection.Type(platform.FlowInstanceType).Rows().Len(); got != 1 {
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
	live.Lane.Lock()
	image := live.readImage.Load()
	left, leftError := image.state.forkReadState(leftContext)
	right, rightError := image.state.forkReadState(t.Context())
	live.Lane.Unlock()
	if leftError != nil || rightError != nil {
		t.Fatalf("prepare independent scopes: %v, %v", leftError, rightError)
	}
	var readers sync.WaitGroup
	started := make(chan struct{})
	for _, scope := range []*Tenant{left, right} {
		readers.Go(func() {
			<-started
			for range 32 {
				_ = scope.Catalog(member)
				_ = scope.Definitions(member)
				for _, entity := range scope.Entities(member) {
					if entity.Type == "build.alpha" && entity.Title != "Alpha" {
						t.Errorf("read scope mixed metadata from a later publication: %s", entity.Title)
						return
					}
				}
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
	live.accept(t, "build.object.edit", build.ObjectType, "OBJECT", map[string]any{"title": "Changed alpha"})
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
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

func TestInvariantK13PublishedReadsDoNotWaitForBackgroundLane(t *testing.T) {
	live := newInvariantTenant(t, "invariant-published-read")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "before"})
	member := live.member(t)
	image := live.readImage.Load()
	if image == nil {
		t.Fatal("an applied result did not publish a read image")
	}
	if err := live.Lane.Hold(t.Context(), "background"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	view, refusal := live.RecordOf(ctx, member, "build.alpha", "A1", invariantClock)
	cancel()
	lane := live.Lane.State()
	live.Lane.Unlock()
	if refusal != nil || lane.Holder != "background" || lane.Waiters != 0 {
		t.Fatalf("read waited behind background work: %v, %+v", refusal, lane)
	}
	var record map[string]any
	if json.Unmarshal(platform.Raw(view.Record), &record) != nil || record["note"] != "before" {
		t.Fatalf("published read returned another state: %v", view.Record)
	}
	version := live.liveVersion("/v1/records/build.alpha/A1")
	_, wake := live.Changes()
	live.Lane.Lock()
	submission := live.submission("build.alpha.edit", "build.alpha", "A1", map[string]any{"note": "after"})
	_, updateRefusal := live.Tenant.accept(live.App(build.ID), member, submission, invariantClock, acceptOptions{})
	if updateRefusal != nil {
		live.Lane.Unlock()
		t.Fatalf("accept the next state: %v", updateRefusal)
	}
	if live.liveVersion("/v1/records/build.alpha/A1") != version {
		t.Error("live read invalidation advanced before image publication")
	}
	select {
	case <-wake:
		t.Error("live readers were notified before image publication")
	default:
	}
	live.Lane.Unlock()
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("published changes did not wake live readers")
	}
	retained, refusal, err := readImageState(live.Tenant, image.state, t.Context(), func(scope *Tenant) (objects.RecordView, *kernel.Error) {
		return scope.recordOf(member, "build.alpha", "A1", invariantClock)
	})
	if err != nil || refusal != nil || json.Unmarshal(platform.Raw(retained.Record), &record) != nil || record["note"] != "before" {
		t.Fatalf("publication changed an older reader's state: %v, %v, %v", retained.Record, refusal, err)
	}
	current, refusal := live.RecordOf(t.Context(), member, "build.alpha", "A1", invariantClock)
	if refusal != nil || json.Unmarshal(platform.Raw(current.Record), &record) != nil || record["note"] != "after" {
		t.Fatalf("latest publication missed an accepted update: %v, %v", current.Record, refusal)
	}
}

func TestInvariantK13SQLReadsRetryWithoutMixingPositions(t *testing.T) {
	live := newInvariantTenant(t, "invariant-image-sql-position")
	state := &readImageSQLState{mismatches: make(chan int64, 8)}
	state.position.Store(1)
	live.Lane.Lock()
	live.Projection.Current().Configure(state, live.ID, 1, state.position.Load)
	live.sequences.Set(map[string]int{"position": 1})
	live.enableCurrentReadScopes()
	live.Lane.Unlock()
	live.Lane.Lock()
	state.position.Store(2)
	type result struct {
		position int64
		sequence int
		refusal  *kernel.Error
	}
	answered := make(chan result, 1)
	go func() {
		value, refusal := coordinatedRecordRead(live.Tenant, t.Context(), func(scope *Tenant) (result, *kernel.Error) {
			return result{position: scope.Projection.Current().Applied(), sequence: scope.sequences.Clone()["position"]}, nil
		})
		value.refusal = refusal
		answered <- value
	}()
	select {
	case applied := <-state.mismatches:
		if applied != 1 {
			t.Errorf("first read used position %d, want 1", applied)
		}
	case <-time.After(time.Second):
		live.Lane.Unlock()
		t.Fatal("the reader waited on the lane instead of checking its image position")
	}
	live.sequences.Set(map[string]int{"position": 2})
	live.acceptedResultApplied()
	live.Lane.Unlock()
	select {
	case value := <-answered:
		if value.refusal != nil || value.position != 2 || value.sequence != 2 || live.quarantined() {
			t.Fatalf("read mixed two positions or quarantined a normal publication delay: %+v", value)
		}
	case <-time.After(time.Second):
		t.Fatal("the read did not resume when a consistent image was published")
	}
	state.position.Store(1)
	executed := false
	_, refusal := coordinatedRecordRead(live.Tenant, t.Context(), func(scope *Tenant) (struct{}, *kernel.Error) {
		executed = true
		return struct{}{}, nil
	})
	if refusal == nil || executed || !live.quarantined() {
		t.Fatal("SQL behind the applied image was served as a consistent read")
	}
}

func TestInvariantK13ConcurrentSubmissionsKeepOneSerialHistory(t *testing.T) {
	live := newInvariantTenant(t, "invariant-concurrent-submit")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "before"})
	member := live.member(t)
	submissions := []*pb.Submission{
		live.submission("build.alpha.edit", "build.alpha", "A1", map[string]any{"note": "first"}),
		live.submission("build.alpha.edit", "build.alpha", "A1", map[string]any{"note": "second"}),
	}
	for _, submission := range submissions {
		revision := uint32(1)
		submission.ExpectedRevision = &revision
	}
	type answer struct {
		index   int
		record  *pb.ChangeRecord
		refusal *kernel.Error
	}
	answered := make(chan answer, len(submissions))
	live.Lane.Lock()
	for index, submission := range submissions {
		go func() {
			record, refusal := live.Submit(member, submission, invariantClock)
			answered <- answer{index: index, record: record, refusal: refusal}
		}()
	}
	deadline := time.Now().Add(10 * time.Second)
	for live.Lane.State().Waiters != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	waiters := live.Lane.State().Waiters
	live.Lane.Unlock()
	if waiters != 2 {
		t.Fatal("both decisions did not reach commit while the lane was held")
	}
	accepted, refused, note := 0, 0, ""
	for range submissions {
		var result answer
		select {
		case result = <-answered:
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent submissions did not finish")
		}
		repeated, refusal := live.Submit(member, submissions[result.index], invariantClock)
		if result.refusal == nil {
			accepted++
			if refusal != nil || !proto.Equal(result.record, repeated) {
				t.Fatalf("accepted retry differs: %v", refusal)
			}
			var payload map[string]string
			_ = json.Unmarshal(result.record.GetSubmission().GetPayload(), &payload)
			note = payload["note"]
		} else {
			refused++
			if result.refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || refusal == nil || refusal.Code != result.refusal.Code {
				t.Fatalf("stale write or retry was not refused consistently: %v, %v", result.refusal, refusal)
			}
		}
	}
	view, refusal := live.RecordOf(t.Context(), member, "build.alpha", "A1", invariantClock)
	var record struct {
		Revision uint32
		Note     string
	}
	if accepted != 1 || refused != 1 || refusal != nil || json.Unmarshal(platform.Raw(view.Record), &record) != nil || record.Revision != 2 || record.Note != note {
		t.Fatalf("concurrent writes are not one serial history: accepted=%d refused=%d record=%+v refusal=%v", accepted, refused, record, refusal)
	}
	restored := live.restore(t, live.entries)
	if differing := differingTypes(live.recordImage(t), restored.recordImage(t)); len(differing) != 0 {
		t.Fatalf("concurrent accepted history replays differently: %v", differing)
	}
}
