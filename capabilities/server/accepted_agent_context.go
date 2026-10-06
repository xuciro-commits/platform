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
	prior = legacyAgentBindingImage(prior, image)
	if prior != nil {
		if before, err := acceptedRowOf(image.Type, image.ID, prior); err == nil {
			if digest, err := canonicalDigest(before); err == nil && digest == image.Before {
				return prior
			}
		}
	}
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

// Additive startup fields omitted by a legacy replay may be recovered only
// from the already committed row image. The caller still requires the exact
// predecessor digest; no version is inferred from today's agent registry.
func legacyAgentBindingImage(prior *row, image acceptedBatchRow) *row {
	if prior == nil || image.App != AgentApp || image.Type != RunType || len(image.History) < len(prior.history) {
		return prior
	}
	run, ok := prior.value.Interface().(AgentRunRecord)
	if !ok {
		return prior
	}
	var saved AgentRunRecord
	if json.Unmarshal(image.Value, &saved) != nil {
		return prior
	}
	changed := false
	if run.Release == "" && saved.Release != "" {
		run.Release = saved.Release
		changed = true
	}
	if run.DefinitionVersion == "" && saved.DefinitionVersion != "" {
		run.DefinitionVersion = saved.DefinitionVersion
		changed = true
	}
	if !changed {
		return prior
	}
	history := copyHistory(prior.history)
	for i, entry := range history {
		fields := map[string]FieldChange{}
		for _, field := range entry.Fields {
			fields[field.Field] = field
		}
		ordered := []FieldChange{}
		for _, field := range image.History[i].Fields {
			if field.Field == "release" || field.Field == "definitionVersion" {
				if existing, ok := fields[field.Field]; ok {
					ordered = append(ordered, existing)
					delete(fields, field.Field)
				} else {
					ordered = append(ordered, field)
				}
			} else {
				existing, ok := fields[field.Field]
				if !ok {
					return prior
				}
				ordered = append(ordered, existing)
				delete(fields, field.Field)
			}
		}
		if len(fields) > 0 {
			return prior
		}
		history[i].Fields = ordered
	}
	return &row{value: reflect.ValueOf(&run).Elem(), history: history}
}
