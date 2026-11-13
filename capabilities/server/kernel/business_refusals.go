package kernel

import (
	"encoding/json"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type RefusalView struct {
	Member     string
	Readable   func(ref string) bool
	Exists     func() bool
	Correction func(*pb.Submission) string
	Category   func(kernel.Error, *platform.BusinessRefusalFact, []platform.FieldIssue) (string, string)
	Changes    func(before, after []byte) []platform.BusinessInputChange
	Issues     func([]platform.FieldIssue) []platform.FieldIssue
	Fact       func(platform.BusinessRefusalFact) platform.BusinessRefusalFact
}

func (c *Committed) BusinessRefusals(v RefusalView, typ, id, key string, offset, limit int) platform.BusinessRefusalPage {
	out := platform.BusinessRefusalPage{Records: []platform.BusinessRefusalEntry{}}
	add := func(sub *pb.Submission, e platform.BusinessRefusalEntry) {
		if sub.GetTarget().GetType() != typ || sub.GetTarget().GetId() != id {
			return
		}
		if key != "" && sub.GetIdempotencyKey() != key {
			return
		}
		own := sub.GetPrincipalId() == v.Member
		readable := v.Readable(Target(sub))
		exists := v.Exists()
		if exists && !readable || !exists && !own {
			return
		}
		e.Key, e.App, e.Type, e.ID, e.Action, e.Member = sub.GetIdempotencyKey(), sub.GetAuthority(), typ, id, sub.GetSchema().GetName(), sub.GetPrincipalId()
		e.CorrectionOf = v.Correction(sub)
		if own && (!exists || readable) {
			e.Inputs = slices.Clone(sub.GetPayload())
			if e.CorrectionOf != "" {
				if prior, ok := c.Refusal(e.App + "/" + e.CorrectionOf); ok {
					var old pb.Submission
					if protojson.Unmarshal(prior.Submission, &old) == nil {
						e.Changes = v.Changes(old.GetPayload(), sub.GetPayload())
					}
				}
			}
		}
		e.Issues = v.Issues(e.Issues)
		for i, f := range e.Facts {
			e.Facts[i] = v.Fact(f)
			if !own {
				e.Facts[i].Inputs = nil
			}
		}
		out.Records = append(out.Records, e)
	}
	for _, r := range c.AllRefusals() {
		var sub pb.Submission
		if protojson.Unmarshal(r.Submission, &sub) != nil {
			continue
		}
		category, code := v.Category(r.Error, r.Business, r.Issues)
		e := platform.BusinessRefusalEntry{At: r.At, Category: category, Code: code, Message: r.Error.Message, State: "refused", Issues: r.Issues}
		if r.Business != nil {
			e.Facts = []platform.BusinessRefusalFact{acceptedformat.CloneBusinessFact(*r.Business)}
			e.Message = r.Business.Message
		}
		add(&sub, e)
	}
	for _, raw := range c.AllAnswers() {
		var kind struct{ Kind string }
		if json.Unmarshal(raw, &kind) != nil || kind.Kind != "record-batch" {
			continue
		}
		r, receipt, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			continue
		}
		sub, err := acceptedformat.BatchSubmission(r, receipt)
		if err != nil {
			continue
		}
		if len(r.Business) == 0 && v.Correction(sub) == "" {
			continue
		}
		if len(r.Business) == 0 {
			add(sub, platform.BusinessRefusalEntry{At: r.At, State: "accepted", Category: "business"})
			continue
		}
		for _, decision := range r.Decisions {
			accepted := new(pb.ChangeRecord)
			if protojson.Unmarshal(decision.Receipt, accepted) != nil {
				continue
			}
			child := accepted.GetSubmission()
			facts := []platform.BusinessRefusalFact{}
			for _, f := range r.Business {
				if f.Subject == Target(child) && f.Action == child.GetSchema().GetName() && f.Member == child.GetPrincipalId() {
					facts = append(facts, f)
				}
			}
			if len(facts) == 0 {
				continue
			}
			e := platform.BusinessRefusalEntry{At: r.At, State: "accepted", Category: "business", Facts: facts, Code: facts[0].Code, Message: facts[0].Message}
			add(child, e)
		}
	}
	slices.SortFunc(out.Records, func(a, b platform.BusinessRefusalEntry) int {
		if c := b.At.Compare(a.At); c != 0 {
			return c
		}
		return strings.Compare(a.Key, b.Key)
	})
	out.Total = len(out.Records)
	offset = max(0, min(offset, out.Total))
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	out.Records = out.Records[offset:min(out.Total, offset+limit)]
	return out
}
