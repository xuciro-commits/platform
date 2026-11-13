package automation

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"strings"
	"time"
)

type ContinuousRetry struct {
	Plan   platform.FlowComputeRetryPreparation
	Member platform.Member
}

type ContinuousCleanup struct {
	Plan     platform.FlowArtifactCleanupPreparation
	Instance string
	Refusal  *kernel.Error
}

type ContinuousInventory struct {
	Owner           platform.ContinuousFlowOwner
	Caller          platform.Caller
	Member          func(id string) (platform.Member, bool)
	StreamPublished func(record string) bool
}

type ContinuousWork struct {
	Pending  []platform.FlowInstance
	Retries  []ContinuousRetry
	Cleanups []ContinuousCleanup
}

func (i ContinuousInventory) Scan(now time.Time) (ContinuousWork, *kernel.Error) {
	var work ContinuousWork
	instances, total, queryErr := platform.Find[platform.FlowInstance](i.Caller, platform.Query{Sort: []string{"id"}})
	if queryErr != nil {
		return work, queryErr
	}
	if total != len(instances) {
		return work, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Continuous Flow inventory is incomplete")
	}
	if i.Owner == nil {
		return work, nil
	}
	for _, instance := range instances {
		if instance.CleanupPending {
			plan, err := i.Owner.PlanArtifactCleanup(instance, now)
			if err != nil {
				work.Cleanups = append(work.Cleanups, ContinuousCleanup{Instance: instance.ID, Refusal: err})
			} else {
				work.Cleanups = append(work.Cleanups, ContinuousCleanup{Plan: plan, Instance: instance.ID})
			}
			continue
		}
		if instance.State != "waiting" {
			continue
		}
		member, exists := i.Member(instance.OnBehalf)
		if !exists {
			continue
		}
		if plan, err := i.Owner.PlanComputeRetry(instance, i.Caller, now); err == nil {
			work.Retries = append(work.Retries, ContinuousRetry{Plan: plan, Member: member})
			continue
		}
		if plan, err := i.Owner.PlanBatch(instance, i.Caller); err == nil {
			if _, intake, _ := plan.Intake(); intake != nil && i.StreamPublished(intake.SourceRecord) {
				work.Pending = append(work.Pending, instance)
			}
		}
	}
	return work, nil
}

func ReplayBatch(f platform.ContinuousFlowOwner, tenant string, before FlowInstance, batch platform.FlowBatch) (platform.FlowBatchOutcome, bool, *kernel.Error) {
	digest, err := json.Marshal(batch)
	if err != nil {
		return platform.FlowBatchOutcome{}, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A batch needs valid JSON signal values")
	}
	batchDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(digest))
	key := platform.FlowBatchKey(before.ID, before.Version, batch.ID)
	prior := f.AcceptedLedger().AcceptedFor(tenant, key)
	if prior == nil {
		return platform.FlowBatchOutcome{}, false, nil
	}
	var saved struct{ Batch, Digest string }
	if json.Unmarshal(prior.GetSubmission().GetPayload(), &saved) != nil || saved.Batch != batch.ID || saved.Digest != batchDigest {
		return platform.FlowBatchOutcome{}, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The batch identity was reused with different content")
	}
	return platform.FlowBatchOutcome{Cursor: batch.ID}, true, nil
}

func RetireArtifacts(plan platform.FlowArtifactCleanupPreparation, tenant string, now time.Time, discard func(platform.FlowStateArtifact) error) *kernel.Error {
	if plan == nil || plan.Instance() == "" || now.IsZero() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Flow artifact cleanup is unavailable")
	}
	refs := plan.Artifacts()
	if len(refs) == 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow artifact cleanup intent is empty")
	}
	for _, ref := range refs {
		if ref.Tenant != tenant {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A retired Flow artifact belongs to another tenant")
		}
		if err := discard(ref); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A superseded Flow artifact could not be retired: "+err.Error())
		}
	}
	return nil
}

func DrainSource(rounds int, consume func() (platform.FlowBatchOutcome, *kernel.Error)) *kernel.Error {
	for round := 0; round < rounds; round++ {
		outcome, refusal := consume()
		if refusal != nil {
			return refusal
		}
		if outcome.Cursor == "" || strings.HasPrefix(outcome.Cursor, "tick:") {
			return nil
		}
	}
	return nil
}
