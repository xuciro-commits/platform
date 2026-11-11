package ai

import (
	"encoding/json"
	"strings"
	"time"

	"platformserver/platform"
	aigateway "platformserver/services/ai/gateway"
)

type TrainingWorld struct {
	Tenant   string
	Models   []platform.Effect
	App      func(string) platform.App
	Entity   func(string) (platform.EntityInfo, bool)
	Provider func(string) (platform.App, string, bool)
}

func (w TrainingWorld) Evidence(out platform.AITrainingEvidence, event platform.Event, images map[string]json.RawMessage) platform.AITrainingEvidence {
	for i := range out.Candidates {
		candidate := &out.Candidates[i]
		for _, effect := range w.Models {
			var ask aigateway.ModelAsk
			if json.Unmarshal([]byte(effect.Body), &ask) == nil && ask.Record == candidate.Run && ask.Binding != nil {
				candidate.ModelConfig, _ = platform.CanonicalDigest(ask.Binding)
				candidate.PolicyRevision = ask.Binding.PolicyRevision
				break
			}
		}
	}
	sub := event.Record.GetSubmission()
	if sub.GetTarget().GetType() != platform.RunType {
		return out
	}
	image, ok := images[platform.RunType+"/"+sub.GetTarget().GetId()]
	if !ok {
		return out
	}
	var run platform.AgentRunRecord
	if json.Unmarshal(image, &run) != nil {
		return out
	}
	at := event.Record.GetRecordedTime().AsTime()
	for i, draft := range run.Proposals {
		if draft.Outcome == "" || draft.Evidence != event.App+"/"+event.Record.GetChangeId() {
			continue
		}
		identity, _ := platform.CanonicalDigest([]any{w.Tenant, run.ID, i, draft.Definition, draft.Sources, draft.Accepted, draft.Rejected, draft.Outcome})
		candidate := platform.AITrainingCandidate{Target: draft.Type + "/" + draft.Target, ExpectedSources: draft.ExpectedSources, Authorization: draft.Authorization, ID: identity, Tenant: w.Tenant, Intent: run.ID, Run: run.ID, Step: i, App: draft.Owner, Action: draft.Action, Definition: draft.Definition, ActionDefinition: draft.Definition, Sources: draft.Sources, Model: run.Model, Evidence: draft.Evidence, At: at, Outcome: draft.Outcome}
		if run.ModelBinding != nil {
			candidate.PolicyRevision = run.ModelBinding.PolicyRevision
			candidate.ModelConfig, _ = platform.CanonicalDigest(run.ModelBinding)
		}
		if draft.Accepted != "" {
			_ = json.Unmarshal([]byte(draft.Accepted), &candidate.Chosen)
		}
		if draft.Rejected != "" {
			_ = json.Unmarshal([]byte(draft.Rejected), &candidate.Rejected)
		}
		if candidate.Authorization == "" {
			candidate.Rejected = nil
		}
		if len(run.Signals) > 0 {
			candidate.Critique = run.Signals[len(run.Signals)-1].Detail
		}
		owner := w.App(draft.Owner)
		if owner != nil {
			action, _ := owner.Manifest().Actions.Action(draft.Action)
			allowed := map[string]bool{}
			info, _ := w.Entity(draft.Type)
			for _, field := range action.Payload {
				if field.Type != "string" && field.Type != "text" && field.Type != "integer" && field.Type != "decimal" && field.Type != "boolean" && field.Type != "number" {
					continue
				}
				usable := true
				for _, declared := range info.Fields {
					if declared.Name == field.Name && declared.AIInput != nil && !*declared.AIInput {
						usable = false
					}
				}
				if usable {
					allowed[field.Name] = true
				}
			}
			for name := range candidate.Chosen {
				if !allowed[name] {
					delete(candidate.Chosen, name)
				}
			}
			for name := range candidate.Rejected {
				if !allowed[name] {
					delete(candidate.Rejected, name)
				}
			}
		} else {
			candidate.Chosen, candidate.Rejected = nil, nil
		}
		candidate.SourceFields = map[string][]string{}
		for ref := range draft.Sources {
			candidate.SourceFields[ref] = nil

		}
		out.Candidates = append(out.Candidates, candidate)
	}
	return out
}

func (w TrainingWorld) Readable(c platform.Caller, candidate platform.AITrainingCandidate, now time.Time) string {
	if candidate.ActionDefinition != "" {
		owner := w.App(candidate.App)
		if owner == nil {
			return "The proposed action definition changed"
		}
		if _, _, protocol := strings.Cut(candidate.Action, "#"); protocol {
			provider, provided, ok := w.Provider(candidate.Action)
			if !ok || provider == nil {
				return "The proposed action definition changed"
			}
			action, ok := provider.Manifest().Actions.Action(provided)
			if !ok {
				return "The proposed action definition changed"
			}
			digest, _ := platform.CanonicalDigest([]any{candidate.App, candidate.Action, action, action.Roles, action.NeedsApproval})
			if digest != candidate.ActionDefinition {
				return "The proposed action definition changed"
			}
		} else {
			action, ok := owner.Manifest().Actions.Action(candidate.Action)
			if !ok {
				return "The proposed action definition changed"
			}
			digest, _ := platform.CanonicalDigest([]any{candidate.App, action, action.Roles, action.NeedsApproval})
			if digest != candidate.ActionDefinition {
				return "The proposed action definition changed"
			}
		}
	}
	for ref := range candidate.Sources {
		if _, known := candidate.ExpectedSources[ref]; !known {
			return "The original training source evidence is unavailable"
		}
	}
	for ref, revision := range candidate.ExpectedSources {
		typ, id, ok := strings.Cut(ref, "/")
		if !ok {
			return "The training source is unavailable"
		}
		raw, refusal := c.ReadRecord(typ, id, now)
		if refusal != nil {
			return "The training source is not readable"
		}
		var current struct {
			Revision uint32 `json:"revision"`
		}
		_ = json.Unmarshal(raw, &current)
		if current.Revision != revision {
			return "The training source revision changed"
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		for _, field := range candidate.SourceFields[ref] {
			if _, ok := fields[field]; !ok {
				return "A training source field is not readable"
			}
		}
	}
	return ""
}
