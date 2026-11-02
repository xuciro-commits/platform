package data

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
)

func ScheduleEntity(validate func(platform.Caller, *platform.Schedule) *kernel.Error) platform.Entity {
	return platform.Entity{Type: platform.ScheduleType, Title: "Schedule", Plural: "Schedules", Model: platform.Schedule{}, Display: "title",
		Description: "An independent time plan for pipeline builds.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{"builder": platform.ScopeTenant, "integrator": platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{"builder", "integrator"}, Capability: "integrations"},
		Validate: func(c platform.Caller, value any) *kernel.Error {
			schedule := value.(*platform.Schedule)
			every, err := time.ParseDuration(schedule.Every)
			if err != nil || every < time.Minute || every > 366*24*time.Hour {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A schedule runs every period between 1m and a year")
			}
			if !c.Replaying {
				schedule.Runner, schedule.RunnerScopes = c.ID, string(platform.Raw(c.Member.Scopes))
			}
			return validate(c, schedule)
		}}
}

func ScheduleActions() []platform.Action {
	return []platform.Action{{Schema: platform.SchemaScheduleRan, Target: platform.ScheduleType, Title: "Keep scheduled build", Description: "Retain the accepted time that requested a pipeline build.", Capability: "integrations", Roles: []string{"builder", "integrator"}, Payload: []platform.Field{{Name: "at", Type: "datetime", Required: true}}}}
}

func SubmitScheduleRan(ledger *platform.Ledger, c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			At time.Time `json:"at"`
		}
		schedule, ok := platform.Get[platform.Schedule](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The schedule run needs a retained build time")
		}
		schedule.LastRun = &payload.At
		return func(r *pb.ChangeRecord) { c.Put(r, schedule) }, nil
	})
}
