package ai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

const (
	trainingPolicyType      = "ai.training-policy"
	trainingCandidateType   = "ai.training-candidate"
	trainingPolicySet       = "ai.training-policy.set"
	trainingReview          = "ai.training-candidate.review"
	trainingRevoke          = "ai.training-candidate.revoke"
	trainingCandidatesKept  = 1024
	trainingCandidatesBytes = platform.AcceptedResultMaxBytes / 4
)

func (a *AI) AttachAITraining(h platform.AITrainingHost) { a.trainingHost = h }

func trainingActions() []platform.Action {
	admin := []string{Admin}
	return []platform.Action{
		{Schema: trainingPolicySet, Target: trainingPolicyType, New: true, Title: "Set training data policy", Description: "Choose which accepted fields may become training candidates, for how long, and by what quality rule.", Capability: "training", Roles: admin, Payload: []platform.Field{{Name: "enabled", Type: "boolean"}, {Name: "fields", Type: "json"}, {Name: "trainingAllowed", Type: "boolean"}, {Name: "providerAllowed", Type: "boolean"}, {Name: "qualityRule", Type: "string"}, {Name: "retainDays", Type: "integer", Required: true}}},
		{Schema: trainingReview, Target: trainingCandidateType, Title: "Review training candidate", Description: "A person approves or rejects a candidate with independent quality evidence.", Capability: "training", Roles: admin, Payload: []platform.Field{{Name: "approved", Type: "boolean", Required: true}, {Name: "qualityEvidence", Type: "string", Required: true}}},
		{Schema: trainingRevoke, Target: trainingCandidateType, Title: "Revoke training candidate", Description: "Withdraw a candidate from every later export.", Capability: "training", Roles: admin, Payload: []platform.Field{}},
	}
}

func (a *AI) trainingDecision(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	switch s.GetSchema().GetName() {
	case trainingPolicySet:
		var policy platform.AITrainingPolicy
		if json.Unmarshal(s.GetPayload(), &policy) != nil || id == "" || policy.RetainDays < 1 || policy.RetainDays > 3650 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Training policy needs a name and a retention period of 1–3650 days")
		}
		seen := map[string]bool{}
		for _, field := range policy.Fields {
			if field == "" || strings.ContainsAny(field, "./#") || seen[field] {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose unique declared scalar fields for training")
			}
			seen[field] = true
		}
		policy.ID, policy.Revision, policy.EffectiveAt = id, a.trainingPolicies[id].Revision+1, now
		return func(*pb.ChangeRecord) { a.trainingPolicies[id] = policy }, nil
	case trainingReview, trainingRevoke:
		candidate, ok := a.trainingCandidates[id]
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The training candidate is unavailable")
		}
		if s.GetSchema().GetName() == trainingRevoke {
			candidate.Revoked, candidate.Approved = true, false
		} else {
			var review struct {
				Approved        bool
				QualityEvidence string
			}
			if json.Unmarshal(s.GetPayload(), &review) != nil || strings.TrimSpace(review.QualityEvidence) == "" || c.Agent {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A person must supply independent quality evidence")
			}
			if why := a.trainingProblem(c.Member, candidate, now, false); why != "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{message}", why)
			}
			candidate.Approved, candidate.QualityEvidence = review.Approved, review.QualityEvidence
		}
		return func(*pb.ChangeRecord) { a.trainingCandidates[id] = candidate }, nil
	}
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Unknown training action")
}

// The original event queue owns retries and recovery. Derivation only saves
// bounded, policy-filtered observations; the accepted business result stands.
func (*AI) Interested(_ []string, event platform.Event) bool {
	return event.Record.GetSubmission().GetTarget().GetType() == "agent.run" || slices.ContainsFunc(event.Changed, func(ref string) bool { return strings.HasPrefix(ref, "build.function-call/") })
}
func (a *AI) Listen(c platform.Caller, event platform.Event, _ []string, now time.Time) *kernel.Error {
	if a.trainingHost == nil {
		return nil
	}
	evidence := a.trainingHost.TrainingEvidence(event)
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, candidate := range evidence.Candidates {
		if _, known := a.trainingCandidates[candidate.ID]; known {
			continue
		}
		// No history backfill: every source is derived only from its new accepted
		// event. A policy must already be effective when that event happened.
		originalChosen, originalRejected := candidate.Chosen, candidate.Rejected
		candidate.Chosen, candidate.Rejected = nil, nil
		candidate.Critique = ""
		ids := slices.Sorted(maps.Keys(a.trainingPolicies))
		for _, id := range ids {
			policy := a.trainingPolicies[id]
			if !policy.Enabled || candidate.At.Before(policy.EffectiveAt) || !policy.TrainingAllowed || !policy.ProviderAllowed {
				continue
			}
			candidate.Policy, candidate.PolicyVersion = id, policy.Revision
			if _, ok := candidate.ExpectedSources[candidate.Target]; ok && len(candidate.SourceFields[candidate.Target]) == 0 {
				candidate.SourceFields[candidate.Target] = slices.Clone(policy.Fields)
			}
			candidate.Chosen, candidate.Rejected = map[string]json.RawMessage{}, map[string]json.RawMessage{}
			for _, field := range policy.Fields {
				if value := originalChosen[field]; len(value) > 0 {
					candidate.Chosen[field] = slices.Clone(value)
				}
				if value := originalRejected[field]; len(value) > 0 {
					candidate.Rejected[field] = slices.Clone(value)
				}
			}
			// A public fixed critique avoids copying unrestricted free text into data.
			if len(candidate.Rejected) > 0 {
				candidate.Critique = "The reviewer rejected or corrected the proposed fields"
			}
			break
		}
		if candidate.Outcome == "accepted" && len(candidate.Rejected) == 0 && candidate.Policy != "" && candidate.Authorization != "" {
			current, _ := platform.CanonicalDigest([]any{candidate.Intent, candidate.Action, candidate.Definition, candidate.Sources, candidate.ModelConfig, candidate.Policy, candidate.PolicyVersion, candidate.Authorization})
			var rejected platform.AITrainingCandidate
			for _, previous := range a.trainingCandidates {
				if previous.Outcome != "rejected" || len(previous.Rejected) == 0 || previous.At.After(candidate.At) {
					continue
				}
				prior, _ := platform.CanonicalDigest([]any{previous.Intent, previous.Action, previous.Definition, previous.Sources, previous.ModelConfig, previous.Policy, previous.PolicyVersion, previous.Authorization})
				if prior == current && (rejected.ID == "" || previous.At.After(rejected.At) || previous.At.Equal(rejected.At) && previous.ID > rejected.ID) {
					rejected = previous
				}
			}
			if rejected.ID != "" {
				candidate.Rejected = maps.Clone(rejected.Rejected)
				candidate.Critique = "The reviewer rejected the earlier proposal under the same source and definition"
			}
		}
		// Keep metadata only without an opt-in policy. Old transcript bytes and
		// protected provider continuation never enter this state.
		a.trainingCandidates[candidate.ID] = candidate
	}
	for id, candidate := range a.trainingCandidates {
		policy := a.trainingPolicies[candidate.Policy]
		if policy.RetainDays > 0 && now.Sub(candidate.At) > time.Duration(policy.RetainDays)*24*time.Hour {
			candidate.Chosen, candidate.Rejected, candidate.Revoked = nil, nil, true
			a.trainingCandidates[id] = candidate
		}
	}
	// This is a bounded derivative, not the original evidence ledger. Prefer
	// the newest facts, with an explicit order even for equal timestamps.
	candidates := slices.Collect(maps.Values(a.trainingCandidates))
	slices.SortFunc(candidates, func(left, right platform.AITrainingCandidate) int {
		if order := right.At.Compare(left.At); order != 0 {
			return order
		}
		return strings.Compare(right.ID, left.ID)
	})
	retained := make(map[string]platform.AITrainingCandidate, min(len(candidates), trainingCandidatesKept))
	bytes := 2 // JSON object braces; keys and separators count toward capacity.
	for _, candidate := range candidates {
		if len(retained) == trainingCandidatesKept {
			break
		}
		key, _ := json.Marshal(candidate.ID)
		raw, _ := json.Marshal(candidate)
		entryBytes := len(key) + 1 + len(raw)
		if entryBytes+2 > trainingCandidatesBytes {
			candidate.Chosen, candidate.Rejected = nil, nil
			candidate.Critique, candidate.QualityEvidence, candidate.Approved = "", "", false
			raw, _ = json.Marshal(candidate)
			entryBytes = len(key) + 1 + len(raw)
			if entryBytes+2 > trainingCandidatesBytes {
				continue // The original accepted event still owns this evidence.
			}
		}
		if len(retained) > 0 {
			entryBytes++
		}
		if bytes+entryBytes > trainingCandidatesBytes {
			break
		}
		retained[candidate.ID] = candidate
		bytes += entryBytes
	}
	a.trainingCandidates = retained
	return nil
}

func (a *AI) trainingProblem(member platform.Member, candidate platform.AITrainingCandidate, now time.Time, quality bool) string {
	policy, known := a.trainingPolicies[candidate.Policy]
	switch {
	case candidate.Revoked:
		return "Training use was revoked"
	case !known || !policy.Enabled || policy.Revision != candidate.PolicyVersion:
		return "The original data policy is missing or changed"
	case !policy.TrainingAllowed:
		return "Training use is not permitted"
	case !policy.ProviderAllowed:
		return "Provider terms do not permit training use"
	case len(candidate.Chosen) == 0:
		return "No allowed accepted fields are available"
	case policy.RetainDays > 0 && now.Sub(candidate.At) > time.Duration(policy.RetainDays)*24*time.Hour:
		return "The training retention period expired"
	case quality && (!candidate.Approved || candidate.QualityEvidence == ""):
		return "Independent quality review is required"
	case a.trainingHost == nil:
		return "Training source checks are unavailable"
	}
	return a.trainingHost.TrainingReadable(member, candidate, now)
}

func (a *AI) TrainingExport(c platform.Caller, format string, now time.Time) (platform.AITrainingExport, *kernel.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !c.Holds(ID, Admin) || c.Agent {
		return platform.AITrainingExport{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only a person with AI administration access can export training data")
	}
	if format != "sft" && format != "dpo" {
		return platform.AITrainingExport{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose SFT or DPO")
	}
	rows, manifest := []string{}, []any{}
	for _, id := range slices.Sorted(maps.Keys(a.trainingCandidates)) {
		candidate := a.trainingCandidates[id]
		if a.trainingProblem(c.Member, candidate, now, true) != "" {
			continue
		}
		if format == "dpo" && (len(candidate.Rejected) == 0 || candidate.Critique == "") {
			continue
		}
		sourceRefs := map[string]uint32{}
		for ref, revision := range candidate.Sources {
			digest := sha256.Sum256([]byte(c.Tenant + ":" + ref))
			sourceRefs[hex.EncodeToString(digest[:16])] = revision
		}
		row := map[string]any{"version": 1, "format": format, "id": id, "context": map[string]any{"action": candidate.Action, "schema_digest": candidate.Definition}, "source_refs": sourceRefs, "policy_revision": fmt.Sprintf("%s:%d", candidate.Policy, candidate.PolicyVersion), "quality_evidence": candidate.QualityEvidence, "model_configuration": candidate.ModelConfig, "stage_policy": candidate.PolicyRevision}
		if format == "sft" {
			row["accepted"] = candidate.Chosen
		} else {
			row["chosen"], row["rejected"], row["critique"] = candidate.Chosen, candidate.Rejected, candidate.Critique
		}
		raw, _ := json.Marshal(row)
		rows = append(rows, string(raw))
		manifest = append(manifest, map[string]any{"id": id, "source": candidate.Evidence, "definition": candidate.Definition, "policy": candidate.Policy, "policyVersion": candidate.PolicyVersion})
	}
	jsonl := strings.Join(rows, "\n")
	if len(rows) > 0 {
		jsonl += "\n"
	}
	identity, _ := json.Marshal([]any{format, jsonl, manifest})
	digest := sha256.Sum256(identity)
	id := hex.EncodeToString(digest[:])
	raw, _ := json.Marshal(map[string]any{"version": 1, "id": id, "format": format, "samples": manifest, "revocation": "Revocation prevents future exports. Downloaded files cannot be recalled."})
	return platform.AITrainingExport{ID: id, Format: format, JSONL: jsonl, Manifest: raw}, nil
}
