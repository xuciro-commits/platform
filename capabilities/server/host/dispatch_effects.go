package host

import (
	"encoding/json"
	dataowner "platformserver/services/data"
	"time"

	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) beginDispatchAttempt(x platform.Effect, at time.Time) bool {
	if x.App != build.ID || x.Event != build.ID+"/dispatch" {
		return true
	}
	var request dataowner.WritebackRequest
	if json.Unmarshal([]byte(x.Body), &request) != nil || request.Dispatch == "" {
		return false
	}
	if request.SafeRetry {
		return true
	}
	if t.businessStopped() {
		return false
	}
	const reason = "Dispatch attempt started; awaiting delivery evidence"
	t.settleWithUsage(x.ID, platform.Outcome{Effect: x.ID, Result: "unknown", Detail: reason}, nil, at)
	claimed := false
	t.outbound.Update(x.ID, func(current *actions.Queued) {
		if current.State == "unknown" && current.Attempts == x.Attempts+1 && current.Error == reason && current.CallbackError == "" {
			current.Sending, claimed = true, true
		}
	})
	return claimed
}
