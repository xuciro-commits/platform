package platformserver

import (
	"encoding/json"
	"time"

	"platformserver/platform"
	"platformserver/studio/build"
)

// A command that cannot be safely repeated first accepts an unknown outcome.
// A crash after that append cannot turn the original pending effect into a
// second physical action. The live attempt may still record its actual answer.
func (t *Tenant) beginDispatchAttempt(x platform.Effect, at time.Time) bool {
	if x.App != build.ID || x.Event != build.ID+"/dispatch" {
		return true
	}
	var request build.WritebackRequest
	if json.Unmarshal([]byte(x.Body), &request) != nil || request.Dispatch == "" {
		return false
	}
	if request.SafeRetry {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.businessStopped() {
		return false
	}
	const reason = "Dispatch attempt started; awaiting delivery evidence"
	t.settleAccepted(x.ID, platform.Outcome{Effect: x.ID, Result: "unknown", Detail: reason}, nil, at)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, current := range t.outbound {
		if current.ID == x.ID && current.State == "unknown" && current.Attempts == x.Attempts+1 && current.Error == reason && current.CallbackError == "" {
			current.sending = true
			return true
		}
	}
	return false
}
