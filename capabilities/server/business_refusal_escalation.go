package platformserver

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Escalation consumes durable business facts in a later Work decision. Nothing
// from the rejected transaction's private records or effects is promoted.
func (d *stagedDecision) PendingRefusalEscalations(c platform.Caller, at time.Time) ([]platform.RefusalEscalationRequest, *kernel.Error) {
	if !c.Staging() || !c.Automation || c.App != platform.WorkApp || c.ID != "app:"+platform.WorkApp || c.Replaying || c.Tenant != d.tenant.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal escalation requires the private Work owner")
	}
	type event struct {
		key  string
		fact platform.BusinessRefusalFact
	}
	type group struct {
		app, subject, action, code string
		policy                     platform.RefusalEscalation
	}
	groups := map[group][]event{}
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
		groups[g] = append(groups[g], event{key: key, fact: fact})
	}
	for key, r := range d.tenant.committed.refusals {
		if r.Business != nil {
			add(key, *r.Business)
		}
	}
	for key, raw := range d.tenant.committed.answers {
		var envelope struct{ Kind string }
		if json.Unmarshal(raw, &envelope) != nil || envelope.Kind != "record-batch" {
			continue
		}
		r, _, err := decodeAcceptedBatch(raw)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted refusal history cannot be read")
		}
		for _, fact := range r.Business {
			add(key, fact)
		}
	}
	var pending []platform.RefusalEscalationRequest
	for g, events := range groups {
		slices.SortFunc(events, func(a, b event) int {
			if c := a.fact.At.Compare(b.fact.At); c != 0 {
				return c
			}
			return strings.Compare(a.key, b.key)
		})
		start := 0
		for index, current := range events {
			if !current.fact.At.Before(events[start].fact.At.Add(time.Duration(g.policy.WindowSeconds) * time.Second)) {
				start = index
			}
			if index-start+1 != g.policy.Count {
				continue
			}
			key, err := canonicalDigest([]any{g.app, g.subject, g.action, g.code, g.policy, events[start].key})
			if err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The refusal escalation identity cannot be frozen")
			}
			request := platform.RefusalEscalationRequest{Key: key, App: g.app, Subject: g.subject, Action: g.action, Code: g.code, Message: current.fact.Message, Count: g.policy.Count, First: events[start].fact.At, Last: current.fact.At}
			if _, exists := platform.Get[platform.WorkTask](c, request.TaskID()); exists {
				continue // closed tasks also retain their permanent refusal identity
			}
			business := platform.NewCaller(d, platform.Member{ID: "app:" + g.app, Tenant: c.Tenant}, g.app, false, true)
			request.Candidates = d.recipients(business, at, []platform.Recipient{{AppRole: g.policy.ReviewerRole}})
			request.Candidates = slices.DeleteFunc(request.Candidates, func(id string) bool {
				m, known := (hostView{t: d.tenant, decision: d}).Member(id)
				return !known || !m.Active() || !d.Readable(platform.NewCaller(d, m, g.app, false, false), g.subject)
			})
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
