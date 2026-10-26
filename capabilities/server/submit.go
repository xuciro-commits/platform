package platformserver

// The submit pipeline: one submission becomes one accepted result or one
// durable refusal. Tenant.Submit is the door; submitAccepted applies what the
// app's decision accepted; explained names refusals that arrive bare.

import (
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/systemscope"
	"platformserver/platform"
	"time"
)

func unknown() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA} }

// Submit routes a submission to the app declaring its action and records it when accepted.
func (t *Tenant) Submit(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return t.submit(m, s, now, nil)
}

// submitDiagnosed captures one invocation, including its original durable refusal.
func (t *Tenant) submitDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	var issues []platform.FieldIssue
	record, err := t.submit(m, s, now, func(_ *kernel.Error, found []platform.FieldIssue) {
		issues = t.readableFieldIssues(m, found, now)
	})
	return record, err, issues
}

func (t *Tenant) submit(m platform.Member, s *pb.Submission, now time.Time, report func(*kernel.Error, []platform.FieldIssue), contentVersion ...string) (record *pb.ChangeRecord, err *kernel.Error) {

	if err := t.admits(m); err != nil {
		return nil, err
	}
	if t.ID != systemscope.Tenant && s.GetAuthority() == systemscope.App {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the system tenant may change system assets")
	}
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	// Signal dead-letter replay prepares its retained batch (outside I/O)
	// before the decision path; business-action replay continues inside the
	// Flow app's own submission handling.
	// flow.SchemaFlowReplay, named literally so the runtime never imports the
	// concrete Flow app (the boundary check): the constant lives in apps/flow/flow.go.
	if s.GetSchema().GetName() == "flow.dead-letter.replay" {
		record, refusal, handled := t.prepareContinuousDeadLetterReplay(m, s, now, contentVersion...)
		if handled {
			if refusal != nil && report != nil {
				report(refusal, nil)
			}
			return record, refusal
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if refusal := t.recordReadRefusal(); refusal != nil {
		return nil, refusal
	}
	a := t.owner["action:"+s.GetSchema().GetName()]
	retired := a == nil
	if a == nil {
		// Retirement removes an exposed action, not its original receipts.
		a = t.app(s.GetAuthority())
		if a == nil {
			return nil, unknown()
		}
	}
	if retired {
		key := a.Manifest().ID + "/" + s.GetIdempotencyKey()
		_, refused := t.committed.refusal(key)
		if !refused && len(t.committed.answer(key)) == 0 && a.AcceptedLedger().AcceptedFor(t.ID, s.GetIdempotencyKey()) == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
		}
	}
	// A builder role held only through projects is bounded to their assets,
	// from this submission's target under the tenant lock, before any app decides it.
	if err = t.delegatedBound(m, s); err != nil {
		return nil, err
	}
	var binding *platform.SchemaVersion
	var schemaRefusal *kernel.Error
	if len(contentVersion) > 0 {
		binding, schemaRefusal = t.submissionSchemaVersion(a, s, contentVersion[0])
		if schemaRefusal != nil && schemaRefusal.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
			return nil, schemaRefusal
		}
	} else {
		s = t.currentSubmissionSchema(a, s)
	}
	if prior, ok := t.committed.refusal(s.GetAuthority() + "/" + s.GetIdempotencyKey()); ok {
		hash, _ := resultformat.SubmissionHash(s, binding)
		if hash != prior.RequestHash || s.GetPrincipalId() != m.ID {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		if report != nil {
			report(&prior.Error, prior.Issues)
		}
		answer := prior.Error
		return nil, &answer
	}
	end := t.begin("submit "+s.GetSchema().GetName(), trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID),
		attribute.String("platform.target", target(s)), attribute.String("platform.member", m.ID))
	defer func() { end(outcomeOf(err)) }()
	record, err = t.submitAcceptedVersion(a, m, s, now, false, binding, schemaRefusal, nil)
	if report != nil && err != nil {
		if prior, ok := t.committed.refusal(a.Manifest().ID + "/" + s.GetIdempotencyKey()); ok {
			hash, _ := resultformat.SubmissionHash(s, binding)
			if prior.RequestHash == hash {
				report(err, prior.Issues)
			}
		}
	}
	return record, err
}

func (t *Tenant) submitAccepted(a platform.App, m platform.Member, s *pb.Submission, now time.Time, automation bool, input ...*preparedOperationInput) (record *pb.ChangeRecord, refusal *kernel.Error) {
	return t.submitAcceptedVersion(a, m, t.currentSubmissionSchema(a, s), now, automation, nil, nil, nil, input...)
}

func (t *Tenant) submitAcceptedVersion(a platform.App, m platform.Member, s *pb.Submission, now time.Time, automation bool, binding *platform.SchemaVersion, schemaRefusal *kernel.Error, observation *resultformat.ObservationDecisionEvidence, input ...*preparedOperationInput) (record *pb.ChangeRecord, refusal *kernel.Error) {
	if t.businessStopped() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business access is stopped for maintenance")
	}
	// A malformed transport identity cannot reserve a durable key for another
	// tenant, authority or member. The kernel still supplies its ordinary
	// refusal, but no result under this tenant can represent that submission.
	if s.GetTenantId() != t.ID || s.GetPrincipalId() != m.ID {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if s.GetAuthority() != a.Manifest().ID {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY}
	}
	if s.GetIdempotencyKey() == "" || s.GetTarget() == nil || s.GetSchema() == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission has an invalid identity")
	}
	ledger := a.AcceptedLedger()
	if saved := t.committed.answer(a.Manifest().ID + "/" + s.GetIdempotencyKey()); len(saved) > 0 {
		result, receipt, err := resultformat.DecodeBatch(saved)
		hash, hashErr := resultformat.SubmissionHash(s, binding)
		if err != nil || hashErr != nil || result.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return receipt, nil
	}
	if prior := ledger.AcceptedFor(t.ID, s.GetIdempotencyKey()); prior != nil {
		if !proto.Equal(prior.GetSubmission(), s) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		saved, bound := t.committed.schemaVersion(a.Manifest().ID + "/" + s.GetIdempotencyKey())
		if bound != (binding != nil) || binding != nil && saved != *binding {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return prior, nil
	}
	hash, err := resultformat.SubmissionHash(s, binding)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission cannot be encoded")
	}
	if prior, ok := t.committed.refusal(a.Manifest().ID + "/" + s.GetIdempotencyKey()); ok {
		if prior.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		answer := prior.Error
		return nil, &answer
	}
	// Reject plaintext or invalid envelopes before a durable refusal captures input.
	if s.GetSchema().GetName() == SchemaCredentialSave {
		if _, failure := decodeCredentialWrite(t.ID, s.GetPayload()); failure != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid credential configuration or encrypted value")
		}
	}

	if err := t.checkBusinessCorrection(m, s, binding); err != nil {
		return nil, err
	}
	if m.Agent && t.suspended(m.ID) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", m.ID)
	}
	draft := t.newStagedDecision()
	draft.observationInput = observation
	draft.schemaVersion = binding
	if len(input) > 0 {
		draft.operationInputs = make(map[string][]*preparedOperationInput, len(input))
		for _, prepared := range input {
			if prepared != nil {
				draft.operationInputs[prepared.requestHash] = append(draft.operationInputs[prepared.requestHash], prepared)
			}
		}
	}
	if schemaRefusal != nil {
		refusal = schemaRefusal
	} else {
		record, refusal = decideAcceptedAs(a, draft, m, s, now, automation)
	}
	if draft.failure != nil {
		refusal = draft.failure
		record = nil
	}
	if refusal == nil && pendingOperationInputs(draft.operationInputs) > 0 {
		record = nil
		refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A prepared Compute input was not consumed by this accepted Flow decision")
	}
	var raw []byte
	if refusal != nil {
		refusal = explained(refusal, a, s.GetSchema().GetName(), target(s))
		fact, business := draft.businessRefusals[refusal]
		var businessFact *platform.BusinessRefusalFact
		if business {
			businessFact = &fact
		}
		raw, err = resultformat.EncodeBusinessRefusal(s, now, refusal, binding, businessFact, draft.inputIssues[refusal]...)
	} else {
		raw, err = draft.sealResult(a.Manifest().ID, s, record, now)
		if err != nil {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
			var budget *resultformat.ResultBudgetError
			if errors.As(err, &budget) {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Accepted result needs {bytes} bytes; limit is {limit} bytes / 接受结果超出字节预算", budget.Bytes, budget.Limit)
			}
			raw, err = resultformat.EncodeSchemaRefusal(s, now, refusal, binding)
		}
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
	}
	member, _ := json.Marshal(m)
	committed, err := t.acceptResult(Entry{App: a.Manifest().ID, Kind: "accepted-result",
		Principal: member, Body: raw, At: now}, s.GetIdempotencyKey(), hash)
	if err != nil {
		if err == errAcceptedConflict {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted result could not be committed")
	}
	return t.finishCommittedResult(a, m, hash, committed)
}

func (t *Tenant) finishCommittedResult(a platform.App, m platform.Member, hash string, committed []byte) (record *pb.ChangeRecord, refusal *kernel.Error) {
	// Once committed, even an unexpected application panic is tenant-local:
	// never answer from a possibly half-applied memory image or take down the
	// other tenants. A repaired journal requires a fresh tenant generation.
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("apply committed result: %v", failure))
			record = nil
			refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}()
	var envelope struct{ Kind string }
	if err := json.Unmarshal(committed, &envelope); err != nil {
		t.quarantine(fmt.Errorf("committed result is not an envelope: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if envelope.Kind == "refusal" {
		saved, sub, err := resultformat.DecodeRefusal(committed)
		if err != nil || saved.App != a.Manifest().ID || saved.Tenant != t.ID ||
			saved.RequestHash != hash || sub.GetPrincipalId() != m.ID {
			t.quarantine(fmt.Errorf("committed refusal differs from the accepted request: %v", err))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		t.committed.saveRefusal(saved.App+"/"+sub.GetIdempotencyKey(), saved)
		t.acceptedResultApplied()
		t.changedOwner(saved.App)
		answer := saved.Error
		return nil, &answer
	}
	if envelope.Kind == "record-batch" {
		saved, receipt, err := resultformat.DecodeBatch(committed)
		sub, subErr := resultformat.BatchSubmission(saved, receipt)
		if err != nil || saved.App != a.Manifest().ID || saved.Tenant != t.ID ||
			saved.RequestHash != hash || subErr != nil || sub.GetPrincipalId() != m.ID {
			t.quarantine(fmt.Errorf("committed batch differs from the accepted request: %v", err))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		applied, err := t.applyAcceptedBatch(a.AcceptedLedger(), committed)
		if err != nil {
			t.quarantine(fmt.Errorf("committed record batch could not be applied: %w", err))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		t.acceptedResultApplied()
		if applied {
			t.audit.remember(submitted(m.ID, a, sub, saved.At))
			t.publishAcceptedBatch(saved)
			t.changedOwner(saved.App)
		}
		t.enqueue(saved.At)
		return receipt, nil
	}
	saved, receipt, err := resultformat.DecodeRecord(committed)
	if err != nil || saved.App != a.Manifest().ID || saved.Tenant != t.ID ||
		saved.RequestHash != hash || receipt.GetSubmission().GetPrincipalId() != m.ID {
		t.quarantine(fmt.Errorf("committed result differs from the accepted request: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	for _, subscriber := range saved.Event.Subscribers {
		if t.app(subscriber) == nil {
			t.quarantine(fmt.Errorf("committed result requires missing subscriber %s", subscriber))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}
	applied, err := t.applyAcceptedResult(a.AcceptedLedger(), committed)
	if err != nil {
		t.quarantine(fmt.Errorf("committed accepted result could not be applied: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	t.acceptedResultApplied()
	if saved.At.IsZero() {
		saved.At = receipt.GetRecordedTime().AsTime()
	}
	if applied {
		t.audit.remember(submitted(m.ID, a, receipt.GetSubmission(), saved.At))
		t.publishAccepted(platform.Event{App: a.Manifest().ID, Record: receipt, Changed: saved.Event.Changed}, saved.Event, saved.Version)
		t.changedOwner(saved.App)
	}
	t.enqueue(saved.At)
	return receipt, nil
}

// Only decision evaluation may turn an unsupported-effect panic into a
// refusal. A panic after the journal commit must escape so the tenant stops.
func decideAccepted(a platform.App, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time) (record *pb.ChangeRecord, refusal *kernel.Error) {
	return decideAcceptedAs(a, draft, m, s, now, false)
}

func decideAcceptedAs(a platform.App, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time, automation bool) (record *pb.ChangeRecord, refusal *kernel.Error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stagedEffectPanic); !ok {
				panic(p)
			}
			record = nil
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action uses an effect outside the accepted-result boundary")
		}
	}()
	if declared, _ := a.Manifest().Actions.Action(s.GetSchema().GetName()); declared.Approval != nil &&
		draft.tenant.owner["action:"+platform.WorkApprovalRequestSchema] != nil {
		record, refusal = draft.requestApproval(a, m, s, now)
	} else {
		record, refusal = platform.Decide(platform.NewCaller(draft, m, a.Manifest().ID, false, automation), a, s, now)
	}
	if refusal == nil {
		refusal = draft.answerRequests(now)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	if refusal == nil {
		refusal = draft.checkFinalConstraints()
	}
	return record, refusal
}

// explained gives a refusal an app returned without saying why the reason its
// code implies, naming the action and its target, so that every refusal a
// person, an agent or another app reads says something.
// An app's own reason, through platform.Refuse, is always better; the known
// apps that still refuse bare are counted in scripts/escapes.sh (#129).
func explained(err *kernel.Error, a platform.App, schema, target string) *kernel.Error {
	if err == nil || err.Message != "" {
		return err
	}
	action := schema
	if declared, ok := a.Manifest().Actions.Action(schema); ok && declared.Title != "" {
		action = declared.Title
	}
	text := map[pb.ErrorCode]string{
		pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:     "{action} was refused: a value is missing or out of range",
		pb.ErrorCode_ERROR_CODE_CONFLICT:             "{action} was refused: {target} is not in a state that allows it, or changed since it was read",
		pb.ErrorCode_ERROR_CODE_NOT_FOUND:            "{action} was refused: {target} does not exist, or you may not see it",
		pb.ErrorCode_ERROR_CODE_POLICY_DENIED:        "{action} was refused: you may not do it to {target}",
		pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA:       "{action} is not an action here",
		pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT: "{action} was sent before with other values",
		pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE:    "{action} names a record that does not exist",
		pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY:        "{action} was refused: another authority decides {target}",
	}[err.Code]
	if text == "" {
		return err
	}
	return platform.Refuse(err.Code, text, action, target)
}

func outcomeOf(err *kernel.Error) string {
	if err != nil {
		return err.Code.String()
	}
	return "ok"
}
