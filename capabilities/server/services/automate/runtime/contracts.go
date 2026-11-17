package automation

import (
	"crypto/sha256"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type FlowFrameStore interface {
	Read(platform.FlowStateArtifact) ([]byte, error)
	Seal(instance string, version int, raw []byte, budget int) (platform.FlowStateArtifact, error)
	Discard(platform.FlowStateArtifact) error
}

type ContinuousFlowOwner interface {
	platform.App
	PlanBatch(platform.FlowInstance, platform.Caller) (FlowBatchPreparation, *platform.Error)
	PlanComputeRetry(platform.FlowInstance, platform.Caller, time.Time) (FlowComputeRetryPreparation, *platform.Error)
	PlanArtifactCleanup(platform.FlowInstance, time.Time) (FlowArtifactCleanupPreparation, *platform.Error)
	PlanDeadLetterReplay(platform.FlowInstance, platform.Caller, platform.FlowDeadLetterAsset) (FlowDeadLetterReplayPreparation, *platform.Error)
	ExpandFrame(platform.FlowInstance, FlowFrameStore) (*platform.FlowBatchFrame, error)
}

type FlowProcesses interface {
	platform.Processes
	ContinuousFlowOwner
	InstallationDraft() FlowProcesses
}

type FlowDeadLetterReplayPreparation interface {
	Prepare(*pb.Submission, time.Time, FlowFrameStore) (FlowPreparedBatch, *platform.Error)
}

type FlowBatchPreparation interface {
	Intake() (string, *platform.StreamIntake, int)
	WindowTick(time.Time, FlowFrameStore) (platform.FlowBatch, bool, *platform.Error)
	Prepare(platform.FlowBatch, time.Time, FlowFrameStore) (FlowPreparedBatch, *platform.Error)
}

type FlowPreparedDecision interface {
	Decision(platform.App) platform.App
	Submission(tenant string) *pb.Submission
	Matches(platform.FlowInstance) bool
}

type FlowPreparedBatch interface {
	FlowPreparedDecision
	Artifact() platform.FlowStateArtifact
	Artifacts() []platform.FlowStateArtifact
	OperationRequest() (platform.OperationRequest, bool)
}

type FlowComputeRetryPreparation interface {
	Instance() string
	Prepare(time.Time, FlowFrameStore) (FlowPreparedComputeRetry, *platform.Error)
}

type FlowPreparedComputeRetry interface {
	FlowPreparedDecision
	Instance() string
	OperationRequest() platform.OperationRequest
}

type FlowArtifactCleanupPreparation interface {
	FlowPreparedDecision
	Instance() string
	Artifacts() []platform.FlowStateArtifact
}

func FlowBatchKey(instance string, version int, batch string) string {
	return fmt.Sprintf("flow-batch:%x", sha256.Sum256(platform.Raw([]any{instance, version, batch})))
}
