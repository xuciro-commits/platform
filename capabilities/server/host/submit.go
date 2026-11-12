package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	controlowner "platformserver/services/security/control"
	"time"
)

func (t *Tenant) priorOf(a platform.App, key string) runtimekernel.Prior {
	return t.Committed.Prior(a.Manifest().ID, a.AcceptedLedger(), t.ID, key)
}

func unknown() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA} }

type submitOptions struct {
	report         func(*kernel.Error, []platform.FieldIssue)
	source         *sourceSubmission
	versioned      bool
	contentVersion string
}

type acceptOptions struct {
	automation    bool
	binding       *platform.SchemaVersion
	schemaRefusal *kernel.Error
	observation   *platform.ObservationDecisionEvidence
	source        *preparedSourceClaim
	before        func(*stagedDecision) *kernel.Error
	inputs        []*preparedOperationInput
}

func (t *Tenant) Submit(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return t.submit(m, s, now, submitOptions{})
}

func (t *Tenant) submitDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	var issues []platform.FieldIssue
	record, err := t.submit(m, s, now, submitOptions{report: func(_ *kernel.Error, found []platform.FieldIssue) { issues = found }})
	return record, err, issues
}

func (t *Tenant) submit(m platform.Member, s *pb.Submission, now time.Time, opts submitOptions) (*pb.ChangeRecord, *kernel.Error) {
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if s.GetSchema().GetName() == "flow.dead-letter.replay" {
		record, refusal, handled := t.prepareContinuousDeadLetterReplay(m, s, now, opts)
		if handled {
			if refusal != nil && opts.report != nil {
				opts.report(refusal, nil)
			}
			return record, refusal
		}
	}
	type answer struct {
		record *pb.ChangeRecord
		issues []platform.FieldIssue
	}
	result, refusal := decideOnImage(t, context.Background(), "submit", func(t *Tenant) (out answer, err *kernel.Error) {
		if refusal := t.recordReadRefusal(); refusal != nil {
			return out, refusal
		}
		a := t.owner["action:"+s.GetSchema().GetName()]
		if a == nil {
			a = t.App(s.GetAuthority())
			if a == nil {
				return out, unknown()
			}
			if !t.priorOf(a, s.GetIdempotencyKey()).Known() {
				return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
			}
		}
		source := opts.source
		if prior := t.priorOf(a, s.GetIdempotencyKey()); source != nil && prior.Record != nil {
			previous := prior.Record.GetSubmission()
			if previous.GetPrincipalId() != m.ID || !proto.Equal(previous.GetTarget(), s.GetTarget()) || !proto.Equal(previous.GetSchema(), s.GetSchema()) && !source.deleted {
				return out, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
			}
			return answer{record: prior.Record}, nil
		} else if source != nil && prior.Refusal != nil {
			saved := prior.Refusal.Error
			return out, &saved
		}
		if err = t.delegatedBound(m, s); err != nil {
			return out, err
		}
		decision := acceptOptions{}
		if opts.versioned {
			decision.binding, decision.schemaRefusal = t.submissionSchemaVersion(a, s, opts.contentVersion)
			if decision.schemaRefusal != nil && decision.schemaRefusal.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
				return out, decision.schemaRefusal
			}
		} else {
			s = t.currentSubmissionSchema(a, s)
		}
		if source != nil {
			s, decision.source, err = t.prepareSourceSubmission(m, s, source, now)
			if err != nil {
				return out, err
			}
		}
		end := t.begin("submit "+s.GetSchema().GetName(), trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID),
			attribute.String("platform.target", runtimekernel.Target(s)), attribute.String("platform.member", m.ID))
		defer func() { end(outcomeOf(err)) }()
		out.record, err = t.accept(a, m, s, now, decision)
		if err == nil {
			return out, nil
		}
		if t.preparedResult != nil && len(t.preparedResult.entry.Body) > 0 {
			out.issues = t.readableFieldIssues(m, t.preparedResult.issues, now)
		} else if prior, ok := t.Committed.Refusal(a.Manifest().ID + "/" + s.GetIdempotencyKey()); ok {
			if hash, _ := acceptedformat.SubmissionHash(s, decision.binding); prior.RequestHash == hash {
				out.issues = t.readableFieldIssues(m, prior.Issues, now)
			}
		}
		return out, err
	}, func(t *Tenant, candidate preparedAccepted, value answer, refusal *kernel.Error) (answer, *kernel.Error) {
		value.record, refusal = t.finishPreparedSubmission(m, candidate, value.record, refusal)
		return value, refusal
	})
	if refusal != nil && opts.report != nil {
		opts.report(refusal, result.issues)
	}
	return result.record, refusal
}

func (t *Tenant) accept(a platform.App, m platform.Member, s *pb.Submission, now time.Time, opts acceptOptions) (record *pb.ChangeRecord, refusal *kernel.Error) {
	binding := opts.binding
	if binding == nil {
		s = t.currentSubmissionSchema(a, s)
	}
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
	prior := t.priorOf(a, s.GetIdempotencyKey())
	if len(prior.Answer) > 0 {
		result, receipt, err := acceptedformat.DecodeBatch(prior.Answer)
		hash, hashErr := acceptedformat.SubmissionHash(s, binding)
		if err != nil || hashErr != nil || result.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return receipt, nil
	}
	if prior.Record != nil {
		if !proto.Equal(prior.Record.GetSubmission(), s) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		saved, bound := t.Committed.SchemaVersion(a.Manifest().ID + "/" + s.GetIdempotencyKey())
		if bound != (binding != nil) || binding != nil && saved != *binding {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return prior.Record, nil
	}
	hash, err := acceptedformat.SubmissionHash(s, binding)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission cannot be encoded")
	}
	if prior.Refusal != nil {
		if prior.Refusal.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		saved := prior.Refusal.Error
		return nil, &saved
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
	draft.sourceProjection = opts.source
	draft.ObservationInput = opts.observation
	draft.SchemaVersion = binding
	if len(opts.inputs) > 0 {
		draft.operationInputs = make(map[string][]*preparedOperationInput, len(opts.inputs))
		for _, prepared := range opts.inputs {
			if prepared != nil {
				draft.operationInputs[prepared.requestHash] = append(draft.operationInputs[prepared.requestHash], prepared)
			}
		}
	}
	if opts.schemaRefusal != nil {
		refusal = opts.schemaRefusal
	} else {
		if opts.before != nil {
			refusal = opts.before(draft)
		}
		if refusal == nil {
			record, refusal = decideAcceptedAs(a, draft, m, s, now, opts.automation)
		}
	}
	if draft.Failure != nil {
		refusal = draft.Failure
		record = nil
	}
	if refusal == nil && pendingOperationInputs(draft.operationInputs) > 0 {
		record = nil
		refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A prepared Compute input was not consumed by this accepted Flow decision")
	}
	var raw []byte
	if refusal != nil {
		refusal = actions.Explained(refusal, a, s.GetSchema().GetName(), runtimekernel.Target(s))
		fact, business := draft.BusinessRefusals[refusal]
		var businessFact *platform.BusinessRefusalFact
		if business {
			businessFact = &fact
		}
		raw, err = acceptedformat.EncodeBusinessRefusal(s, now, refusal, binding, businessFact, draft.InputIssues[refusal]...)
	} else {
		raw, err = draft.SealResult(draft.sources(), a.Manifest().ID, s, record, now)
		if err == nil && opts.source != nil {
			raw, err = acceptedformat.AttachSourceClaims(raw, opts.source.origin, opts.source.claims)
			if err == nil {
				_, err = t.sourceFacts(s.GetTarget().GetType(), s.GetTarget().GetId(), record.GetChangeId(), opts.source)
			}
		}
		if err != nil {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The action could not be accepted: {reason}", err.Error())
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
	entry := Entry{App: a.Manifest().ID, Kind: "accepted-result", Principal: member, Body: raw, At: now}
	if t.preparedResult != nil {
		*t.preparedResult = preparedAccepted{entry: entry, key: s.GetIdempotencyKey(), hash: hash, issues: draft.InputIssues[refusal], uploads: t.preparedResult.uploads}
		return record, refusal
	}
	committed, err := t.acceptResult(entry, s.GetIdempotencyKey(), hash)
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
		t.Committed.SaveRefusal(saved.App+"/"+sub.GetIdempotencyKey(), saved)
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
		if t.App(subscriber) == nil {
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
		t.publishAccepted(platform.Event{App: a.Manifest().ID, Record: receipt, Changed: saved.Event.Changed}, saved.Event)
		t.changedOwner(saved.App)
	}
	t.enqueue(saved.At)
	return receipt, nil
}

func decideAcceptedAs(a platform.App, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time, automation bool) (record *pb.ChangeRecord, refusal *kernel.Error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stagedEffectPanic); !ok {
				panic(p)
			}
			record = nil
			refusal = platform.RefuseOutsideAcceptedBoundary()
		}
	}()
	if declared, _ := a.Manifest().Actions.Action(s.GetSchema().GetName()); declared.Approval != nil &&
		draft.tenant.owner["action:"+platform.WorkApprovalRequestSchema] != nil {
		record, refusal = draft.requestApproval(a, m, s, now)
	} else {
		caller := platform.NewCaller(draft, m, a.Manifest().ID, false, automation)
		if source := draft.sourceProjection; source != nil {
			caller = caller.WithSourceProjection(source.restore)
		}
		record, refusal = platform.Decide(caller, a, s, now)
	}
	if refusal == nil {
		refusal = draft.answerRequests(now)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	if refusal == nil {
		refusal = draft.CheckFinalConstraints()
	}
	return record, refusal
}

func outcomeOf(err *kernel.Error) string {
	if err != nil {
		return err.Code.String()
	}
	return "ok"
}

func (t *Tenant) finishPreparedSubmission(m platform.Member, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	if len(candidate.entry.Body) == 0 {
		return record, refusal
	}
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		if err == errAcceptedConflict {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted result could not be committed")
	}
	return t.finishCommittedResult(t.App(candidate.entry.App), m, candidate.hash, committed)
}

func (t *Tenant) submitBackground(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return decideOnImage(t, context.Background(), "submit:background", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		app := t.App(s.GetAuthority())
		if app == nil {
			return nil, unknown()
		}
		return t.accept(app, m, s, now, acceptOptions{automation: true})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		return t.finishPreparedSubmission(m, candidate, record, refusal)
	})
}
