package platformserver

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type BusinessInputChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}
type BusinessRefusalEntry struct {
	Key          string                         `json:"key"`
	App          string                         `json:"app"`
	Type         string                         `json:"type"`
	ID           string                         `json:"id"`
	Action       string                         `json:"action"`
	Member       string                         `json:"member"`
	At           time.Time                      `json:"at"`
	Category     string                         `json:"category"`
	Code         string                         `json:"code"`
	Message      string                         `json:"message"`
	State        string                         `json:"state"`
	Inputs       json.RawMessage                `json:"inputs,omitempty"`
	Issues       []platform.FieldIssue          `json:"issues,omitempty"`
	Facts        []platform.BusinessRefusalFact `json:"facts,omitempty"`
	CorrectionOf string                         `json:"correctionOf,omitempty"`
	Changes      []BusinessInputChange          `json:"changes,omitempty"`
}
type BusinessRefusalPage struct {
	Records []BusinessRefusalEntry `json:"records"`
	Total   int                    `json:"total"`
}
type RefusalCodeCount struct {
	Category string `json:"category"`
	Code     string `json:"code"`
	Type     string `json:"type"`
	Count    int    `json:"count"`
}

func refusalCategory(result refusedResult) (string, string) {
	if result.Business != nil {
		return "business", result.Business.Code
	}
	if slices.ContainsFunc(result.Issues, func(i platform.FieldIssue) bool { return i.Constraint != "" }) {
		return "constraint", "constraint_violation"
	}
	if result.Error.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED || result.Error.Code == pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY {
		return "policy", result.Error.Code.String()
	}
	if result.Error.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || result.Error.Code == pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE {
		return "input", result.Error.Code.String()
	}
	return "technical", result.Error.Code.String()
}
func correctionOf(sub *pb.Submission) string {
	key, _ := strings.CutPrefix(sub.GetCorrelationId(), "correction:")
	if key == sub.GetCorrelationId() {
		return ""
	}
	return key
}
func changedBusinessInputs(before, after []byte) []BusinessInputChange {
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
	var out []BusinessInputChange
	for k := range names {
		ah, _ := canonicalDigest(a[k])
		bh, _ := canonicalDigest(b[k])
		_, aExists := a[k]
		_, bExists := b[k]
		if ah != bh || aExists != bExists {
			out = append(out, BusinessInputChange{Field: k, Before: slices.Clone(a[k]), After: slices.Clone(b[k])})
		}
	}
	slices.SortFunc(out, func(a, b BusinessInputChange) int { return strings.Compare(a.Field, b.Field) })
	return out
}
func (t *Tenant) checkBusinessCorrection(m platform.Member, sub *pb.Submission) *kernel.Error {
	key := correctionOf(sub)
	if key == "" {
		return nil
	}
	prior, ok := t.committed.refusals[sub.GetAuthority()+"/"+key]
	var original pb.Submission
	if !ok || key == sub.GetIdempotencyKey() || protojson.Unmarshal(prior.Submission, &original) != nil || original.GetPrincipalId() != m.ID || original.GetAuthority() != sub.GetAuthority() || original.GetSchema().GetName() != sub.GetSchema().GetName() || original.GetTarget().GetType() != sub.GetTarget().GetType() || original.GetTarget().GetId() != sub.GetTarget().GetId() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A correction needs the same applicant, action and target as its original refused request")
	}
	if len(changedBusinessInputs(original.GetPayload(), sub.GetPayload())) == 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unchanged inputs must repeat the original request key")
	}
	return nil
}

// The accepted-result journal remains the truth. This is a caller-filtered
// projection of its already restored envelopes, not another refusal store.
func (t *Tenant) BusinessRefusals(m platform.Member, typ, id string, offset, limit int, at time.Time, keys ...string) (BusinessRefusalPage, *kernel.Error) {
	out := BusinessRefusalPage{Records: []BusinessRefusalEntry{}}
	if err := t.admits(m); err != nil {
		return out, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if typ == "" || id == "" {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the original business record")
	}
	add := func(sub *pb.Submission, e BusinessRefusalEntry) {
		if sub.GetTarget().GetType() != typ || sub.GetTarget().GetId() != id {
			return
		}
		if len(keys) > 0 && keys[0] != "" && sub.GetIdempotencyKey() != keys[0] {
			return
		}
		own := sub.GetPrincipalId() == m.ID
		readable := t.Readable(m, target(sub), at)
		exists := false
		t.records.mu.Lock()
		if et := t.records.types[typ]; et != nil {
			exists = et.rows.get(id) != nil
		}
		t.records.mu.Unlock()
		if exists && !readable || !exists && !own {
			return
		}
		e.Key, e.App, e.Type, e.ID, e.Action, e.Member = sub.GetIdempotencyKey(), sub.GetAuthority(), typ, id, sub.GetSchema().GetName(), sub.GetPrincipalId()
		e.CorrectionOf = correctionOf(sub)
		if own && (!exists || readable) {
			e.Inputs = slices.Clone(sub.GetPayload())
			if e.CorrectionOf != "" {
				if prior, ok := t.committed.refusals[e.App+"/"+e.CorrectionOf]; ok {
					var old pb.Submission
					if protojson.Unmarshal(prior.Submission, &old) == nil {
						e.Changes = changedBusinessInputs(old.GetPayload(), sub.GetPayload())
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
	for _, r := range t.committed.refusals {
		var sub pb.Submission
		if protojson.Unmarshal(r.Submission, &sub) != nil {
			continue
		}
		category, code := refusalCategory(r)
		e := BusinessRefusalEntry{At: r.At, Category: category, Code: code, Message: r.Error.Message, State: "refused", Issues: r.Issues}
		if r.Business != nil {
			e.Facts = []platform.BusinessRefusalFact{cloneBusinessFact(*r.Business)}
			e.Message = r.Business.Message
		}
		add(&sub, e)
	}
	for _, raw := range t.committed.answers {
		var kind struct{ Kind string }
		if json.Unmarshal(raw, &kind) != nil || kind.Kind != "record-batch" {
			continue
		}
		r, receipt, err := decodeAcceptedBatch(raw)
		if err != nil {
			continue
		}
		sub, err := batchSubmission(r, receipt)
		if err != nil {
			continue
		}
		if len(r.Business) == 0 && correctionOf(sub) == "" {
			continue
		}
		if len(r.Business) == 0 {
			add(sub, BusinessRefusalEntry{At: r.At, State: "accepted", Category: "business"})
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
			e := BusinessRefusalEntry{At: r.At, State: "accepted", Category: "business", Facts: facts, Code: facts[0].Code, Message: facts[0].Message}
			add(child, e)
		}
	}
	slices.SortFunc(out.Records, func(a, b BusinessRefusalEntry) int {
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
	f = cloneBusinessFact(f)
	if f.Record != "" && !t.Readable(m, f.Record, at) {
		f.Record = ""
	}
	if f.Route != nil && f.Route.ID != "" && !t.Readable(m, f.Route.Type+"/"+f.Route.ID, at) {
		f.Route = nil
	}
	typ, id, _ := strings.Cut(f.Subject, "/")
	t.records.mu.Lock()
	et := t.records.types[typ]
	readable := et != nil && et.rows.get(id) != nil && t.readableIn(t.records, m, f.Subject, at)
	t.records.mu.Unlock()
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
	info := et.info
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
			beforeHash, _ := canonicalDigest(rawBefore[name])
			afterHash, _ := canonicalDigest(rawAfter[name])
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
