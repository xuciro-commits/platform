package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
)

// FlowHost is the runtime surface consumed by the original Flow owner.
// Declarations, member reads and execution collaborators remain explicit;
// an instance cannot access unrelated installation or host management APIs.
type FlowHost interface {
	Automation(platform.Caller, string) platform.Caller
	Member(string) (platform.Member, bool)
	Record(string) (any, bool)
	Declares(string) bool
	Calendar(string, platform.Date) platform.Calendar
	Tasks() Tasks
	Runs() Runs
	BindFlow(string, string, int, FlowBinding) (FlowBinding, error)
	Invoke(platform.Caller, string, string, string, []byte, string, string, time.Time) (*pb.EntityRef, *kernel.Error)
	Action(string) (string, platform.Action, bool)
	Caller(platform.Caller, platform.Member, string) platform.Caller
	OwnerOf(string) (string, bool)
	Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
}

type AttachedFlow interface{ AttachFlow(FlowHost) }
