package build

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// flowOf binds the original host's authorized reads to a pure native-flow
// compilation. The compiler cannot resolve members, records or queries itself.
func (b *Build) flowOf(p construction.Process) platform.Flow {
	execution := construction.ProcessExecution{
		StateField: "state",
		ContinuousEnabled: func(c platform.Caller) bool {
			if b.host == nil || p.ID == "" || p.Version < 1 {
				return false
			}
			current, ok := platform.Get[construction.Process](b.host.Automation(c, construction.ID), p.ID)
			return ok && current.State == "published" && current.Version == p.Version && current.Name == p.Name
		},
		Subject: func(c platform.Caller, r *platform.Run) (json.RawMessage, *kernel.Error) {
			member, ok := b.host.Member(r.OnBehalf)
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			raw, err := b.host.Caller(c, member, construction.ID).ReadRecord(p.Object, r.Key, r.Now)
			if err == nil {
				var fields map[string]json.RawMessage
				info, known := b.lookupEntity(p.Object)
				if known && json.Unmarshal(raw, &fields) == nil {
					for name := range fields {
						if _, declared := info.Field(name); declared {
							r.Sources = append(r.Sources, p.Object+"/"+r.Key+"#"+name)
						}
					}
				}
			}
			return raw, err
		},
		SubjectPath: func(c platform.Caller, r *platform.Run, path []string) (json.RawMessage, []string, *kernel.Error) {
			member, known := b.host.Member(r.OnBehalf)
			if !known {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			return b.host.Caller(c, member, construction.ID).ReadRecordPath(p.Object, r.Key, path, r.Now)
		},
		Query: func(c platform.Caller, r *platform.Run, app, name string, version int, input json.RawMessage) (json.RawMessage, *kernel.Error) {
			member, ok := b.host.Member(r.OnBehalf)
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The initiating member is no longer available")
			}
			return b.host.Caller(c, member, app).ReadQueryVersion(app, name, version, input, r.Now)
		},
	}
	if p.Every == "" && !(p.Continuous != nil && p.Continuous.Intake != nil) && !p.Manual {
		if info, ok := b.lookupEntity(p.Object); ok && info.Lifecycle != nil {
			execution.StateField = info.Lifecycle.Field
		}
	}
	return construction.CompileProcess(p, execution)
}
