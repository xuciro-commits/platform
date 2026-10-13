package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
)

// WorkHost provides the original approval/task owner's actual collaborators.
type WorkHost interface {
	Action(string) (string, platform.Action, bool)
	Member(string) (platform.Member, bool)
	Caller(platform.Caller, platform.Member, string) platform.Caller
	Automation(platform.Caller, string) platform.Caller
	Holding(string, string) []string
	Calendar(string, platform.Date) platform.Calendar
	Units(string, string, platform.Date) []string
	Holders(string, string, string, platform.Date) []string
	Recipients(platform.Caller, time.Time, []platform.Recipient) []string
	Attempt(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	ActiveRelease() string
	ApprovalExecution(platform.Caller, platform.ApprovalRequest) (WorkHost, *kernel.Error)
	Seen(platform.Caller, ...string)
}

type AttachedWork interface{ AttachWork(WorkHost) }
