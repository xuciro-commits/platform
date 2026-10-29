package platformserver

import records "platformserver/kernel/records"

import (
	"encoding/json"
	"platformserver/engine/business"
	acceptedformat "platformserver/kernel/acceptedformat"

	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) checkBusinessCorrection(m platform.Member, sub *pb.Submission, binding *platform.SchemaVersion) *kernel.Error {
	key := business.CorrectionOf(sub)
	if key == "" {
		return nil
	}
	prior, ok := t.committed.Refusal(sub.GetAuthority() + "/" + key)
	var original pb.Submission
	if !ok || key == sub.GetIdempotencyKey() || protojson.Unmarshal(prior.Submission, &original) != nil || original.GetPrincipalId() != m.ID || original.GetAuthority() != sub.GetAuthority() || original.GetSchema().GetName() != sub.GetSchema().GetName() || original.GetTarget().GetType() != sub.GetTarget().GetType() || original.GetTarget().GetId() != sub.GetTarget().GetId() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A correction needs the same applicant, action and target as its original refused request")
	}
	sameSchema := original.GetSchema().GetVersion() == sub.GetSchema().GetVersion() && reflect.DeepEqual(prior.SchemaVersion, binding)
	if sameSchema && len(business.ChangedBusinessInputs(original.GetPayload(), sub.GetPayload())) == 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unchanged inputs must repeat the original request key")
	}
	return nil
}

// The accepted-result journal remains the truth. This is a caller-filtered
// projection of its already restored envelopes, not another refusal store.
func (t *Tenant) BusinessRefusals(m platform.Member, typ, id string, offset, limit int, at time.Time, keys ...string) (platform.BusinessRefusalPage, *kernel.Error) {
	out := platform.BusinessRefusalPage{Records: []platform.BusinessRefusalEntry{}}
	if err := t.admits(m); err != nil {
		return out, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if typ == "" || id == "" {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the original business record")
	}
	add := func(sub *pb.Submission, e platform.BusinessRefusalEntry) {
		if sub.GetTarget().GetType() != typ || sub.GetTarget().GetId() != id {
			return
		}
		if len(keys) > 0 && keys[0] != "" && sub.GetIdempotencyKey() != keys[0] {
			return
		}
		own := sub.GetPrincipalId() == m.ID
		readable := t.readable(m, target(sub), at)
		exists := false
		func() {
			t.records.Lock()
			defer t.records.Unlock()
			if et := t.records.Type(typ); et != nil {
				exists = et.Rows().Get(id) != nil
			}
		}()
		if exists && !readable || !exists && !own {
			return
		}
		e.Key, e.App, e.Type, e.ID, e.Action, e.Member = sub.GetIdempotencyKey(), sub.GetAuthority(), typ, id, sub.GetSchema().GetName(), sub.GetPrincipalId()
		e.CorrectionOf = business.CorrectionOf(sub)
		if own && (!exists || readable) {
			e.Inputs = slices.Clone(sub.GetPayload())
			if e.CorrectionOf != "" {
				if prior, ok := t.committed.Refusal(e.App + "/" + e.CorrectionOf); ok {
					var old pb.Submission
					if protojson.Unmarshal(prior.Submission, &old) == nil {
						e.Changes = business.ChangedBusinessInputs(old.GetPayload(), sub.GetPayload())
					}
				}
			}
		}
		e.Issues = t.readableFieldIssues(m, e.Issues, at)
		for i, f := range e.Facts {
			e.Facts[i] = t.readableBusinessFact(m, f, at)
			if !own {
				e.Facts[i].Inputs = nil
			}
		}
		out.Records = append(out.Records, e)
	}
	for _, r := range t.committed.AllRefusals() {
		var sub pb.Submission
		if protojson.Unmarshal(r.Submission, &sub) != nil {
			continue
		}
		category, code := business.RefusalCategory(r.Error, r.Business, r.Issues)
		e := platform.BusinessRefusalEntry{At: r.At, Category: category, Code: code, Message: r.Error.Message, State: "refused", Issues: r.Issues}
		if r.Business != nil {
			e.Facts = []platform.BusinessRefusalFact{acceptedformat.CloneBusinessFact(*r.Business)}
			e.Message = r.Business.Message
		}
		add(&sub, e)
	}
	for _, raw := range t.committed.AllAnswers() {
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
		if len(r.Business) == 0 && business.CorrectionOf(sub) == "" {
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
				if f.Subject == target(child) && f.Action == child.GetSchema().GetName() && f.Member == child.GetPrincipalId() {
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
	return out, nil
}

func (t *Tenant) readableBusinessFact(m platform.Member, f platform.BusinessRefusalFact, at time.Time) platform.BusinessRefusalFact {
	f = acceptedformat.CloneBusinessFact(f)
	if f.Record != "" && !t.readable(m, f.Record, at) {
		f.Record = ""
	}
	if f.Route != nil && f.Route.ID != "" && !t.readable(m, f.Route.Type+"/"+f.Route.ID, at) {
		f.Route = nil
	}
	typ, id, _ := strings.Cut(f.Subject, "/")
	var readable bool
	var et *records.Type
	func() {
		t.records.Lock()
		defer t.records.Unlock()
		et = t.records.Type(typ)
		readable = et != nil && et.Rows().Get(id) != nil && t.readableIn(t.records, m, f.Subject, at)
	}()
	if !readable {
		f.Original = nil
		f.Inputs = nil
		return f
	}
	var original map[string]json.RawMessage
	if json.Unmarshal(f.Original, &original) != nil {
		f.Original = nil
		return f
	}
	info := et.Info()
	// Re-run the existing derived-source projection on the historical value itself,
	// not on today's record, whose source references may have changed.
	historical := reflect.New(info.Go).Elem()
	if json.Unmarshal(f.Original, historical.Addr().Interface()) != nil {
		f.Original = nil
		return f
	}
	if info.HasReadSources() || len(info.VersionPins) > 0 {
		narrowed, _ := t.narrower(m, at, true).derive(et, historical)
		var rawBefore, rawAfter map[string]json.RawMessage
		_ = json.Unmarshal(platform.Raw(historical.Interface()), &rawBefore)
		_ = json.Unmarshal(platform.Raw(narrowed.Interface()), &rawAfter)
		for name := range original {
			beforeHash, _ := platform.CanonicalDigest(rawBefore[name])
			afterHash, _ := platform.CanonicalDigest(rawAfter[name])
			if beforeHash != afterHash {
				delete(original, name)
			}
		}
	}
	for name := range original {
		field, ok := info.Field(name)
		if name != "id" && (!ok || !field.ReadsAny(m.RolesIn(info.App))) {
			delete(original, name)
		}
	}
	f.Original = platform.Raw(original)
	if m.ID != f.Member {
		f.Inputs = nil
	}
	return f
}
