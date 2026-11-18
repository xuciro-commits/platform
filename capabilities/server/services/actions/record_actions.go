package actions

import (
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
)

func transitionRefusal(c platform.Caller, action platform.ActionType, id, state string, archived bool) *platform.Error {
	if archived {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is archived", id)
	}
	pending := action.Approval != nil && action.Approval.Pending != "" && state == action.Approval.Pending && !c.Automation && !c.Probing()
	if !pending && !action.Criteria.State.Holds(state, c.Rules()) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is {state}; {transition} takes it only from {states}", id, state, action.Name, strings.Join(action.Criteria.State.In, ", "))
	}
	return nil
}

func RecordActions(owner platform.Service, c platform.Caller, info platform.RecordTypeInfo, record any, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool), say func(string) string) []objects.RecordAction {
	if owner == nil || reflect.TypeOf(record) != info.Go {
		return nil
	}
	manifest := owner.Manifest()
	value := reflect.ValueOf(record)
	head := value.Field(0).Interface().(platform.Record)
	var life *platform.Lifecycle
	for _, entity := range manifest.Entities {
		if entity.Type == info.Type {
			life = entity.Lifecycle
			break
		}
	}
	var out []objects.RecordAction
	for _, entry := range manifest.Actions.All() {
		if entry.Target != info.Type || entry.New || entry.ConfirmationOnly {
			continue
		}
		sub := &pb.Submission{Target: &pb.EntityRef{Type: info.Type, Id: head.ID}, Schema: &pb.SchemaRef{Name: entry.Schema}}
		if !manifest.Actions.DecideSubmission(c, sub, now).Allow {
			continue
		}
		item := objects.RecordAction{Schema: entry.Schema, Allowed: true}
		if life != nil {
			i := slices.IndexFunc(life.Actions, func(action platform.ActionType) bool { return info.Type+"."+action.Name == entry.Schema })
			if i >= 0 {
				stateRead := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: info.Type, Record: head.ID, Fields: []string{life.Field}}, now)
				if !stateRead.Allow || len(stateRead.Withheld) > 0 {
					continue
				}
				action := life.Actions[i]
				if refusal := transitionRefusal(c, action, head.ID, records.BusinessField(info, value, life.Field), head.Archived); refusal != nil {
					item.Allowed, item.Reason = false, say(refusal.Message)
				} else if plan := action.Criteria.Business; plan != nil && len(plan.Reads.Inputs) == 0 {
					for _, guard := range plan.Conditions {
						allowed, refusal := protocol.GuardSatisfied(guard.Expr, plan.Reads, c, record, nil, now, lookup)
						if refusal != nil {
							item.Allowed = false
							break
						}
						if !allowed {
							item.Allowed, item.Reason = false, say(guard.Message)
							break
						}
					}
					if !item.Allowed && item.Reason == "" {
						continue
					}
				}
			}
		}
		out = append(out, item)
	}
	return out
}
