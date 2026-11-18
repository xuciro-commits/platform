package automation

import (
	"crypto/sha256"
	"fmt"
	"platformserver/kernel/protocol"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type FlowFrameStore interface {
	Read(protocol.FlowStateArtifact) ([]byte, error)
	Seal(instance string, version int, raw []byte, budget int) (protocol.FlowStateArtifact, error)
	Discard(protocol.FlowStateArtifact) error
}

type ContinuousFlowOwner interface {
	platform.App
	PlanBatch(protocol.FlowInstance, platform.Caller) (FlowBatchPreparation, *platform.Error)
	PlanComputeRetry(protocol.FlowInstance, platform.Caller, time.Time) (FlowComputeRetryPreparation, *platform.Error)
	PlanArtifactCleanup(protocol.FlowInstance, time.Time) (FlowArtifactCleanupPreparation, *platform.Error)
	PlanDeadLetterReplay(protocol.FlowInstance, platform.Caller, protocol.FlowDeadLetterAsset) (FlowDeadLetterReplayPreparation, *platform.Error)
	ExpandFrame(protocol.FlowInstance, FlowFrameStore) (*protocol.FlowBatchFrame, error)
}

type FlowProcesses interface {
	protocol.Processes
	platform.NativeFlowAssets
	ContinuousFlowOwner
	InstallationDraft() FlowProcesses
	HasDefinition(string, string, int) bool
}

type FlowDeadLetterReplayPreparation interface {
	Prepare(*pb.Submission, time.Time, FlowFrameStore) (FlowPreparedBatch, *platform.Error)
}

type FlowBatchPreparation interface {
	Intake() (string, *platform.StreamIntake, int)
	WindowTick(time.Time, FlowFrameStore) (protocol.FlowBatch, bool, *platform.Error)
	Prepare(protocol.FlowBatch, time.Time, FlowFrameStore) (FlowPreparedBatch, *platform.Error)
}

type FlowPreparedDecision interface {
	Decision(platform.App) platform.App
	Submission(tenant string) *pb.Submission
	Matches(protocol.FlowInstance) bool
}

type FlowPreparedBatch interface {
	FlowPreparedDecision
	Artifact() protocol.FlowStateArtifact
	Artifacts() []protocol.FlowStateArtifact
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
	Artifacts() []protocol.FlowStateArtifact
}

func FlowBatchKey(instance string, version int, batch string) string {
	return fmt.Sprintf("flow-batch:%x", sha256.Sum256(platform.Raw([]any{instance, version, batch})))
}
