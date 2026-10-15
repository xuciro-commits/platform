package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
)

// Confirmation transport composes the original declaration compiler, outbox
// and accepted caller. It supplies no independent decision or scheduler.
type ConfirmationHost interface {
	Holding(string, string) []string
	ResolveConfirmationDelivery(platform.Caller, platform.ConfirmationRun, int, bool, time.Time) *kernel.Error
	FreezeConfirmation(platform.Caller, string, platform.Confirmation, time.Time) (string, []platform.ReleaseAsset, error)
	QueueConfirmation(platform.Caller, *pb.ChangeRecord, platform.ConfirmationRun, int, bool, time.Time) *kernel.Error
	CompleteConfirmation(platform.Caller, *pb.ChangeRecord, platform.ConfirmationRun, string, time.Time) (string, *kernel.Error)
}
