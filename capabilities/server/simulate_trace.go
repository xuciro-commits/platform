package platformserver

import "platformserver/platform"

func (r runtime) TraceEnabled() bool { return r.t.simulationTrace != nil }
func (r runtime) RecordDecisionTrace(event platform.DecisionTrace) {
	if r.t.simulationTrace != nil {
		r.t.simulationTrace(event)
	}
}
func (d *stagedDecision) TraceEnabled() bool { return d.tenant.simulationTrace != nil }
func (d *stagedDecision) RecordDecisionTrace(event platform.DecisionTrace) {
	if d.tenant.simulationTrace != nil {
		d.tenant.simulationTrace(event)
	}
}

func (r runtime) Simulating() bool         { return r.t.simulation }
func (d *stagedDecision) Simulating() bool { return d.simulation || d.tenant.simulation }
