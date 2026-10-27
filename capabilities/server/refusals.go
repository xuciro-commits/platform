package platformserver

import (
	"cmp"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

// business_refusals.go consolidated into this owner

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

func refusalCategory(result resultformat.Refusal) (string, string) {
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
		ah, _ := acceptance.Digest(a[k])
		bh, _ := acceptance.Digest(b[k])
		_, aExists := a[k]
		_, bExists := b[k]
		if ah != bh || aExists != bExists {
			out = append(out, BusinessInputChange{Field: k, Before: slices.Clone(a[k]), After: slices.Clone(b[k])})
		}
	}
	slices.SortFunc(out, func(a, b BusinessInputChange) int { return strings.Compare(a.Field, b.Field) })
	return out
}
func (t *Tenant) checkBusinessCorrection(m platform.Member, sub *pb.Submission, binding *platform.SchemaVersion) *kernel.Error {
	key := correctionOf(sub)
	if key == "" {
		return nil
	}
	prior, ok := t.committed.refusal(sub.GetAuthority() + "/" + key)
	var original pb.Submission
	if !ok || key == sub.GetIdempotencyKey() || protojson.Unmarshal(prior.Submission, &original) != nil || original.GetPrincipalId() != m.ID || original.GetAuthority() != sub.GetAuthority() || original.GetSchema().GetName() != sub.GetSchema().GetName() || original.GetTarget().GetType() != sub.GetTarget().GetType() || original.GetTarget().GetId() != sub.GetTarget().GetId() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A correction needs the same applicant, action and target as its original refused request")
	}
	sameSchema := original.GetSchema().GetVersion() == sub.GetSchema().GetVersion() && reflect.DeepEqual(prior.SchemaVersion, binding)
	if sameSchema && len(changedBusinessInputs(original.GetPayload(), sub.GetPayload())) == 0 {
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
		readable := t.readable(m, target(sub), at)
		exists := false
		func() {
			t.records.mu.Lock()
			defer t.records.mu.Unlock()
			if et := t.records.types[typ]; et != nil {
				exists = et.rows.Get(id) != nil
			}
		}()
		if exists && !readable || !exists && !own {
			return
		}
		e.Key, e.App, e.Type, e.ID, e.Action, e.Member = sub.GetIdempotencyKey(), sub.GetAuthority(), typ, id, sub.GetSchema().GetName(), sub.GetPrincipalId()
		e.CorrectionOf = correctionOf(sub)
		if own && (!exists || readable) {
			e.Inputs = slices.Clone(sub.GetPayload())
			if e.CorrectionOf != "" {
				if prior, ok := t.committed.refusal(e.App + "/" + e.CorrectionOf); ok {
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
	for _, r := range t.committed.allRefusals() {
		var sub pb.Submission
		if protojson.Unmarshal(r.Submission, &sub) != nil {
			continue
		}
		category, code := refusalCategory(r)
		e := BusinessRefusalEntry{At: r.At, Category: category, Code: code, Message: r.Error.Message, State: "refused", Issues: r.Issues}
		if r.Business != nil {
			e.Facts = []platform.BusinessRefusalFact{resultformat.CloneBusinessFact(*r.Business)}
			e.Message = r.Business.Message
		}
		add(&sub, e)
	}
	for _, raw := range t.committed.allAnswers() {
		var kind struct{ Kind string }
		if json.Unmarshal(raw, &kind) != nil || kind.Kind != "record-batch" {
			continue
		}
		r, receipt, err := resultformat.DecodeBatch(raw)
		if err != nil {
			continue
		}
		sub, err := resultformat.BatchSubmission(r, receipt)
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
	f = resultformat.CloneBusinessFact(f)
	if f.Record != "" && !t.readable(m, f.Record, at) {
		f.Record = ""
	}
	if f.Route != nil && f.Route.ID != "" && !t.readable(m, f.Route.Type+"/"+f.Route.ID, at) {
		f.Route = nil
	}
	typ, id, _ := strings.Cut(f.Subject, "/")
	var readable bool
	var et *entityType
	func() {
		t.records.mu.Lock()
		defer t.records.mu.Unlock()
		et = t.records.types[typ]
		readable = et != nil && et.rows.Get(id) != nil && t.readableIn(t.records, m, f.Subject, at)
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
			beforeHash, _ := acceptance.Digest(rawBefore[name])
			afterHash, _ := acceptance.Digest(rawAfter[name])
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

// accepted_refusal.go consolidated into this owner

func resultRequestHash(raw []byte) (string, error) {
	identity, err := acceptedIdentity(raw)
	return identity.Hash, err
}

func acceptedIdentity(raw []byte) (resultIdentity, error) {
	var identity resultIdentity
	var envelope struct{ Kind string }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return identity, err
	}
	identity.Scope = "submission"
	if envelope.Kind == "host-result" {
		r, e := decodeAcceptedHostControl(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "host"
		return identity, e
	}
	if envelope.Kind == "composite-result" {
		r, e := decodeAcceptedComposite(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "composite"
		return identity, e
	}
	if envelope.Kind == "usage-result" {
		r, e := resultformat.DecodeUsage(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "usage"
		return identity, e
	}
	if envelope.Kind == "agent-result" {
		r, e := decodeAcceptedAgent(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "agent"
		return identity, e
	}
	if envelope.Kind == "operation-claim" {
		result, err := decodeOperationClaim(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	if envelope.Kind == "refusal" {
		result, sub, err := resultformat.DecodeRefusal(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "record-batch" {
		result, receipt, err := resultformat.DecodeBatch(raw)
		if err != nil {
			return identity, err
		}
		sub, err := resultformat.BatchSubmission(result, receipt)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "work-result" {
		result, err := decodeAcceptedWork(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "work"
		return identity, err
	}
	if envelope.Kind == "input-result" {
		result, err := resultformat.DecodeInput(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "input"
		return identity, err
	}
	if envelope.Kind == "effect-result" {
		result, err := resultformat.DecodeEffect(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, PlatformApp, result.Key, result.RequestHash
		identity.Scope = "effect"
		return identity, err
	}
	if envelope.Kind == "release-result" {
		result, err := resultformat.DecodeRelease(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	result, receipt, err := resultformat.DecodeRecord(raw)
	identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, receipt.GetSubmission().GetIdempotencyKey(), result.RequestHash
	return identity, err
}

// business_refusal_facts.go consolidated into this owner

func (d *stagedDecision) BusinessRefused(answer *kernel.Error, fact platform.BusinessRefusalFact) {
	if answer == nil {
		return
	}
	app, _, _ := strings.Cut(fact.Action, ".")
	if err := resultformat.ValidateBusinessFact(app, fact); err != nil {
		d.failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
		return
	}
	if d.businessRefusals == nil {
		d.businessRefusals = map[*kernel.Error]platform.BusinessRefusalFact{}
	}
	d.businessRefusals[answer] = resultformat.CloneBusinessFact(fact)
	d.InputRefused(answer, []platform.FieldIssue{{Code: "business_refusal", BusinessCode: fact.Code, Record: fact.Subject, Message: fact.Message, Translations: maps.Clone(fact.Translations), Path: []string{"$business"}}})
}

func (d *stagedDecision) CaptureBusinessRefusal(c platform.Caller, receipt *pb.ChangeRecord, fact platform.BusinessRefusalFact) *kernel.Error {
	if !c.Staging() || c.Replaying || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || fact.Member != c.ID || !fact.At.Equal(d.at) || fact.Subject != target(receipt.GetSubmission()) || fact.Action != receipt.GetSubmission().GetSchema().GetName() || fact.Level == "block" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal evidence must belong to its original decision")
	}
	if err := resultformat.ValidateBusinessFact(c.App, fact); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if len(d.businessFacts) >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
	}
	d.businessFacts = append(d.businessFacts, resultformat.CloneBusinessFact(fact))
	return nil
}

func (d *stagedDecision) RefuseBusinessCondition(c platform.Caller, code, message string, original any, inputs map[string]any, at time.Time) *kernel.Error {
	sub := d.currentSubmission
	if sub == nil || sub.GetAuthority() != c.App || sub.GetPrincipalId() != c.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", message)
	}
	fact := platform.BusinessRefusalFact{Level: "block", Code: code, Message: message, Subject: target(sub), Action: sub.GetSchema().GetName(), Member: c.ID, At: at.UTC(), Original: platform.Raw(original), Inputs: map[string]json.RawMessage{}}
	for name, v := range inputs {
		fact.Inputs[name] = platform.Raw(v)
	}
	return c.RefuseBusiness(fact)
}

// business_refusal_escalation.go consolidated into this owner

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
	for key, r := range storedValues(&d.tenant.committed, "refusal", d.tenant.committed.refusals, "business") {
		if r.Business != nil {
			add(key, *r.Business)
		}
	}
	for key, raw := range storedValues(&d.tenant.committed, "answer", d.tenant.committed.answers, "business") {
		var envelope struct{ Kind string }
		if json.Unmarshal(raw, &envelope) != nil || envelope.Kind != "record-batch" {
			continue
		}
		r, _, err := resultformat.DecodeBatch(raw)
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
			key, err := acceptance.Digest([]any{g.app, g.subject, g.action, g.code, g.policy, events[start].key})
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

// conflict_refusal.go consolidated into this owner

// conflictRefusal reports err as a conflict refusal, passing an existing
// refusal through and nil as nil.
func conflictRefusal(err error) *kernel.Error {
	if err == nil {
		return nil
	}
	if refusal, ok := err.(*kernel.Error); ok {
		return refusal
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
}
