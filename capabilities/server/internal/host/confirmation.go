package host

import "platformserver/platform"

// The caller provides the private transport; the attached owner also reads
// current reviewer membership. There is no independent decision or scheduler.
type ConfirmationHost interface {
	platform.ConfirmationTransport
	Holding(string, string) []string
}
