package platformserver

import (
	"encoding/json"
	acceptedformat "platformserver/kernel/acceptedformat"

	"platformserver/kernel/records"
	"platformserver/platform"
	"strings"
	"time"
)

func (h hostView) TrainingEvidence(event platform.Event) platform.AITrainingEvidence {
	out := platform.AITrainingEvidence{}
	images := map[string]json.RawMessage{}
	for _, ref := range event.Changed {
		if image, ok := h.trainingRecordAt(ref, event.Record.GetChangeId()); ok {
			images[ref] = image
		}
	}
	for _, app := range h.t.liveApps() {
		if owner, ok := app.(platform.TrainingEvidenceOwner); ok {
			out.Candidates = append(out.Candidates, owner.TrainingCandidates(h.t.automation(app.Manifest().ID, false), event, images)...)
		}
	}
	h.t.opsMu.Lock()
	for i := range out.Candidates {
		candidate := &out.Candidates[i]
		for _, effect := range h.t.outbound {
			if effect.Endpoint != acceptedformat.ModelEndpoint {
				continue
			}
			var ask modelAsk
			if json.Unmarshal([]byte(effect.Body), &ask) == nil && ask.Record == candidate.Run && ask.Binding != nil {
				candidate.ModelConfig, _ = platform.CanonicalDigest(ask.Binding)
				candidate.PolicyRevision = ask.Binding.PolicyRevision
				break
			}
		}
	}
	h.t.opsMu.Unlock()
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
		identity, _ := platform.CanonicalDigest([]any{h.t.ID, run.ID, i, draft.Definition, draft.Sources, draft.Accepted, draft.Rejected, draft.Outcome})
		candidate := platform.AITrainingCandidate{Target: draft.Type + "/" + draft.Target, ExpectedSources: draft.ExpectedSources, Authorization: draft.Authorization, ID: identity, Tenant: h.t.ID, Intent: run.ID, Run: run.ID, Step: i, App: draft.Owner, Action: draft.Action, Definition: draft.Definition, ActionDefinition: draft.Definition, Sources: draft.Sources, Model: run.Model, Evidence: draft.Evidence, At: at, Outcome: draft.Outcome}
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
		owner := h.t.app(draft.Owner)
		if owner != nil {
			action, _ := owner.Manifest().Actions.Action(draft.Action)
			allowed := map[string]bool{}
			info, _ := h.t.entity(draft.Type)
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

func (h hostView) TrainingReadable(member platform.Member, candidate platform.AITrainingCandidate, now time.Time) string {
	if candidate.Tenant != h.t.ID {
		return "The training candidate belongs to another tenant"
	}
	c := platform.NewCaller(runtime{h.t}, member, platform.AIApp, false, false)
	if candidate.ActionDefinition != "" {
		owner := h.t.app(candidate.App)
		if owner == nil {
			return "The proposed action definition changed"
		}
		if _, _, protocol := strings.Cut(candidate.Action, "#"); protocol {
			provider, provided, ok := h.t.provider(candidate.Action)
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

var _ platform.AITrainingHost = hostView{}

func (h hostView) trainingRecordAt(ref, change string) (json.RawMessage, bool) {
	typ, id, ok := strings.Cut(ref, "/")
	if !ok {
		return nil, false
	}
	h.t.records.Lock()
	entity := h.t.records.Type(typ)
	var row *records.Row
	if entity != nil {
		row = entity.Rows().Get(id)
	}
	h.t.records.Unlock()
	if row == nil {
		return nil, false
	}
	raw, err := row.Image()
	if err != nil {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, false
	}
	history := row.RecordHistory()
	found := false
	for i := len(history) - 1; i >= 0; i-- {
		item := history[i]
		if item.Change == change {
			found = true
			break
		}
		for _, field := range item.Fields {
			if len(field.Before) == 0 {
				delete(fields, field.Field)
			} else {
				fields[field.Field] = field.Before
			}
		}
	}
	if !found {
		return nil, false
	}
	raw, err = json.Marshal(fields)
	return raw, err == nil
}
