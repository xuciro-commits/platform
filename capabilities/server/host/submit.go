package host

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	automation "platformserver/services/automate/runtime"
	dataowner "platformserver/services/data"
	releaseowner "platformserver/services/devops/releases"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func unknown() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA} }

type submitOptions struct {
	report         func(*kernel.Error, []platform.FieldIssue)
	source         *dataowner.SourceApplication
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
			if !t.PriorOf(a, s.GetIdempotencyKey()).Known() {
				return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
			}
		}
		source := opts.source
		if source != nil {
			if record, refusal, done := t.PriorOf(a, s.GetIdempotencyKey()).SourceReplay(m.ID, s, source.Deleted); done {
				return answer{record: record}, refusal
			}
		}
		if err = t.delegatedBound(m, s); err != nil {
			return out, err
		}
		decision := acceptOptions{}
		if opts.versioned {
			decision.binding, decision.schemaRefusal = t.SchemaBinding(a, s, opts.contentVersion, t.actionSchemaVersion)
			if decision.schemaRefusal != nil && decision.schemaRefusal.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
				return out, decision.schemaRefusal
			}
		} else {
			s = t.CurrentSchema(a, s)
		}
		if source != nil {
			prepared, refusal := t.sourcePreparation().Prepare(m, s, source, now)
			if refusal != nil {
				return out, refusal
			}
			s, decision.source = prepared.Submission, &runtimekernel.ClaimSet{Origin: prepared.Origin, Claims: prepared.Claims, Restore: prepared.Restore}
		}
		end := t.begin("submit "+s.GetSchema().GetName(), trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID),
			attribute.String("platform.target", runtimekernel.Target(s)), attribute.String("platform.member", m.ID))
		defer func() { end(runtimekernel.OutcomeOf(err)) }()
		out.record, err = t.accept(a, m, s, now, decision)
		if err == nil {
			return out, nil
		}
		if t.preparedResult != nil && len(t.preparedResult.entry.Body) > 0 {
			out.issues = t.readableFieldIssues(m, t.preparedResult.issues, now)
		} else if prior, ok := t.Committed.RefusedIssues(a.Manifest().ID, s, decision.binding); ok {
			out.issues = t.readableFieldIssues(m, prior, now)
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
		s = t.CurrentSchema(a, s)
	}
	if t.businessStopped() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business access is stopped for maintenance")
	}
	if refusal := t.CheckSubmission(m, s); refusal != nil {
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
	draft.operationInputs = automation.GroupOperationInputs(opts.inputs)
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
	if refusal == nil && automation.PendingOperationInputs(draft.operationInputs) > 0 {
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
		return nil, runtimekernel.CommitRefusal(err)
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

func (t *Tenant) finishPreparedSubmission(m platform.Member, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	if len(candidate.entry.Body) == 0 {
		return record, refusal
	}
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		return nil, runtimekernel.CommitRefusal(err)
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

func (t *Tenant) applyAcceptedBatch(l *platform.Ledger, raw []byte) (bool, error) {
	return t.ApplyBatch(l, raw, t.batchApply())
}

func (t *Tenant) batchApply() runtimekernel.BatchApply {
	h := runtimekernel.BatchApply{App: t.App, OwnerLedger: runtimekernel.LedgerOf(t.App), CheckBindings: build.CheckRunApplicationBindings, DefinitionType: build.AppType,
		Claims: t.Claims.BatchPrepare(t.ID),
		Rows: func(draft *records.Store, result acceptedformat.Batch) error {
			return actions.CallbackRows{
				Answerer: func(app string) bool { return t.answerers[app] != nil },
				Pending:  func(id string) platform.Effect { queued, _ := t.outbound.Get(id); return queued.Effect },
				Prepare:  t.prepareAcceptedDirectRows,
			}.Apply(draft, result.DirectRows, result.At)
		},
		Install: runtimekernel.BatchInstall{
			Definitions: func(draft *records.Store, definitions []platform.ReleaseInstallation) (*records.Store, func() error, error) {
				installation, err := t.prepareBatchDefinitions(draft, definitions)
				if err != nil {
					return nil, nil, err
				}
				return installation.Projection, func() error { return t.installReleaseDefinitions(installation, definitions) }, nil
			},
			Publication: t.installPublicationLinkConstraint}.Install,
		Installed: t.Claims.BatchInstalled(t.ID),
	}
	return t.batchState().Hooks(h)
}

func (t *Tenant) batchState() runtimekernel.BatchState {
	return runtimekernel.BatchState{App: t.App, States: t.states, Notices: &t.Notices, Sequences: &t.Sequences,
		CheckControl: t.validateAcceptedControl, ApplyControl: t.applyAcceptedControl, CheckIntents: t.validateAcceptedIntents, InstallIntents: t.installAcceptedIntents,
		ApplyNotices: t.applyAcceptedNotices, CheckOperations: t.operationCancellations().Validate, ApplyOperations: t.operationCancellations().Apply}
}

func (t *Tenant) installReleaseDefinitions(installed *Tenant, definitions []platform.ReleaseInstallation) error {
	t.setDefinitions(installed.definitions)
	t.owner = maps.Clone(installed.owner)
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return err
	}
	return releaseowner.ApplyInstallations(registry, definitions)
}

func (t *Tenant) publishAcceptedBatch(result acceptedformat.Batch) {
	runtimekernel.PublishBatch(result, t.publishAccepted)
}

func (t *Tenant) applyAcceptedResult(l *platform.Ledger, raw []byte) (bool, error) {
	return t.ApplyRecordResult(l, raw, runtimekernel.RecordApply{App: t.App, CheckBindings: build.CheckRunApplicationBindings, DefinitionType: build.AppType,
		Claims: func(result acceptedformat.Record, receipt *pb.ChangeRecord) (func() error, error) {
			return t.Claims.RecordClaims(t.ID, result, receipt)
		}})
}

func (t *Tenant) acceptResult(entry Entry, key, hash string) ([]byte, error) {
	if t.quarantined() {
		return nil, fmt.Errorf("tenant %s is stopped", t.ID)
	}
	if t.readError() != nil {
		return nil, t.readError()
	}
	return t.Accept(entry, key, hash, storageDelta, t.quarantine)
}

func (t *Tenant) acceptedResultApplied() {
	t.ResultApplied(t.currentReadScopes)
	if t.currentReadScopes {
		t.closeRecordRead()
		t.openRecordRead(context.Background())
	}
}

func (t *Tenant) settlement() runtimekernel.Settlement {
	return runtimekernel.Settlement{
		Quarantine:        t.quarantine,
		Applied:           t.acceptedResultApplied,
		Enqueue:           t.enqueue,
		ChangedOwner:      t.changedOwner,
		OperationsChanged: t.operationsChanged,
	}
}

func (t *Tenant) changeApply() runtimekernel.ChangeApply {
	return t.batchState().Changes(t.batchApply())
}

func (t *Tenant) publishAcceptedChanges(p *runtimekernel.PreparedChanges) {
	if p.Child != nil {
		t.publishAcceptedBatch(p.Child.Result)
	}
}

func (t *Tenant) prepareAcceptedDirectRows(rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error) {
	return runtimekernel.ApplyDirectRows(t.Projection, rows, member, at, build.CheckRunApplicationBindings)
}

func (t *Tenant) inputAccepted(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time) (any, *kernel.Error) {
	return t.AcceptInput(a, m, name, body, now, runtimekernel.InputAcceptance{CommittedInput: t.committedInput(),
		Draft: func() runtimekernel.InputDraft {
			draft := t.newStagedDecision()
			return runtimekernel.InputDraft{Decision: &draft.Decision, Sources: draft.sources,
				Decide: func(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, at time.Time) (any, *kernel.Error, bool) {
					return decideAcceptedInput(a, draft, m, name, body, at)
				}}
		},
		Authorize: t.authorizeEntry,
		Prepare: func(plan acceptedformat.ChangePlan) error {
			_, err := t.PrepareChanges(plan, t.changeApply())
			return err
		},
		Flows: t.procs != nil,
		Stage: func(entry Entry, key, hash string) bool {
			return t.stage(preparedAccepted{entry: entry, key: key, hash: hash})
		},
		Commit: t.acceptResult})
}

func (t *Tenant) committedInput() runtimekernel.CommittedInput {
	return runtimekernel.CommittedInput{Settlement: t.settlement(), Apply: t.applyAcceptedInput,
		Remember: func(e platform.AuditEntry) { t.AuditLog.Remember(e) }}
}

func (t *Tenant) stage(candidate preparedAccepted) bool {
	if t.preparedResult == nil {
		return false
	}
	candidate.uploads = t.preparedResult.uploads
	*t.preparedResult = candidate
	return true
}

func decideAcceptedInput(a platform.AcceptedInputApp, draft *stagedDecision, m platform.Member, name string, body []byte, at time.Time) (answer any, refusal *kernel.Error, unsupported bool) {
	defer func() {
		if p := recover(); p != nil {
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				panic(p)
			}
			answer = nil
			refusal = platform.RefuseInputOutsideAcceptedBoundary()
			unsupported = true
		}
	}()
	answer, refusal = inputApp(draft.stateApp(a), platform.NewCaller(draft, m, a.Manifest().ID, false, false),
		name, slices.Clone(body), at)
	return answer, refusal, false
}

func (t *Tenant) applyAcceptedInput(raw []byte) (acceptedformat.Input, bool, error) {
	return t.ApplyInputResult(raw, runtimekernel.InputApply{Changes: t.changeApply(), Published: t.publishAcceptedChanges, Refused: t.ConnectorRoster.Refused,
		Input: func(app string) platform.AcceptedInputApp { return t.inputs[app] }})
}

func (t *Tenant) finishPreparedInput(m platform.Member, name string, candidate preparedAccepted, answer any, refusal *kernel.Error) (any, *kernel.Error) {
	if len(candidate.entry.Body) == 0 {
		return answer, refusal
	}
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return t.FinishInput(t.App(candidate.entry.App).(platform.AcceptedInputApp), m, name, candidate.key, candidate.hash, committed, t.committedInput())
}
