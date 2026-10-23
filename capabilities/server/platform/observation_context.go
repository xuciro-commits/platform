package platform

import "strings"

// ObservationContext is supplied only by the original private acceptance view.
// JSON inputs and callers cannot manufacture these rule facts.
type ObservationContext struct {
	Definition AssetBinding
	Record     string
	Intervals  *ObservationIntervalResult
}
type ObservationContextRuntime interface {
	ObservationContext(Caller) *ObservationContext
}

func (c Caller) ObservationContext() *ObservationContext {
	if c.Replaying || !c.Staging() {
		return nil
	}
	if runtime, ok := c.rt.(ObservationContextRuntime); ok {
		return runtime.ObservationContext(c)
	}
	return nil
}
func (c Caller) observationRuleFacts() map[string]ExprValue {
	out := map[string]ExprValue{"observation.ready": ExprPresent("boolean", false), "observation.source": {Kind: "text", Status: "missing"}, "observation.object": {Kind: "text", Status: "missing"}, "observation.subject": {Kind: "text", Status: "missing"}}
	if proof := c.ObservationContext(); proof != nil && proof.Intervals != nil && proof.Intervals.Ready {
		object, subject, ok := strings.Cut(proof.Record, "/")
		if !ok {
			return out
		}
		out["observation.ready"] = ExprPresent("boolean", true)
		out["observation.source"] = ExprPresent("text", proof.Definition.Ref.String())
		out["observation.object"] = ExprPresent("text", object)
		out["observation.subject"] = ExprPresent("text", subject)
	}
	return out
}
