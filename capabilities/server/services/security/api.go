package security

import (
	"platformserver/kernel/records"
	"platformserver/platform"
	"time"
)

func (a Authorizer) RecordSet(store *records.Store, c platform.Caller, at time.Time) func(platform.AuthzRequest) platform.AuthzDecision {
	walk := NewTraversal()
	return func(q platform.AuthzRequest) platform.AuthzDecision {
		return a.Authorize(store, c, q, at, true, 0, walk)
	}
}
