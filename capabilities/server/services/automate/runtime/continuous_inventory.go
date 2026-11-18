package runtime

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

type ContinuousRetry struct {
	Plan   FlowComputeRetryPreparation
	Member platform.Member
}

type ContinuousCleanup struct {
	Plan     FlowArtifactCleanupPreparation
	Instance string
	Refusal  *platform.Error
}

type ContinuousInventory struct {
	Owner           ContinuousFlowOwner
	Caller          platform.Caller
	Member          func(id string) (platform.Member, bool)
	StreamPublished func(record string) bool
}

type ContinuousWork struct {
	Pending  []protocol.FlowInstance
	Retries  []ContinuousRetry
	Cleanups []ContinuousCleanup
}

func (i ContinuousInventory) Scan(now time.Time) (ContinuousWork, *platform.Error) {
	var work ContinuousWork
	instances, total, queryErr := platform.Find[protocol.FlowInstance](i.Caller, platform.Query{Sort: []string{"id"}})
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

func ReplayBatch(f ContinuousFlowOwner, tenant string, before FlowInstance, batch protocol.FlowBatch) (protocol.FlowBatchOutcome, bool, *platform.Error) {
	digest, err := json.Marshal(batch)
	if err != nil {
		return protocol.FlowBatchOutcome{}, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A batch needs valid JSON signal values")
	}
	batchDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(digest))
	key := FlowBatchKey(before.ID, before.Version, batch.ID)
	prior := f.Authority().AcceptedFor(tenant, key)
	if prior == nil {
		return protocol.FlowBatchOutcome{}, false, nil
	}
	var saved struct{ Batch, Digest string }
	if json.Unmarshal(prior.GetSubmission().GetPayload(), &saved) != nil || saved.Batch != batch.ID || saved.Digest != batchDigest {
		return protocol.FlowBatchOutcome{}, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The batch identity was reused with different content")
	}
	return protocol.FlowBatchOutcome{Cursor: batch.ID}, true, nil
}

func RetireArtifacts(plan FlowArtifactCleanupPreparation, tenant string, now time.Time, discard func(protocol.FlowStateArtifact) error) *platform.Error {
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

func DrainSource(rounds int, consume func() (protocol.FlowBatchOutcome, *platform.Error)) *platform.Error {
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

type SourcePull struct {
	Stopped   func() bool
	Inventory func(now time.Time) (ContinuousWork, *platform.Error)
	Retire    func(plan FlowArtifactCleanupPreparation, now time.Time) *platform.Error
	Retry     func(member platform.Member, plan FlowComputeRetryPreparation, now time.Time)
	Member    func(id string) (platform.Member, *platform.Error)
	Consume   func(member platform.Member, id string, now time.Time) (protocol.FlowBatchOutcome, *platform.Error)
	Collect   func(now time.Time) error
}

func (p SourcePull) Run(now time.Time) {
	if p.Stopped() {
		return
	}
	work, queryErr := p.Inventory(now)
	if queryErr != nil {
		log.Printf("continuous Flow inventory unavailable; no artifacts retired")
		return
	}
	for _, cleanup := range work.Cleanups {
		if cleanup.Refusal != nil {
			log.Printf("continuous Flow artifact cleanup %s refused: %s", cleanup.Instance, cleanup.Refusal.Code)
			continue
		}
		if refusal := p.Retire(cleanup.Plan, now); refusal != nil {
			log.Printf("continuous Flow artifact cleanup %s refused: %s", cleanup.Instance, refusal.Code)
		}
	}
	for _, retry := range work.Retries {
		p.Retry(retry.Member, retry.Plan, now)
	}
	for _, instance := range work.Pending {
		member, refusal := p.Member(instance.OnBehalf)
		if refusal != nil {
			continue
		}
		if refusal := DrainSource(8, func() (protocol.FlowBatchOutcome, *platform.Error) {
			return p.Consume(member, instance.ID, now)
		}); refusal != nil {
			log.Printf("continuous source intake %s refused: %s", instance.ID, refusal.Code)
		}
	}
	if err := p.Collect(now); err != nil {
		log.Printf("continuous Flow artifact collection deferred: %v", err)
	}
}

type ReplayPreflight struct {
	Versioned bool
	Version   func() (*platform.SchemaVersion, *platform.Error)
	Current   func(sub *pb.Submission) *pb.Submission
	Settled   func() bool
	Accept    func(sub *pb.Submission, binding *platform.SchemaVersion, schemaRefusal *platform.Error) (*pb.ChangeRecord, *platform.Error)
}

type ReplayPreflightResult struct {
	Sub     *pb.Submission
	Binding *platform.SchemaVersion
	Record  *pb.ChangeRecord
	Handled bool
}

func (p ReplayPreflight) Run(sub *pb.Submission) (ReplayPreflightResult, *platform.Error) {
	value := ReplayPreflightResult{Sub: sub}
	var refusal *platform.Error
	if p.Versioned {
		value.Binding, refusal = p.Version()
		if refusal != nil && refusal.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
			return value, refusal
		}
	} else {
		value.Sub = p.Current(sub)
	}
	if refusal != nil || p.Settled() {
		value.Handled = true
		value.Record, refusal = p.Accept(value.Sub, value.Binding, refusal)
		return value, refusal
	}
	return value, nil
}

type FlowSnapshot struct {
	Tenant      string
	Quarantined func() bool
	Member      func(id string) (platform.Member, bool)
	Admits      func(platform.Member) *platform.Error
	Owner       func() (ContinuousFlowOwner, bool)
	Record      func(m platform.Member, id string) (any, *platform.Error)
}

func (s FlowSnapshot) Take(m platform.Member, id string) (ContinuousFlowOwner, protocol.FlowInstance, platform.Member, *platform.Error) {
	if s.Quarantined() || m.Tenant != s.Tenant {
		return nil, protocol.FlowInstance{}, m, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The Flow member is unavailable")
	}
	current, ok := s.Member(m.ID)
	current.Scopes = slices.Clone(m.Scopes)
	if !ok || s.Admits(current) != nil {
		return nil, protocol.FlowInstance{}, m, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The Flow member is unavailable")
	}
	f, ok := s.Owner()
	if !ok {
		return nil, protocol.FlowInstance{}, current, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	record, refusal := s.Record(current, id)
	if refusal != nil {
		return nil, protocol.FlowInstance{}, current, refusal
	}
	var x protocol.FlowInstance
	switch value := record.(type) {
	case protocol.FlowInstance:
		x = value
	default:
		return nil, protocol.FlowInstance{}, current, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A source batch acts only for its original Flow member")
	}
	if x.Withheld || x.OnBehalf != current.ID {
		return nil, protocol.FlowInstance{}, current, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A source batch acts only for its original Flow member")
	}
	return f, x, current, nil
}

type ReplayContext struct {
	Asset    protocol.FlowDeadLetterAsset
	Member   platform.Member
	Instance protocol.FlowInstance
}

type DeadLetterLoad struct {
	Record   func(id string) (any, *platform.Error)
	Snapshot func(instance string) (protocol.FlowInstance, platform.Member, *platform.Error)
}

func (l DeadLetterLoad) Load(id string) (ReplayContext, *platform.Error) {
	record, refusal := l.Record(id)
	if refusal != nil {
		return ReplayContext{}, refusal
	}
	var asset protocol.FlowDeadLetterAsset
	switch value := record.(type) {
	case protocol.FlowDeadLetterAsset:
		asset = value
	default:
		return ReplayContext{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dead letter's protected source is unavailable")
	}
	if asset.Withheld {
		return ReplayContext{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dead letter's protected source is unavailable")
	}
	if asset.Action != "" {
		return ReplayContext{Asset: asset}, nil
	}
	instance, current, refusal := l.Snapshot(asset.Instance)
	return ReplayContext{asset, current, instance}, refusal
}
