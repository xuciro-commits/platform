package business

import (
	"cmp"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func RefusalCategory(answer kernel.Error, business *platform.BusinessRefusalFact, issues []platform.FieldIssue) (string, string) {
	if business != nil {
		return "business", business.Code
	}
	if slices.ContainsFunc(issues, func(i platform.FieldIssue) bool { return i.Constraint != "" }) {
		return "constraint", "constraint_violation"
	}
	if answer.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED || answer.Code == pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY {
		return "policy", answer.Code.String()
	}
	if answer.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || answer.Code == pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE {
		return "input", answer.Code.String()
	}
	return "technical", answer.Code.String()
}
func CorrectionOf(sub *pb.Submission) string {
	key, _ := strings.CutPrefix(sub.GetCorrelationId(), "correction:")
	if key == sub.GetCorrelationId() {
		return ""
	}
	return key
}
func ChangedBusinessInputs(before, after []byte) []platform.BusinessInputChange {
	var a, b map[string]json.RawMessage
	if json.Unmarshal(before, &a) != nil || json.Unmarshal(after, &b) != nil {
		return nil
	}
	names := map[string]bool{}
	for k := range a {
		names[k] = true
	}
	for k := range b {
		names[k] = true
	}
	var out []platform.BusinessInputChange
	for k := range names {
		ah, _ := platform.CanonicalDigest(a[k])
		bh, _ := platform.CanonicalDigest(b[k])
		_, aExists := a[k]
		_, bExists := b[k]
		if ah != bh || aExists != bExists {
			out = append(out, platform.BusinessInputChange{Field: k, Before: slices.Clone(a[k]), After: slices.Clone(b[k])})
		}
	}
	slices.SortFunc(out, func(a, b platform.BusinessInputChange) int { return strings.Compare(a.Field, b.Field) })
	return out
}

type RefusalEvent struct {
	Key  string
	Fact platform.BusinessRefusalFact
}

func RefusalEscalations(events []RefusalEvent, at time.Time, exists func(string) bool, recipients func(platform.RefusalEscalationRequest, string) []string) ([]platform.RefusalEscalationRequest, *kernel.Error) {

	type group struct {
		app, subject, action, code string
		policy                     platform.RefusalEscalation
	}
	groups := map[group][]RefusalEvent{}
	seen := map[string]bool{}
	add := func(key string, fact platform.BusinessRefusalFact) {
		if fact.Escalation == nil || fact.Escalation.Check() != nil || fact.At.After(at) {
			return
		}
		app, _, _ := strings.Cut(fact.Action, ".")
		g := group{app: app, subject: fact.Subject, action: fact.Action, code: fact.Code, policy: *fact.Escalation}
		identity := app + "/" + key + "/" + fact.Subject + "/" + fact.Action + "/" + fact.Code
		if seen[identity] {
			return // one submitted intent, even when a foreach flags many rows
		}
		seen[identity] = true
		groups[g] = append(groups[g], RefusalEvent{Key: key, Fact: fact})
	}

	for _, event := range events {
		add(event.Key, event.Fact)
	}
	var pending []platform.RefusalEscalationRequest
	for g, events := range groups {
		slices.SortFunc(events, func(a, b RefusalEvent) int {
			if c := a.Fact.At.Compare(b.Fact.At); c != 0 {
				return c
			}
			return strings.Compare(a.Key, b.Key)
		})
		start := 0
		for index, current := range events {
			if !current.Fact.At.Before(events[start].Fact.At.Add(time.Duration(g.policy.WindowSeconds) * time.Second)) {
				start = index
			}
			if index-start+1 != g.policy.Count {
				continue
			}
			key, err := platform.CanonicalDigest([]any{g.app, g.subject, g.action, g.code, g.policy, events[start].Key})
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The refusal escalation identity cannot be frozen")
			}
			request := platform.RefusalEscalationRequest{Key: key, App: g.app, Subject: g.subject, Action: g.action, Code: g.code, Message: current.Fact.Message, Count: g.policy.Count, First: events[start].Fact.At, Last: current.Fact.At}
			if exists(request.TaskID()) {
				continue
			}
			request.Candidates = recipients(request, g.policy.ReviewerRole)
			slices.Sort(request.Candidates)

			if len(request.Candidates) > 0 {
				pending = append(pending, request)
			}
		}
	}
	slices.SortFunc(pending, func(a, b platform.RefusalEscalationRequest) int {
		if c := a.Last.Compare(b.Last); c != 0 {
			return c
		}
		return cmp.Compare(a.Key, b.Key)
	})
	return pending[:min(len(pending), 32)], nil
}
