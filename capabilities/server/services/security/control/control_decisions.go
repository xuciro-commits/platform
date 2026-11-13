package control

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type Decider func(platform.Caller, *pb.Submission, time.Time) (func(*pb.ChangeRecord), *kernel.Error)

type Deciders struct {
	Connector, Setting, Binding, Work, Notification, Endpoint, Effect, Translation Decider
}

func (d Deciders) Decide(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	switch s.GetTarget().GetType() {
	case ConnectorType:
		return d.Connector(c, s, at)
	case SettingType:
		return d.Setting(c, s, at)
	case ProtocolType:
		return d.Binding(c, s, at)
	case WorkType:
		return d.Work(c, s, at)
	case NotificationType:
		return d.Notification(c, s, at)
	case EndpointType:
		return d.Endpoint(c, s, at)
	case EffectType:
		return d.Effect(c, s, at)
	case TranslationType:
		return d.Translation(c, s, at)
	default:
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
}

type StagedControl struct {
	Tenant      string
	NoticeOwner func(id string) (string, bool)
	MarkRead    func(id string)
	Effect      func(id string) platform.Effect
	Cancel      func(current, after platform.Effect, out platform.Outcome)
}

func (s StagedControl) Decide(c platform.Caller, sub *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error, bool) {
	if c.App != PlatformApp || sub.GetAuthority() != PlatformApp || c.ID != sub.GetPrincipalId() || c.Tenant != s.Tenant || sub.GetTenantId() != s.Tenant {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the console may decide tenant controls"), true
	}
	if sub.GetTarget().GetType() == NotificationType {
		id := sub.GetTarget().GetId()
		owner, found := s.NoticeOwner(id)
		if !found {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, ""), true
		}
		if owner != c.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, ""), true
		}
		return func(*pb.ChangeRecord) { s.MarkRead(id) }, nil, true
	}
	if sub.GetTarget().GetType() == EffectType && sub.GetSchema().GetName() == SchemaEffectDiscard {
		current := s.Effect(sub.GetTarget().GetId())
		if current.Endpoint == acceptedformat.OperationEndpoint {
			if platform.EffectSettled(current.State) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, ""), true
			}
			return func(*pb.ChangeRecord) {
				after := current
				after.State, after.Error, after.Output = "discarded", "Cancelled", nil
				s.Cancel(current, after, platform.Outcome{Effect: current.ID, Result: "discarded", Detail: "Cancelled"})
			}, nil, true
		}
	}
	return nil, nil, false
}
