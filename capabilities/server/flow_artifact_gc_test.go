package platformserver

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

type orphanRetryFiles struct {
	*memoryFiles
	key  string
	fail bool
}

func (s *orphanRetryFiles) Delete(ctx context.Context, key string) error {
	if key == s.key && s.fail {
		s.fail = false
		return fmt.Errorf("object store unavailable")
	}
	return s.memoryFiles.Delete(ctx, key)
}

func TestContinuousOrphanCollectionPreservesHistoryAndRecoveryBoundary(t *testing.T) {
	const tenant, id = "orphan-gc", "frames.window:source"
	now := time.Now().UTC()
	files := &orphanRetryFiles{memoryFiles: &memoryFiles{}}
	source := newFrameSource(tenant, 2<<20)
	live := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), source)
	live.Files = files
	decide(t, live, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, now)
	feed := func(key, previous string) platform.FlowStateArtifact {
		t.Helper()
		if _, err := live.ConsumeFlowBatch(memberOf(t, live, "member"), id, platform.FlowBatch{ID: key, Predecessor: previous, Signals: []platform.FlowSignal{{Key: key, At: now, Value: platform.Raw(1)}}}, now); err != nil {
			t.Fatal(err)
		}
		held, ok := platform.Get[platform.FlowInstance](live.automation(platform.FlowApp, false), id)
		if !ok || held.Batch == nil || held.Batch.Sealed == nil {
			t.Fatal("fixture has no accepted frame")
		}
		return *held.Batch.Sealed
	}
	frameStore := flowFrameStore{tenant: tenant, files: files}
	old := feed("one", "")
	oldRaw, err := frameStore.Read(old)
	if err != nil {
		t.Fatal(err)
	}
	current := feed("two", "one")
	if err := live.cleanupFlowArtifactsFor(memberOf(t, live, "member"), id, now); err != nil {
		t.Fatal(err)
	}
	// Reintroduce a file referenced only by complete accepted history, not the
	// latest live frame or its now-acknowledged accepted retirement intent.
	oldKey, _ := frameStore.key(old)
	if err := files.Put(context.Background(), oldKey, oldRaw, "application/json"); err != nil {
		t.Fatal(err)
	}
	orphan, err := frameStore.Seal(id, 1, platform.Raw(map[string]any{"unused": true}), 1024)
	if err != nil {
		t.Fatal(err)
	}
	orphanKey, _ := frameStore.key(orphan)
	newer, err := frameStore.Seal(id, 1, platform.Raw(map[string]any{"unknown": true}), 1024)
	if err != nil {
		t.Fatal(err)
	}
	newerKey, _ := frameStore.key(newer)
	currentKey, _ := frameStore.key(current)
	files.mu.Lock()
	for _, key := range []string{oldKey, currentKey, orphanKey} {
		files.modified[key] = now.Add(-48 * time.Hour)
	}
	files.modified[newerKey] = now.Add(-30 * time.Minute)
	files.mu.Unlock()
	if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if !files.Exists(context.Background(), orphanKey) {
		t.Fatal("creation assumed a completed recovery")
	}
	live.flowArtifactRecoveryComplete(now.Add(-time.Hour))
	release := live.beginFlowArtifactPreparation()
	if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if !files.Exists(context.Background(), orphanKey) {
		t.Fatal("active preparation did not exclude collection")
	}
	release()
	files.key, files.fail = orphanKey, true
	if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err == nil {
		t.Fatal("failed delete was reported as cleanup")
	}
	if !files.Exists(context.Background(), orphanKey) {
		t.Fatal("failed deletion lost its retry object")
	}
	if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if files.Exists(context.Background(), orphanKey) {
		t.Fatal("original poll did not retry orphan deletion")
	}
	for _, key := range []string{oldKey, currentKey, newerKey} {
		if !files.Exists(context.Background(), key) {
			t.Fatal("collector deleted an accepted, historical or unresolved object", key)
		}
	}
	// The same collector fails closed when a complete reference image cannot
	// be decoded. No approximation of the latest record authorizes this orphan.
	bad, err := frameStore.Seal(id, 1, platform.Raw(map[string]any{"unaccepted": true}), 1024)
	if err != nil {
		t.Fatal(err)
	}
	badKey, _ := frameStore.key(bad)
	files.mu.Lock()
	files.modified[badKey] = now.Add(-48 * time.Hour)
	files.mu.Unlock()
	for _, unavailable := range []FileStore{
		unavailableInventoryFiles{memoryFiles: files.memoryFiles},
		badCursorInventoryFiles{memoryFiles: files.memoryFiles, key: badKey},
	} {
		live.Files = unavailable
		if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err == nil {
			t.Fatal("unavailable or truncated inventory authorized collection")
		}
		if !files.Exists(context.Background(), badKey) {
			t.Fatal("incomplete inventory deleted an object")
		}
	}
	live.Files = files
	live.records.mu.Lock()
	row := live.records.types[platform.FlowInstanceType].rows.Get(id)
	row.history = append(row.history, RecordChange{Fields: []FieldChange{{Field: "batch", Before: []byte("{")}}})
	live.records.mu.Unlock()
	if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err == nil {
		t.Fatal("invalid historical reference image authorized collection")
	}
	if !files.Exists(context.Background(), badKey) {
		t.Fatal("incomplete reference read deleted an object")
	}
	live.quarantine(fmt.Errorf("recovery incomplete"))
	if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if !files.Exists(context.Background(), badKey) {
		t.Fatal("quarantined tenant deleted an object")
	}
}

func TestFlowArtifactReferencesRetainOriginalRecoveryBytes(t *testing.T) {
	store := flowFrameStore{tenant: "original", files: &memoryFiles{}}
	ref := platform.FlowStateArtifact{Tenant: "original", Instance: "old", Version: 1, Ticket: strings.Repeat("a", 32), Digest: strings.Repeat("b", 64), Size: 2}
	key, err := store.key(ref)
	if err != nil {
		t.Fatal(err)
	}
	records := newRecordStore()
	et := &entityType{}
	et.setRow("old", &row{value: reflect.ValueOf(platform.FlowInstance{}), original: platform.Raw(map[string]any{"batch": map[string]any{"sealed": ref}})})
	records.types[platform.FlowInstanceType] = et
	keep, err := records.flowArtifactReferences(store)
	if err != nil || !keep[key] {
		t.Fatal("original recovery bytes lost their artifact root", err)
	}
}

func TestContinuousOrphanCollectionPreservesExactLargeVersions(t *testing.T) {
	if reflect.TypeOf(int(0)).Bits() < 64 {
		t.Skip("version exceeds the platform's integer width")
	}
	const tenant, id = "exact-version", "frames.window:source"
	now := time.Now().UTC()
	wideVersion := int64(9007199254740993)
	version := int(wideVersion)
	files := &memoryFiles{}
	live := composeTenant(t, tenant, nil, work.New(tenant), flow.New(tenant), newFrameSource(tenant, 2<<20))
	live.Files = files
	store := flowFrameStore{tenant: tenant, files: files}
	seal := func(label string) platform.FlowStateArtifact {
		t.Helper()
		ref, err := store.Seal(id, version, platform.Raw(map[string]any{"label": label}), 1024)
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}
	historical, current, orphan := seal("historical"), seal("current"), seal("orphan")
	x := platform.FlowInstance{Record: platform.Record{ID: id}, Version: version, Batch: &platform.FlowBatchFrame{Sealed: &historical}}
	if err := live.automation(platform.FlowApp, true).PutAt(now, "test.frame", x); err != nil {
		t.Fatal(err)
	}
	x.Batch.Sealed = &current
	if err := live.automation(platform.FlowApp, true).PutAt(now, "test.frame", x); err != nil {
		t.Fatal(err)
	}
	files.mu.Lock()
	for key := range files.m {
		files.modified[key] = now.Add(-48 * time.Hour)
	}
	files.mu.Unlock()
	live.flowArtifactRecoveryComplete(now.Add(-time.Hour))
	if err := live.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []platform.FlowStateArtifact{historical, current} {
		if _, err := store.Read(ref); err != nil {
			t.Fatal("collector deleted a frame with an exact accepted version", err)
		}
	}
	orphanKey, _ := store.key(orphan)
	if files.Exists(context.Background(), orphanKey) {
		t.Fatal("unaccepted frame was not collected")
	}
}
