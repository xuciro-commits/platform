package host

import (
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	source        *runtimekernel.ClaimSet
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
		a := t.App(t.owner["action:"+s.GetSchema().GetName()])
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
		if source != nil {
			if record, refusal, done := t.priorOf(a, s.GetIdempotencyKey()).SourceReplay(m.ID, s, source.deleted); done {
				return answer{record: record}, refusal
			}
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
	if refusal := t.CheckSubmission(a, m, s); refusal != nil {
		return nil, refusal
	}
	record, hash, refusal, done := t.ReplayPrior(a, s, binding)
	if done {
		return record, refusal
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
	raw, refusal, err := draft.Outcome(draft.sources(), runtimekernel.OutcomeHooks{
		Explain: func(r *kernel.Error) *kernel.Error {
			return actions.Explained(r, a, s.GetSchema().GetName(), runtimekernel.Target(s))
		},
		Claims: func(record *pb.ChangeRecord, source *runtimekernel.ClaimSet) error {
			_, err := t.Claims.Prepare(t.ID, s.GetTarget().GetType(), s.GetTarget().GetId(), record.GetChangeId(), source)
			return err
		}}, a.Manifest().ID, s, record, refusal, binding, opts.source, now)
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
	return t.FinishCommitted(a, m, hash, committed, runtimekernel.CommittedResult{Settlement: t.settlement(), App: t.App, ApplyBatch: t.applyAcceptedBatch, ApplyRecord: t.applyAcceptedResult,
		Remember: func(member string, a platform.App, s *pb.Submission, at time.Time) {
			t.AuditLog.Remember(submitted(member, a, s, at))
		},
		PublishBatch: t.publishAcceptedBatch, Publish: t.publishAccepted})
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
		draft.tenant.App(draft.tenant.owner["action:"+platform.WorkApprovalRequestSchema]) != nil {
		record, refusal = draft.approvals().Submit(a, m, s, now)
	} else {
		caller := platform.NewCaller(draft, m, a.Manifest().ID, false, automation)
		if source := draft.sourceProjection; source != nil {
			caller = caller.WithSourceProjection(source.Restore)
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
