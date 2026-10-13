package platformserver

import (
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type migrationRetirement struct {
	caller                                   platform.Caller
	run, action, approval, redirect, release string
}

func (d *stagedDecision) stageActionRetirement(c platform.Caller, run platform.MigrationRun, step platform.MigrateStep) (platform.MigrationRun, *kernel.Error) {
	pending := []string{}
	if requests := d.records.types[platform.WorkApprovalType]; requests != nil {
		for id, value := range requests.rows {
			request := value.value.Interface().(platform.ApprovalRequest)
			if request.State == "pending" && request.Action == step.Action && id > run.Cursor {
				pending = append(pending, id)
			}
		}
	}
	slices.Sort(pending)
	for _, id := range pending[:min(len(pending), step.Batch)] {
		d.retirements = append(d.retirements, migrationRetirement{caller: c, run: run.ID, action: step.Action, approval: id, redirect: step.Redirect, release: run.Plan.To})
		run.Cursor, run.Processed = id, run.Processed+1
	}
	run.State = "running"
	if len(pending) <= step.Batch {
		run.Step++
		run.Cursor = ""
		if run.Step == len(run.Plan.Steps) {
			run.State = "complete"
		}
	}
	return run, nil
}

func (d *stagedDecision) CheckApprovalRetirement(c platform.Caller, migration, approval string) (platform.ApprovalRetirement, *kernel.Error) {
	for _, directive := range d.retirements {
		if directive.run == migration && directive.approval == approval && c.App == platform.WorkApp && c.Automation && c.ID == "app:"+platform.WorkApp {
			return platform.ApprovalRetirement{Actor: directive.caller.ID, Redirect: directive.redirect, Release: directive.release}, nil
		}
	}
	return platform.ApprovalRetirement{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval retirement has no matching private migration directive")
}

// Parent ledgers have been released. Work owns each rejection; the held
// object's owner supplies its original rejected transition as a child receipt.
func (d *stagedDecision) finishMigrationRetirements(at time.Time) *kernel.Error {
	for _, directive := range d.retirements {
		owner, ok := d.tenant.app(platform.WorkApp).(platform.ApprovalRetirementOwner)
		if !ok {
			return migrationRefusal(fmt.Errorf("the approval retirement owner is unavailable"))
		}
		private, ok := d.stateApp(owner).(platform.ApprovalRetirementOwner)
		if !ok {
			return migrationRefusal(fmt.Errorf("the approval retirement owner cannot stage its records"))
		}
		if err := private.RetireApproval(directive.caller, directive.run, directive.approval, at); err != nil {
			return err
		}
	}
	return d.failure
}

func (s *recordStore) retiredApprovalReturnLocked(info platform.EntityInfo, schema string) bool {
	runs := s.types[platform.MigrationRunType]
	if runs == nil || !strings.HasSuffix(schema, platform.ApprovalRejected) && !strings.HasSuffix(schema, platform.ApprovalReturned) {
		return false
	}
	action := strings.TrimSuffix(strings.TrimSuffix(schema, platform.ApprovalRejected), platform.ApprovalReturned)
	for _, value := range runs.rows {
		run := value.value.Interface().(platform.MigrationRun)
		if run.State != "cancelled" && run.Plan.From == s.readRelease && slices.ContainsFunc(run.Plan.Steps, func(step platform.MigrateStep) bool {
			return step.Kind == "retire-action" && step.Action == action && slices.Contains(step.Types, info.Type)
		}) {
			return true
		}
	}
	return false
}
