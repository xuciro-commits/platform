package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
	"slices"
	"time"
)

type creator interface {
	businessRuntime() platform.TransactionExecutor
}

func lifecycle(o Object, roles []string, creates creator, lookup func(string) (platform.EntityInfo, bool)) *platform.Lifecycle {
	if len(o.States) == 0 {
		return nil
	}
	l := &platform.Lifecycle{Field: "state", Initial: o.States[0].Name}
	for _, s := range o.States {
		l.States = append(l.States, platform.State{Name: s.Name, Title: s.Title, Tone: s.Tone, Description: s.Description})
	}
	for _, a := range o.Actions {
		payload := []platform.Field{}
		for _, in := range a.Inputs {
			payload = append(payload, in.Field())
		}
		action := a
		takers := slices.Clone(roles)
		if len(a.Roles) > 0 {
			takers = append([]string{Builder}, a.Roles...)
		}
		var approval *platform.Approval
		if a.Approval != nil {
			approval = &platform.Approval{Pending: a.Approval.Pending, Rejected: a.Approval.Rejected}
			for _, level := range a.Approval.Levels {
				approval.Levels = append(approval.Levels, platform.ApprovalLevel{Title: level.Title, AppRole: level.Role, All: level.All})
			}
		}
		criteria, _ := actionCriteria(o, a, lookup)
		description := a.Description
		if description == "" {
			description = "Apply this business transaction."
		}
		l.Transitions = append(l.Transitions, platform.Transition{ConfirmationOnly: a.ConfirmationOnly, Adoption: a.Adoption, Criteria: criteria, Name: a.Name, Title: a.Title, Description: description,
			From: slices.Clone(a.From), To: slices.Clone(a.From), Roles: takers, Capability: o.Name, Payload: payload, Approval: approval, DeferRequired: true, Txn: a.Transaction,
			ApplyTxn: func(c platform.Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error {
				if creates == nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business transactions need their original owner")
				}
				return creates.businessRuntime().Transact(c, r, action.Transaction, now)
			}})
	}
	return l
}

func assign(f reflect.Value, x any) error {
	if !f.IsValid() {
		return fmt.Errorf("no such field")
	}
	raw, err := json.Marshal(x)
	if err != nil {
		return err
	}
	target := reflect.New(f.Type())
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return fmt.Errorf("%v does not fit a %s", x, f.Type())
	}
	f.Set(target.Elem())
	return nil
}
