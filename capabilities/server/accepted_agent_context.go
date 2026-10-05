package platformserver

import (
	"encoding/json"
	"reflect"
)

// Legacy agent starts reconstructed their context with host-visible flow
// summaries. ADR-0050 narrowed live reads. A later accepted batch contains the
// original history and predecessor digest: recover only that omitted summary,
// and require the entire reconstructed row to match that exact digest. Never
// accept differences in business values, receipts or other history fields.
func legacyAgentContextPredecessor(prior *row, image acceptedBatchRow) *row {
	if prior == nil || image.App != AgentApp || image.Type != RunType || len(prior.history) == 0 || len(image.History) <= len(prior.history) {
		return nil
	}
	run, ok := prior.value.Interface().(AgentRunRecord)
	if !ok || run.Flow == "" || run.OnBehalf != "" {
		return nil
	}
	var historical string
	for _, field := range image.History[0].Fields {
		if field.Field == "seen" {
			if json.Unmarshal(field.After, &historical) != nil {
				return nil
			}
		}
	}
	if historical == "" || historical == run.Seen {
		return nil
	}
	var oldContext, currentContext map[string]json.RawMessage
	if json.Unmarshal([]byte(historical), &oldContext) != nil || json.Unmarshal([]byte(run.Seen), &currentContext) != nil {
		return nil
	}
	var oldFlows, currentFlows []FlowSummary
	if json.Unmarshal(oldContext["flows"], &oldFlows) != nil || json.Unmarshal(currentContext["flows"], &currentFlows) != nil || len(oldFlows) != 1 || len(currentFlows) != 0 || oldFlows[0].ID != run.Flow {
		return nil
	}
	oldContext["flows"] = json.RawMessage("[]")
	oldDigest, err := canonicalDigest(oldContext)
	currentDigest, currentErr := canonicalDigest(currentContext)
	if err != nil || currentErr != nil || oldDigest != currentDigest {
		return nil
	}
	history := copyHistory(prior.history)
	for i, field := range history[0].Fields {
		if field.Field == "seen" {
			var seen string
			if json.Unmarshal(field.After, &seen) != nil || seen != run.Seen {
				return nil
			}
			history[0].Fields[i].After, _ = json.Marshal(historical)
		}
	}
	run.Seen = historical
	recovered := &row{value: reflect.ValueOf(&run).Elem(), history: history}
	before, err := acceptedRowOf(image.Type, image.ID, recovered)
	if err != nil {
		return nil
	}
	digest, err := canonicalDigest(before)
	prefix, prefixErr := canonicalDigest(history)
	expectedPrefix, expectedErr := canonicalDigest(image.History[:len(history)])
	if err != nil || digest != image.Before || prefixErr != nil || expectedErr != nil || prefix != expectedPrefix {
		return nil
	}
	return recovered
}
