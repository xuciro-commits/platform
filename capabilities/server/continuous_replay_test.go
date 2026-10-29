package platformserver

import (
	"context"
	"encoding/json"
	"io"
	"platformserver/engine/resources"
	controlowner "platformserver/kernel/control"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"platformserver/apps/work"
	automation "platformserver/engine/automation"
	"platformserver/platform"
)

type replayComputeSource struct{ *frameSource }

type replayReadFiles struct {
	*resources.MemoryFiles
	once func()
}

func (s *replayReadFiles) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if hook := s.once; hook != nil {
		s.once = nil
		hook()
	}
	return s.MemoryFiles.Get(ctx, key)
}

func (s *replayComputeSource) Manifest() platform.Manifest {
	m := s.frameSource.Manifest()
	m.Flows[0].Continuous.DeadLetterRetry = true
	return m
}

func TestContinuousSignalReplayUsesRetainedOperationAndInputChannel(t *testing.T) {
	for _, dataChannel := range []bool{false, true} {
		name := "native"
		if dataChannel {
			name = "sealed-input"
		}
		t.Run(name, func(t *testing.T) {
			const tenant, id = "signal-replay-operation", "frames.window:source"
			base := newFrameComputeSource(tenant, 1<<20)
			if dataChannel {
				base = newFrameDataComputeSource(tenant, 1<<20)
			}
			source := &replayComputeSource{base}
			tn := composeTenant(t, tenant, []platform.Seat{seatOf("member", "frames:operator", "flow:admin"), seatOf("other", "flow:admin"), seatOf("admin", "platform:admin")}, work.New(tenant), automation.New(tenant), source)
			files := &replayReadFiles{MemoryFiles: &resources.MemoryFiles{}}
			tn.Files = files
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
			worker := &frameDataWorker{}
			if dataChannel {
				tn.ComputeWorker = worker
				if err := tn.files().Put(context.Background(), artifactKey(tenant, source.moduleDigest), source.module, "application/wasm"); err != nil {
					t.Fatal(err)
				}
			}
			at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
			decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
			member := memberOf(t, tn, "member")
			value := platform.Raw(7)
			if dataChannel {
				value = platform.Raw(strings.Repeat("a", 90<<10))
			}
			first := platform.FlowBatch{ID: "one", Signals: []platform.FlowSignal{
				{Key: "ready", Partition: "plant", At: at.Add(-6 * time.Second), Value: value},
				{Key: "future", Partition: "plant", At: at.Add(-time.Second), Value: value},
			}}
			if _, err := tn.ConsumeFlowBatch(member, id, first, at); err != nil {
				t.Fatal(err)
			}
			for _, run := range tn.operationDispatches(time.Now().UTC().Add(time.Second)) {
				run()
			}
			lateValue := platform.Raw(13)
			if dataChannel {
				lateValue = platform.Raw(strings.Repeat("z", 90<<10))
			}
			late := platform.FlowBatch{ID: "two", Predecessor: "one", Signals: []platform.FlowSignal{{Key: "late", Partition: "plant", At: at.Add(-time.Hour), Value: lateValue}}}
			if _, err := tn.ConsumeFlowBatch(member, id, late, at); err != nil {
				t.Fatal(err)
			}
			letters, _, err := platform.Find[platform.FlowDeadLetterAsset](tn.automation(automation.ID, false), platform.Query{})
			if err != nil || len(letters) != 1 {
				t.Fatalf("late input has no replayable original asset: %+v %v", letters, err)
			}
			if got := refuse(t, tn, "other", automation.ID, automation.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{}, at); got == "ok" {
				t.Fatal("another member replayed the protected source")
			}
			sub := submission(tn, member, automation.ID, automation.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{})
			if err := tn.cleanupFlowArtifactsFor(member, id, at); err != nil {
				t.Fatal(err)
			}
			before, _ := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
			objectsBefore := len(storedFiles(t, files))
			files.once = func() {
				decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaRevoke, controlowner.MemberType, member.ID, map[string]any{"app": automation.ID}, at)
			}
			if _, err := tn.Submit(member, sub, at); err == nil {
				t.Fatal("permission revoked during artifact I/O still replayed the source")
			}
			after, _ := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
			assetBefore, _ := platform.Get[platform.FlowDeadLetterAsset](tn.automation(automation.ID, false), letters[0].ID)
			if string(platform.Raw(before)) != string(platform.Raw(after)) || assetBefore.Replayed || len(tn.operationDispatches(time.Now().UTC().Add(time.Second))) != 0 {
				t.Fatal("refused replay exposed partial instance, asset or operation state")
			}
			objectsAfter := len(storedFiles(t, files))
			if objectsAfter != objectsBefore {
				t.Fatalf("durably refused replay retained unused frame/input objects: %d -> %d", objectsBefore, objectsAfter)
			}
			decide(t, tn, "admin", controlowner.PlatformApp, controlowner.SchemaGrant, controlowner.MemberType, member.ID, map[string]any{"app": automation.ID, "role": "admin"}, at)
			member = memberOf(t, tn, "member")
			sub = submission(tn, member, automation.ID, automation.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{})
			receipt, refusal := tn.Submit(member, sub, at)
			if refusal != nil {
				t.Fatalf("retained Compute replay was refused: %v %s roles=%v", refusal, refusal.Message, member.Roles)
			}
			duplicate, refusal := tn.Submit(member, sub, at)
			if refusal != nil || !proto.Equal(receipt, duplicate) {
				t.Fatal("the same HTTP key did not return the original replay receipt", refusal)
			}
			if got := refuse(t, tn, "member", automation.ID, automation.SchemaFlowReplay, "flow.dead-letter", letters[0].ID, map[string]any{}, at); got == "ok" {
				t.Fatal("a fresh command replayed the asset twice")
			}
			runs := tn.operationDispatches(time.Now().UTC().Add(time.Second))
			if len(runs) != 1 {
				t.Fatalf("replay did not plan one original operation: %d", len(runs))
			}
			runs[0]()
			x, _ := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), id)
			asset, _ := platform.Get[platform.FlowDeadLetterAsset](tn.automation(automation.ID, false), letters[0].ID)
			if !asset.Replayed || x.Batch.Cursor != "replay:"+asset.ID || len(x.Tokens) != 1 || x.Tokens[0].Waits != "stream" || x.Data != "{}" {
				t.Fatal("replay did not settle the original instance and asset together")
			}
			if dataChannel {
				var input map[string]string
				if worker.calls != 2 || json.Unmarshal(worker.data, &input) != nil || input["payload"] != strings.Repeat("z", 90<<10) {
					t.Fatal("replay lost the original late input or bypassed its sealed channel")
				}
			} else {
				var output map[string]int
				if len(source.computeInputs) != 2 || json.Unmarshal(x.Outputs["aggregate"], &output) != nil || output["count"] != 2 {
					t.Fatal("late replay included the unclosed future/grace row", string(x.Outputs["aggregate"]))
				}
			}
		})
	}
}
