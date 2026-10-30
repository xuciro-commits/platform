package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/systemscope"
	acceptedformat "platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"time"
)

func unknown() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA} }

func (t *Tenant) Submit(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return t.submit(m, s, now, nil, nil)
}

func (t *Tenant) submitSource(m platform.Member, s *pb.Submission, now time.Time, source *sourceSubmission) (*pb.ChangeRecord, *kernel.Error) {
	return t.submit(m, s, now, nil, source)
}

func (t *Tenant) submitDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	var issues []platform.FieldIssue
	record, err := t.submit(m, s, now, func(_ *kernel.Error, found []platform.FieldIssue) {
		issues = t.readableFieldIssues(m, found, now)
	}, nil)
	return record, err, issues
}

func (t *Tenant) submit(m platform.Member, s *pb.Submission, now time.Time, report func(*kernel.Error, []platform.FieldIssue), source *sourceSubmission, contentVersion ...string) (record *pb.ChangeRecord, err *kernel.Error) {

	if err := t.admits(m); err != nil {
		return nil, err
	}
	if t.ID != systemscope.Tenant && s.GetAuthority() == systemscope.App {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the system tenant may change system assets")
	}
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
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
		a = t.app(s.GetAuthority())
		if a == nil {
			return nil, unknown()
		}
	}
	if retired {
		key := a.Manifest().ID + "/" + s.GetIdempotencyKey()
		_, refused := t.committed.Refusal(key)
		if !refused && len(t.committed.Answer(key)) == 0 && a.AcceptedLedger().AcceptedFor(t.ID, s.GetIdempotencyKey()) == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
		}
	}
	if source != nil {
		if prior := a.AcceptedLedger().AcceptedFor(t.ID, s.GetIdempotencyKey()); prior != nil {
			previous := prior.GetSubmission()
			if previous.GetPrincipalId() != m.ID || !proto.Equal(previous.GetTarget(), s.GetTarget()) || !proto.Equal(previous.GetSchema(), s.GetSchema()) {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
			}
			return prior, nil
		}
		if prior, ok := t.committed.Refusal(a.Manifest().ID + "/" + s.GetIdempotencyKey()); ok {
			answer := prior.Error
			return nil, &answer
		}
	}
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
	var claim *preparedSourceClaim
	if source != nil {
		s, claim, err = t.prepareSourceSubmission(m, s, source, now)
		if err != nil {
			return nil, err
		}
	}
	if prior, ok := t.committed.Refusal(s.GetAuthority() + "/" + s.GetIdempotencyKey()); ok {
		hash, _ := acceptedformat.SubmissionHash(s, binding)
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
	record, err = t.submitAcceptedVersion(a, m, s, now, false, binding, schemaRefusal, nil, claim)
	if report != nil && err != nil {
		if prior, ok := t.committed.Refusal(a.Manifest().ID + "/" + s.GetIdempotencyKey()); ok {
			hash, _ := acceptedformat.SubmissionHash(s, binding)
			if prior.RequestHash == hash {
				report(err, prior.Issues)
			}
		}
	}
	return record, err
}

func (t *Tenant) submitAccepted(a platform.App, m platform.Member, s *pb.Submission, now time.Time, automation bool, input ...*preparedOperationInput) (record *pb.ChangeRecord, refusal *kernel.Error) {
	return t.submitAcceptedVersion(a, m, t.currentSubmissionSchema(a, s), now, automation, nil, nil, nil, nil, input...)
}

func (t *Tenant) submitAcceptedVersion(a platform.App, m platform.Member, s *pb.Submission, now time.Time, automation bool, binding *platform.SchemaVersion, schemaRefusal *kernel.Error, observation *platform.ObservationDecisionEvidence, source *preparedSourceClaim, input ...*preparedOperationInput) (record *pb.ChangeRecord, refusal *kernel.Error) {
	if t.businessStopped() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business access is stopped for maintenance")
	}
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
	if saved := t.committed.Answer(a.Manifest().ID + "/" + s.GetIdempotencyKey()); len(saved) > 0 {
		result, receipt, err := acceptedformat.DecodeBatch(saved)
		hash, hashErr := acceptedformat.SubmissionHash(s, binding)
		if err != nil || hashErr != nil || result.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return receipt, nil
	}
	if prior := ledger.AcceptedFor(t.ID, s.GetIdempotencyKey()); prior != nil {
		if !proto.Equal(prior.GetSubmission(), s) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		saved, bound := t.committed.SchemaVersion(a.Manifest().ID + "/" + s.GetIdempotencyKey())
		if bound != (binding != nil) || binding != nil && saved != *binding {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return prior, nil
	}
	hash, err := acceptedformat.SubmissionHash(s, binding)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission cannot be encoded")
	}
	if prior, ok := t.committed.Refusal(a.Manifest().ID + "/" + s.GetIdempotencyKey()); ok {
		if prior.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		answer := prior.Error
		return nil, &answer
	}
	if s.GetSchema().GetName() == controlowner.SchemaCredentialSave {
		if _, failure := controlowner.DecodeCredentialWrite(t.ID, s.GetPayload()); failure != nil {
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
		raw, err = acceptedformat.EncodeBusinessRefusal(s, now, refusal, binding, businessFact, draft.inputIssues[refusal]...)
	} else {
		raw, err = draft.sealResult(a.Manifest().ID, s, record, now)
		if err == nil && source != nil {
			raw, err = acceptedformat.AttachSourceClaims(raw, source.origin, source.claims)
			if err == nil {
				_, err = t.sourceFacts(s.GetTarget().GetType(), s.GetTarget().GetId(), record.GetChangeId(), source)
			}
		}
		if err != nil {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
			var preparationRefusal *kernel.Error
			if errors.As(err, &preparationRefusal) {
				refusal = preparationRefusal
			}
			var budget *acceptedformat.ResultBudgetError
			if errors.As(err, &budget) {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Accepted result needs {bytes} bytes; limit is {limit} bytes / 接受结果超出字节预算", budget.Bytes, budget.Limit)
			}
			raw, err = acceptedformat.EncodeSchemaRefusal(s, now, refusal, binding)
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
		saved, sub, err := acceptedformat.DecodeRefusal(committed)
		if err != nil || saved.App != a.Manifest().ID || saved.Tenant != t.ID ||
			saved.RequestHash != hash || sub.GetPrincipalId() != m.ID {
			t.quarantine(fmt.Errorf("committed refusal differs from the accepted request: %v", err))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		t.committed.SaveRefusal(saved.App+"/"+sub.GetIdempotencyKey(), saved)
		t.acceptedResultApplied()
		t.changedOwner(saved.App)
		answer := saved.Error
		return nil, &answer
	}
	if envelope.Kind == "record-batch" {
		saved, receipt, err := acceptedformat.DecodeBatch(committed)
		sub, subErr := acceptedformat.BatchSubmission(saved, receipt)
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
			t.audit.Remember(submitted(m.ID, a, sub, saved.At))
			t.publishAcceptedBatch(saved)
			t.changedOwner(saved.App)
		}
		t.enqueue(saved.At)
		return receipt, nil
	}
	saved, receipt, err := acceptedformat.DecodeRecord(committed)
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
		t.audit.Remember(submitted(m.ID, a, receipt.GetSubmission(), saved.At))
		t.publishAccepted(platform.Event{App: a.Manifest().ID, Record: receipt, Changed: saved.Event.Changed}, saved.Event, saved.Version)
		t.changedOwner(saved.App)
	}
	t.enqueue(saved.At)
	return receipt, nil
}

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
