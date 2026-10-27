package platformserver

import (
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (d *stagedDecision) inputResult(app, name string, m platform.Member, body []byte, answer any, at time.Time) ([]byte, error) {
	if d.failure != nil || len(d.requests) > 0 {
		return nil, fmt.Errorf("connector input contains effects not represented by its result")
	}
	hash, err := resultformat.InputHash(d.tenant.ID, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := resultformat.Input{Version: 2, Kind: "input-result", Tenant: d.tenant.ID, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Deliveries: slices.Clone(d.deliveries), Notices: d.savedNotices(), Authorizations: slices.Clone(d.authorizations)}
	if message, ok := answer.(proto.Message); ok {
		result.Answer, err = protojson.Marshal(message)
	} else {
		result.Answer, err = json.Marshal(answer)
	}
	if err != nil {
		return nil, err
	}
	if len(d.events) > 0 {
		receipt, ok := answer.(*pb.ChangeRecord)
		if !ok || receipt.GetSubmission().GetAuthority() != app {
			return nil, fmt.Errorf("connector input has decisions without a matching answer")
		}
		result.Changes, err = d.batchResult(app, receipt.GetSubmission(), receipt, at)
		if err != nil {
			return nil, err
		}
		// The child batch owns the entire staged state; do not apply its marks,
		// notices or record images twice from the enclosing input.
		result.Deliveries, result.Notices = nil, nil
		result.Digest, err = resultformat.DigestInput(result)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if _, err := resultformat.DecodeInput(raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if d.control != nil || len(d.intents) > 0 || len(d.observations) > 0 || len(d.allocated) > 0 || len(d.operationCancels) > 0 {
		return nil, fmt.Errorf("connector input contains effects without a decision")
	}
	// A legacy input retry may read its saved receipt through a private log.
	// Permit that read only when every receipt still equals the live ledger;
	// a newly accepted decision without a published event cannot disappear.
	for ledger, staged := range d.logs {
		if !proto.Equal(ledger.LastAccepted(d.tenant.ID), staged.Last(d.tenant.ID)) {
			return nil, fmt.Errorf("connector input contains an unpublished decision")
		}
	}
	plan, err := d.savedChangePlan()
	if err != nil {
		return nil, err
	}
	result.States, result.Deliveries, result.Notices, result.Unique = plan.States, plan.Deliveries, plan.Notices, plan.Unique
	result.Rows, err = captureDirectRows(d, app, nil, nil, "connector input")
	if err != nil {
		return nil, err
	}
	result.Digest, err = resultformat.DigestInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := resultformat.DecodeInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func encodeAcceptedInputRefusal(tenant, app, name string, m platform.Member, body []byte, at time.Time, refusal *kernel.Error) ([]byte, error) {
	hash, err := resultformat.InputHash(tenant, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := resultformat.Input{Version: 2, Kind: "input-result", Tenant: tenant, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Refusal: refusal}
	result.Digest, err = resultformat.DigestInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := resultformat.DecodeInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (t *Tenant) inputAccepted(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time) (any, *kernel.Error) {
	if m.ID == "" || m.Tenant != t.ID || name == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connector input has an invalid identity")
	}
	hash, hashErr := resultformat.InputHash(t.ID, a.Manifest().ID, name, m.ID, body)
	if hashErr != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	key := "input:" + hash
	if saved := t.committed.input(a.Manifest().ID + "/" + key); len(saved) != 0 {
		return t.answerAcceptedInput(a, saved)
	}
	draft := t.newStagedDecision()
	entry := t.authorizeEntry(m, a.Manifest().ID, "input:"+name, "action", now)
	draft.authorizations = append(draft.authorizations, resultformat.AuthorizationEvidence{Request: platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: "input:" + name}, Decision: entry, Roles: slices.Clone(m.RolesIn(a.Manifest().ID))})
	answer, refusal, unsupported := decideAcceptedInput(a, draft, m, name, body, now)
	if unsupported {
		return nil, refusal
	}
	var raw []byte
	var err error
	if refusal != nil {
		raw, err = encodeAcceptedInputRefusal(t.ID, a.Manifest().ID, name, m, body, now, refusal)
	} else {
		raw, err = draft.inputResult(a.Manifest().ID, name, m, body, answer, now)
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input cannot produce a bounded accepted result")
	}
	result, err := resultformat.DecodeInput(raw)
	if err == nil {
		_, err = t.prepareAcceptedChanges(result.ChangePlan())
	}
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	principal, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = draft.flowVersions()
	}
	committed, err := t.acceptResult(Entry{App: a.Manifest().ID, Kind: "accepted-result",
		Principal: principal, Body: raw, At: now, Versions: versions}, key, hash)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return t.finishCommittedInput(a, m, name, key, hash, committed)
}

func (t *Tenant) finishCommittedInput(a platform.AcceptedInputApp, m platform.Member, name, key, hash string, committed []byte) (answer any, refusal *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("apply committed connector result: %v", failure))
			answer = nil
			refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}()
	result, applied, err := t.applyAcceptedInput(committed)
	if err != nil || result.Key != key || result.RequestHash != hash || result.Member != m.ID ||
		result.Name != name || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("committed connector input differs from request: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	t.acceptedResultApplied()
	if applied && result.Refusal == nil {
		t.audit.remember(AuditEntry{At: result.At, Member: m.ID, App: result.App, Action: "input:" + name})
		t.enqueue(result.At)
		t.changedOwner(result.App)
	}
	return t.answerAcceptedInput(a, committed)
}

func decideAcceptedInput(a platform.AcceptedInputApp, draft *stagedDecision, m platform.Member, name string, body []byte, at time.Time) (answer any, refusal *kernel.Error, unsupported bool) {
	defer func() {
		if p := recover(); p != nil {
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				panic(p)
			}
			answer = nil
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input uses an effect outside the accepted-result boundary")
			unsupported = true
		}
	}()
	answer, refusal = inputApp(draft.stateApp(a), platform.NewCaller(draft, m, a.Manifest().ID, false, false),
		name, slices.Clone(body), at)
	return answer, refusal, false
}

func (t *Tenant) answerAcceptedInput(a platform.AcceptedInputApp, raw []byte) (any, *kernel.Error) {
	result, err := resultformat.DecodeInput(raw)
	if err != nil || result.Tenant != t.ID || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("saved connector answer is incompatible: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if result.Refusal != nil {
		return nil, result.Refusal
	}
	answer, err := a.DecodeAcceptedInput(result.Answer)
	if err != nil {
		t.quarantine(fmt.Errorf("saved connector answer cannot be decoded: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return answer, nil
}

func (t *Tenant) applyAcceptedInput(raw []byte) (resultformat.Input, bool, error) {
	result, err := resultformat.DecodeInput(raw)
	if err != nil {
		return result, false, err
	}
	app, ok := t.app(result.App).(platform.AcceptedInputApp)
	if result.Tenant != t.ID || !ok || !slices.Contains(app.AcceptedInputs(), result.Name) {
		return result, false, fmt.Errorf("connector result belongs to another input")
	}
	if result.Refusal == nil {
		if _, err := app.DecodeAcceptedInput(result.Answer); err != nil {
			return result, false, fmt.Errorf("connector result has an invalid answer: %w", err)
		}
	}
	if prior := t.committed.input(result.App + "/" + result.Key); len(prior) > 0 {
		saved, err := resultformat.DecodeInput(prior)
		if err != nil || saved.Digest != result.Digest {
			return result, false, fmt.Errorf("connector input key belongs to another result")
		}
		return result, false, nil
	}
	prepared, err := t.prepareAcceptedChanges(result.ChangePlan())
	if err != nil {
		return result, false, err
	}
	if err := prepared.apply(t); err != nil {
		return result, false, err
	}
	prepared.publish(t)
	if result.Refusal != nil {
		t.refused(result.Member, result.Name, result.Refusal, result.At)
	}
	t.committed.saveInput(result.App+"/"+result.Key, raw)
	return result, true, nil
}

// accepted_requests.go consolidated into this owner

// Internal protocol requests and replies are child decisions of the input,
// never separately confirmed journal writes. They run after application
// locks are released and before the single result is planned/committed.
func (d *stagedDecision) answerRequests(now time.Time) *kernel.Error {
	for count := 0; len(d.requests) > 0; count++ {
		if count >= resultformat.MaxHops {
			unsupportedStagedEffect()
		}
		q := d.requests[0]
		d.requests = d.requests[1:]
		if err := d.answerRequest(q, now); err != nil {
			return err
		}
	}
	return d.failure
}

func (d *stagedDecision) answerRequest(q request, now time.Time) (refusal *kernel.Error) {
	end := d.tenant.begin("request "+q.Protocol+"#"+q.Action, trace.SpanContext{}, attribute.String("platform.app", q.caller.App), attribute.String("platform.target", q.Target))
	outcome := "ok"
	defer func() {
		if refusal != nil {
			outcome = refusal.Code.String()
		}
		end(outcome)
	}()
	s := q.record.GetSubmission()
	key := fmt.Sprintf("%s:%s#%d", q.caller.App, s.GetIdempotencyKey(), q.n)
	providerKey := key
	if q.Key != "" {
		providerKey = q.Key
	}
	answer := platform.Answer{Tag: q.Tag, Call: q.Target, Action: q.Action, Outcome: "accepted"}
	var ref *pb.EntityRef
	_, err := d.Attempt(func() (*pb.ChangeRecord, *kernel.Error) {
		var record *pb.ChangeRecord
		var refusal *kernel.Error
		ref, record, refusal = d.routingTenant().invoke(q.caller, q.Protocol, q.Action, q.Target,
			platform.Raw(q.Payload), providerKey, s.GetIdempotencyKey(), now, q.Provider)
		return record, refusal
	})
	if err != nil {
		answer.Outcome, answer.Code, answer.Reason = "refused", err.Code.String(), err.Message
		outcome = answer.Code
	} else {
		answer.Ref = ref.GetType() + "/" + ref.GetId()
	}
	payload, errJSON := json.Marshal(answer)
	if errJSON != nil {
		unsupportedStagedEffect()
	}
	c := d.tenant.automated(q.caller, q.caller.App)
	reply := &pb.Submission{TenantId: d.tenant.ID, PrincipalId: c.ID, Authority: s.GetAuthority(), Target: s.GetTarget(),
		Schema: &pb.SchemaRef{Name: q.Reply, Version: 1}, IdempotencyKey: "answer:" + key,
		CorrelationId: s.GetIdempotencyKey(), Payload: payload}
	if _, err := platform.Decide(c, d.tenant.app(c.App), reply, now); err != nil {
		return err
	}
	return d.failure
}

// accepted_intents.go consolidated into this owner

func (t *Tenant) validateAcceptedIntents(intents []platform.Effect) error {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	ids := map[string]bool{}
	for _, x := range intents {
		if err := checkApplicationBindings(x.Applications); err != nil {
			return err
		}
		kind, ok := strings.CutPrefix(x.Event, x.App+"/")
		id := fmt.Sprintf("%s:%s:%s:%s:%s", t.ID, x.App, kind, x.Key, x.Endpoint)
		if kind == "model" && x.Endpoint == resultformat.ModelEndpoint {
			id = fmt.Sprintf("%s:%s:model:%s", t.ID, x.App, x.Key)
		}
		if !ok || kind == "" || x.App == "" || t.app(x.App) == nil || x.Key == "" || x.Endpoint == "" ||
			x.ID != id ||
			ids[x.ID] || x.State != "pending" && x.State != "held" || x.At.IsZero() || !x.Due.Equal(x.At) ||
			x.Attempts != 0 || !x.Last.IsZero() || x.Error != "" || x.Digest != "" ||
			x.State == "held" && x.Agent == "" || slices.ContainsFunc(t.outbound, func(e *effect) bool { return e.ID == x.ID }) {
			return fmt.Errorf("accepted result has an invalid or duplicate outbound intent")
		}
		ids[x.ID] = true
	}
	return nil
}

func (t *Tenant) installAcceptedIntents(intents []platform.Effect) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, planned := range intents {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}

// accepted_approval.go consolidated into this owner

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a, ok := d.tenant.app(app).(platform.App)
	if !ok || c.Automation || c.Tenant != d.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if d.probing {
		return d.Decide(c, a, s, now)
	}
	return d.requestApproval(a, c.Member, s, now)
}

func (d *stagedDecision) requestApproval(a platform.App, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	// Permission/attribute/rule checks occur privately. Even a rule that uses
	// another app cannot leave anything in the actual request transaction.
	c := platform.NewCaller(d, m, a.Manifest().ID, false, false)
	if err := d.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, err := platform.Decide(probe, a, s, now)
		return err
	}); err != nil {
		return nil, err
	}
	owner, ok := d.tenant.app(platform.WorkApp).(platform.ApprovalOwner)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	return owner.RequestApproval(c, s, now)
}

// accepted_connectors.go consolidated into this owner

// A connector's cursor and last-seen mark belong to the same durable result
// as the decisions caused by its delivery. Before guards both fields, including
// push deliveries (which have no cursor transition).

func markFor(marks []kernel.ConnectorMark, tenant, connector string) kernel.ConnectorMark {
	for _, mark := range marks {
		if mark.Tenant == tenant && mark.Connector == connector {
			return mark
		}
	}
	return kernel.ConnectorMark{}
}

func (t *Tenant) validateAcceptedDeliveries(deliveries []resultformat.ConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	descriptors, marks := t.connectors.kernel.State()
	private := t.services.Kernel.Connectors()
	private.Restore(descriptors, marks)
	for _, delivery := range deliveries {
		if delivery.Connector == "" || delivery.DataClass == "" || delivery.At.IsZero() || delivery.Before == "" {
			return fmt.Errorf("accepted connector delivery is incomplete")
		}
		_, current := private.State()
		before, err := acceptance.Digest(markFor(current, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor differs", delivery.Connector)
		}
		if err := private.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s delivery: %w", delivery.Connector, err)
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedDeliveries(deliveries []resultformat.ConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, delivery := range deliveries {
		_, marks := t.connectors.kernel.State()
		before, err := acceptance.Digest(markFor(marks, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor changed during application", delivery.Connector)
		}
		if err := t.connectors.kernel.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s changed during application: %w", delivery.Connector, err)
		}
	}
	return nil
}
