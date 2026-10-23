package platformserver

import (
	"fmt"
	"platformkernel/kernel"
	"strings"
	"testing"
	"time"

	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

type retainedNativeSource struct{ *nativeFrameSource }

type retainedFrameSource struct{ *frameSource }

type retentionWithDueSource struct{ *retainedFrameSource }

func (s *retentionWithDueSource) Manifest() platform.Manifest {
	m := s.retainedFrameSource.Manifest()
	m.Flows[0].Continuous.DeadLetterMaxRecords = 16
	m.Flows = append(m.Flows, platform.Flow{Name: "due", Title: "Due ordinary flow", Version: 1,
		Start: platform.Start{On: []string{"frames.source.start"}, Begin: func(platform.Caller, platform.Event) (string, any, bool) { return "source", nil, true }},
		Steps: []platform.Step{
			{Name: "wait", Next: "done", Wait: &platform.Wait{At: func(_ platform.Caller, run *platform.Run) (time.Time, *kernel.Error) {
				return run.Now.Add(time.Second), nil
			}}},
			{Name: "done", End: true},
		},
	})
	return m
}

func (s *retainedFrameSource) Manifest() platform.Manifest {
	m := s.frameSource.Manifest()
	m.Flows[0].Continuous.DeadLetterRetry = true
	m.Flows[0].Continuous.DeadLetterMaxRecords = 2
	m.Flows[0].Continuous.DeadLetterTTLMs = 1000
	return m
}

func (s *retainedNativeSource) Manifest() platform.Manifest {
	m := s.nativeFrameSource.Manifest()
	m.Flows[0].Continuous.DeadLetterMaxRecords = 2
	m.Flows[0].Continuous.DeadLetterTTLMs = 1000
	return m
}

func TestContinuousDeadLetterAssetsRetainBoundsExpiryAndRecovery(t *testing.T) {
	const tenant, id = "dead-letter-retention", "frames.window:source"
	files := &memoryFiles{}
	compose := func() *Tenant {
		tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), &retainedNativeSource{newNativeFrameSource(tenant)})
		tn.Files = files
		return tn
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	for i := range 8 {
		decide(t, tn, "member", "frames", "frames.probe.create", "frames.probe", fmt.Sprint(i), map[string]any{"blocked": true}, at)
	}
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	first := platform.FlowBatch{ID: "one"}
	for i := range 8 {
		first.Signals = append(first.Signals, platform.FlowSignal{Key: fmt.Sprint(i), Partition: fmt.Sprint(i), At: at.Add(-6 * time.Second), Value: platform.Raw(map[string]any{"probeId": fmt.Sprint(i), "reading": 13})})
	}
	if _, err := tn.ConsumeFlowBatch(member, id, first, at); err != nil {
		t.Fatal(err)
	}
	active, _, err := platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	all, _, _ := platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{Archived: true})
	if err != nil || len(active) != 2 || len(all) != 8 || active[0].Action == "" {
		t.Fatalf("action failures escaped the retained live bound: active=%d all=%d %v", len(active), len(all), err)
	}
	expiredAt := at.Add(time.Second)
	for _, asset := range active {
		decide(t, tn, "member", "frames", "frames.probe.unblock", "frames.probe", asset.Target, map[string]any{}, at)
		if got := refuse(t, tn, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", asset.ID, map[string]any{}, expiredAt); got == "ok" {
			t.Fatal("an expired action replay ran before the retention timer")
		}
		if probe, _ := platform.Get[nativeProbeRecord](tn.automation("frames", false), asset.Target); probe.Applied != 0 {
			t.Fatal("expiry refusal delivered the original rejected business action")
		}
	}
	late := platform.FlowBatch{ID: "two", Predecessor: "one"}
	for i := range 3 {
		late.Signals = append(late.Signals, platform.FlowSignal{Key: fmt.Sprint("late-", i), Partition: "late", At: at.Add(-time.Hour), Value: platform.Raw(map[string]any{"probeId": "late", "reading": 13})})
	}
	if _, err := tn.ConsumeFlowBatch(member, id, late, at.Add(100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	active, _, err = platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	if err != nil || len(active) != 2 || active[0].Action != "" || active[1].Action != "" {
		t.Fatal("signal and action assets did not share the original retained bound", err)
	}
	for _, asset := range active {
		if got := refuse(t, tn, "member", flow.ID, flow.SchemaFlowReplay, "flow.dead-letter", asset.ID, map[string]any{}, at.Add(1100*time.Millisecond)); got == "ok" {
			t.Fatal("an expired signal was prepared before its retention timer")
		}
	}
	decide(t, tn, "member", flow.ID, flow.SchemaFlowStop, flow.InstanceType, id, map[string]any{}, at)
	for tick := range 3 {
		tn.Work(at.Add(time.Duration(tick+2) * time.Second))
	}
	active, _, err = platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	if err != nil || len(active) != 0 {
		t.Fatal("terminal instance retained expired live assets", len(active), err)
	}
	all, _, err = platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{Archived: true})
	if err != nil || len(all) != 10 {
		t.Fatal("retention removed original asset records", len(all), err)
	}
	archiveTransitions := 0
	for _, asset := range all {
		view, err := tn.RecordOf(member, "flow.dead-letter", asset.ID, at.Add(5*time.Second))
		if err != nil || !asset.Archived || asset.Replayed || len(view.History) < 1 || asset.Letter.Value == "" {
			t.Fatal("archive lost original content/history or replayed an expired asset", err)
		}
		if len(view.History) >= 2 {
			archiveTransitions++
		}
	}
	if archiveTransitions != 4 {
		t.Fatal("live action and signal rows did not retain their later archive decisions", archiveTransitions)
	}
	CheckReplay(t, tn, entries, compose)
	raw, _, snapshotErr := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	recovered := compose()
	if err := recovered.Restore(raw); err != nil {
		t.Fatal(err)
	}
	all, _, err = platform.Find[platform.FlowDeadLetterAsset](recovered.automation(flow.ID, false), platform.Query{Archived: true})
	if err != nil || len(all) != 10 {
		t.Fatal("snapshot lost archived assets", err)
	}
	for _, asset := range all {
		if !asset.Archived || asset.Replayed {
			t.Fatal("snapshot revived an expired or replayed asset")
		}
	}
}

func TestContinuousDeadLetterArchiveProgressesPastPagingTarget(t *testing.T) {
	const tenant, id = "large-dead-letter", "frames.window:source"
	at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), &retainedFrameSource{newFrameSource(tenant, 2<<20)})
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	member := memberOf(t, tn, "member")
	large := platform.Raw(strings.Repeat("x", 90<<10))
	for _, batch := range []platform.FlowBatch{
		{ID: "one", Signals: []platform.FlowSignal{{Key: "current", At: at, Value: platform.Raw(1)}}},
		{ID: "two", Predecessor: "one", Signals: []platform.FlowSignal{{Key: "late", At: at.Add(-time.Hour), Value: large}}},
	} {
		if _, err := tn.ConsumeFlowBatch(member, id, batch, at); err != nil {
			t.Fatal(err)
		}
	}
	for tick := range 3 {
		tn.Work(at.Add(time.Duration(tick+2) * time.Second))
	}
	active, _, err := platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	all, _, _ := platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{Archived: true})
	if err != nil || len(active) != 0 || len(all) != 1 || !all[0].Archived || all[0].Letter.Value != string(large) {
		t.Fatal("a legal 90 KiB asset could not archive past the paging target", len(active), len(all), err)
	}
}

func TestContinuousDeadLetterBacklogDoesNotStarveDueFlow(t *testing.T) {
	const tenant, id = "retention-with-due", "frames.window:source"
	at := time.Date(2026, 10, 11, 12, 0, 10, 0, time.UTC)
	source := &retentionWithDueSource{&retainedFrameSource{newFrameSource(tenant, 2<<20)}}
	tn := composeTenant(t, tenant, []Seat{seatOf("member", "frames:operator", "flow:admin")}, work.New(tenant), flow.New(tenant), source)
	decide(t, tn, "member", "frames", "frames.source.start", "frames.source", "source", map[string]any{}, at)
	tn.Work(at)
	due, ok := platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "frames.due:source")
	if !ok || due.State != "waiting" || len(due.Tokens) != 1 || !due.Tokens[0].Due.Equal(at.Add(time.Second)) {
		t.Fatal("fixture did not retain the ordinary due wait", due.State)
	}
	member := memberOf(t, tn, "member")
	if _, err := tn.ConsumeFlowBatch(member, id, platform.FlowBatch{ID: "one", Signals: []platform.FlowSignal{{Key: "current", At: at, Value: platform.Raw(1)}}}, at); err != nil {
		t.Fatal(err)
	}
	late := platform.FlowBatch{ID: "two", Predecessor: "one"}
	for i := range 6 {
		late.Signals = append(late.Signals, platform.FlowSignal{Key: fmt.Sprint(i), At: at.Add(-time.Hour), Value: platform.Raw(strings.Repeat("x", 20<<10))})
	}
	if _, err := tn.ConsumeFlowBatch(member, id, late, at); err != nil {
		t.Fatal(err)
	}
	tn.Work(at.Add(2 * time.Second))
	due, _ = platform.Get[flow.FlowInstance](tn.automation(flow.ID, false), "frames.due:source")
	remaining, _, err := platform.Find[platform.FlowDeadLetterAsset](tn.automation(flow.ID, false), platform.Query{})
	if due.State != "done" || err != nil || len(remaining) == 0 || len(remaining) >= 6 {
		t.Fatalf("due work did not advance alongside a bounded archive batch: state=%s remaining=%d refusal=%v", due.State, len(remaining), err)
	}
}
