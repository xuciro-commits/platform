package platformserver

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The probe writes through an ordinary application action and accepted ledger.
// Its retained Flow is the production window/Wasm/threshold/native-action path.
type nativeProbeRecord struct {
	platform.Record
	Blocked bool    `json:"blocked"`
	Reading float64 `json:"reading"`
	Applied int     `json:"applied"`
}

type nativeFrameSource struct{ *frameSource }

func newNativeFrameSource(tenant string) *nativeFrameSource {
	base := newFrameSource(tenant, 1<<20)
	base.ledger = platform.NewLedger(tenant, "frames", platform.NewCatalog(
		platform.Action{Schema: "frames.source.start", Target: "frames.source", Title: "Start", Description: "Start the retained native stream", Roles: []string{"operator"}, Payload: []platform.Field{}},
		platform.Action{Schema: "frames.probe.create", Target: "frames.probe", Title: "Create probe", Description: "Create one source-bound probe", New: true, Roles: []string{"operator"}, Payload: []platform.Field{{Name: "blocked", Type: "boolean"}}},
		platform.Action{Schema: "frames.probe.unblock", Target: "frames.probe", Title: "Unblock probe", Description: "Permit the original rejected reading", Roles: []string{"operator"}, Payload: []platform.Field{}},
		platform.Action{Schema: "frames.probe.accept", Target: "frames.probe", Title: "Accept reading", Description: "Accept one original stream reading", Roles: []string{"operator"}, Payload: []platform.Field{{Name: "reading", Type: "number", Required: true}}}), "frames.source", "frames.probe")
	return &nativeFrameSource{frameSource: base}
}

func (s *nativeFrameSource) Manifest() platform.Manifest {
	m := s.frameSource.Manifest()
	m.Entities = append(m.Entities, platform.Entity{Type: "frames.probe", Title: "Probe", Model: nativeProbeRecord{}})
	fl := &m.Flows[0]
	fl.Continuous.Entry = "alertsloop"
	fl.Continuous.Concurrency = 8
	fl.Continuous.CheckpointEvery = 2
	fl.Continuous.DeadLetterMaxRecords = 16
	fl.Continuous.ErrorPolicy, fl.Continuous.DeadLetterRetry = "deadLetter", true
	fl.Continuous.Aggregate = &platform.StreamAggregate{Node: "aggregate", Kernel: "wasm:f32-window-stats", Signal: "reading", Group: []string{"probeId"}, Measures: []string{"count", "sum", "mean", "min", "max", "above"}, Above: 11.5}
	fl.Continuous.Threshold = &platform.StreamThreshold{Node: "threshold", Field: "mean", High: 11.5, Low: 9.5}
	fl.Continuous.Output = &platform.StreamOutput{Node: "output", Name: "stats", Mode: "replace"}
	fl.Steps = []platform.Step{
		{Name: "alertsloop", Next: "completed", Loop: &platform.Loop{Body: "accept", MaxIterations: 1000, Concurrency: 1, Items: func(_ platform.Caller, r *platform.Run) ([]json.RawMessage, *kernel.Error) {
			var input struct {
				Triggered []json.RawMessage `json:"triggered"`
			}
			if json.Unmarshal(r.Data, &input) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "invalid alert projection")
			}
			return input.Triggered, nil
		}}},
		{Name: "accept", Fault: "rejected", Retry: &platform.RetryPolicy{Attempts: 1}, Act: &platform.Act{Action: "frames.probe.accept", AsMember: true,
			TargetValue: func(_ platform.Caller, r *platform.Run) (string, *kernel.Error) {
				var item map[string]any
				json.Unmarshal(r.Frames[len(r.Frames)-1].Item, &item)
				id, _ := item["probeId"].(string)
				return id, nil
			},
			Inputs: func(_ platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
				var item map[string]any
				json.Unmarshal(r.Frames[len(r.Frames)-1].Item, &item)
				return platform.Raw(map[string]any{"reading": item["reading"]}), nil
			}}},
		{Name: "rejected", End: true}, {Name: "completed", End: true},
	}
	return m
}

func (s *nativeFrameSource) Submit(c platform.Caller, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if sub.GetSchema().GetName() == "frames.source.start" {
		return s.frameSource.Submit(c, sub, at)
	}
	return s.ledger.Receive(c, sub, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		x, known := platform.Get[nativeProbeRecord](c, sub.GetTarget().GetId())
		switch sub.GetSchema().GetName() {
		case "frames.probe.create":
			if known {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "probe exists")
			}
			var p struct{ Blocked bool }
			json.Unmarshal(sub.GetPayload(), &p)
			x = nativeProbeRecord{Record: platform.Record{ID: sub.GetTarget().GetId()}, Blocked: p.Blocked}
		case "frames.probe.unblock":
			if !known {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "no probe")
			}
			x.Blocked = false
		case "frames.probe.accept":
			if !known || x.Blocked {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "probe rejects reading")
			}
			var p struct{ Reading float64 }
			json.Unmarshal(sub.GetPayload(), &p)
			x.Reading = p.Reading
			x.Applied++
		default:
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "unknown probe action")
		}
		return func(r *pb.ChangeRecord) { c.Put(r, x) }, nil
	})
}
