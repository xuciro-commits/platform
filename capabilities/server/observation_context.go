package platformserver

import "platformserver/platform"

func (d *stagedDecision) ObservationContext(c platform.Caller) *platform.ObservationContext {
	evidence := d.observationInput
	if evidence == nil || c.App != evidence.Definition.Ref.App || c.Tenant != d.tenant.ID {
		return nil
	}
	proof := &platform.ObservationContext{Definition: evidence.Definition, Record: evidence.Record}
	if evidence.Intervals != nil {
		copy := evidence.Intervals.Clone()
		proof.Intervals = &copy
	}
	return proof
}
