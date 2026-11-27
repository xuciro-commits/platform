package host

import (
	"context"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	automation "platformserver/services/automate/runtime"
	dataowner "platformserver/services/data"
	releaseowner "platformserver/services/devops/releases"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func unknown() *platform.Error { return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA} }

type submitOptions struct {
	source         *dataowner.SourceApplication
	versioned      bool
	contentVersion string
}

type acceptOptions struct {
	automation    bool
	binding       *platform.SchemaVersion
	schemaRefusal *platform.Error
	observation   *platform.ObservationDecisionEvidence
	source        *runtimekernel.ClaimSet
	before        func(*stagedDecision) *platform.Error
	inputs        []*preparedOperationInput
}

func (t *Tenant) Submit(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	record, refusal, _ := t.submit(context.Background(), m, s, now, submitOptions{})
	return record, refusal
}

func (t *Tenant) submit(ctx context.Context, m platform.Member, s *pb.Submission, now time.Time, opts submitOptions) (record *pb.ChangeRecord, refusal *platform.Error, issues []platform.FieldIssue) {
	holder := "submit " + s.GetSchema().GetName()
	ctx, span := monitoring.StartSubmission(ctx, holder, t.ID, m.ID, s)
	if span != nil {
		defer func() { monitoring.End(span, runtimekernel.OutcomeOf(refusal)) }()
	}
	if err := t.Admits(m); err != nil {
		return nil, err, nil
	}
	if t.quarantined() {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}, nil
	}
	if s.GetSchema().GetName() == "flow.dead-letter.replay" {
		record, refusal, handled := t.prepareContinuousDeadLetterReplay(ctx, m, s, now, opts)
		if handled {
			return record, refusal, nil
		}
	}
	result, refusal := decideOnImage(t, ctx, holder, func(t *Tenant) (diagnosedSubmission, *platform.Error) {
		return t.prepareSubmission(m, s, now, opts)
	}, func(t *Tenant, decision preparedAccepted, value diagnosedSubmission, refusal *platform.Error) (diagnosedSubmission, *platform.Error) {
		value.record, refusal = t.finishPreparedSubmission(m, decision, value.record, refusal)
		return value, refusal
	})
	return result.record, refusal, result.issues
}

type diagnosedSubmission struct {
	record *pb.ChangeRecord
	issues []platform.FieldIssue
}

func (t *Tenant) prepareSubmission(m platform.Member, s *pb.Submission, now time.Time, opts submitOptions) (out diagnosedSubmission, err *platform.Error) {
	if refusal := t.recordReadRefusal(); refusal != nil {
		return out, refusal
	}
	a, prior, refusal := t.SubmissionPrior(t.App(t.owner["action:"+s.GetSchema().GetName()]), t.App(s.GetAuthority()), s.GetIdempotencyKey())
	if refusal != nil {
		return out, refusal
	}
	source := opts.source
	if source != nil {
		if record, refusal, done := prior.SourceReplay(m.ID, s, source.Deleted); done {
			return diagnosedSubmission{record: record}, refusal
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
	} else if source != nil {
		s = prior.CurrentSchema(a, s)
	}
	if source != nil {
		prepared, refusal := t.sourcePreparation().Prepare(m, s, source, now)
		if refusal != nil {
			return out, refusal
		}
		s, decision.source = prepared.Submission, &runtimekernel.ClaimSet{Origin: prepared.Origin, Claims: prepared.Claims, Restore: prepared.Restore}
	}
	out.record, err = t.acceptWithPrior(a, m, s, now, decision, prior)
	if err == nil {
		return out, nil
	}
	if t.preparedResult != nil && len(t.preparedResult.entry.Body) > 0 {
		out.issues = t.readableFieldIssues(m, t.preparedResult.issues, now)
	} else if prior, ok := prior.RefusedIssues(s, decision.binding); ok {
		out.issues = t.readableFieldIssues(m, prior, now)
	}
	return out, err
}

func (t *Tenant) accept(a platform.Service, m platform.Member, s *pb.Submission, now time.Time, opts acceptOptions) (record *pb.ChangeRecord, refusal *platform.Error) {
	return t.acceptWithPrior(a, m, s, now, opts, t.PriorOf(a, s.GetIdempotencyKey()))
}

func (t *Tenant) acceptWithPrior(a platform.Service, m platform.Member, s *pb.Submission, now time.Time, opts acceptOptions, prior runtimekernel.Prior) (record *pb.ChangeRecord, refusal *platform.Error) {
	binding := opts.binding
	if binding == nil {
		s = prior.CurrentSchema(a, s)
	}
	if t.businessStopped() {
		return nil, actions.StoppedForMaintenance()
	}
	if refusal := t.CheckSubmission(m, s); refusal != nil {
		return nil, refusal
	}
	record, hash, refusal, done := t.ReplayPrior(a, s, binding, prior)
	if done {
		return record, refusal
	}
	if refusal := controlowner.CheckCredentialSave(t.ID, s); refusal != nil {
		return nil, refusal
	}

	if err := t.checkBusinessCorrection(m, s, binding); err != nil {
		return nil, err
	}
	if m.Agent && t.suspended(m.ID) {
		return nil, actions.AgentSuspended(m.ID)
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
			trace.SpanFromContext(t.readContext()).AddEvent("business.decide")
			record, refusal = decideAcceptedAs(a, draft, m, s, now, opts.automation)
		}
	}
	record, refusal = draft.Settled(record, refusal, automation.PendingOperationInputs(draft.operationInputs))
	trace.SpanFromContext(t.readContext()).AddEvent("accepted.encode")
	raw, refusal, err := draft.Outcome(draft.sources(), runtimekernel.OutcomeHooks{
		Explain: func(r *platform.Error) *platform.Error {
			return actions.Explained(r, a, s.GetSchema().GetName(), runtimekernel.Target(s))
		},
		Claims: func(record *pb.ChangeRecord, source *runtimekernel.ClaimSet) error {
			_, err := t.Claims.Prepare(t.ID, s.GetTarget().GetType(), s.GetTarget().GetId(), record.GetChangeId(), source)
			return err
		}}, a.Manifest().ID, s, record, refusal, binding, opts.source, now)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
	}
	entry := runtimekernel.AcceptedEntry(a.Manifest().ID, m, raw, now, nil)
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

func (t *Tenant) finishCommittedResult(a platform.Service, m platform.Member, hash string, committed []byte) (record *pb.ChangeRecord, refusal *platform.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("apply committed result: %v", failure))
			record = nil
			refusal = &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}()
	return t.FinishCommitted(t.readContext(), a, m, hash, committed, runtimekernel.CommittedResult{Settlement: t.settlement(), App: t.App, Batch: t.batchApply(), Record: t.recordApply(), Publish: t.publishAccepted})
}

func decideAcceptedAs(a platform.Service, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time, automation bool) (record *pb.ChangeRecord, refusal *platform.Error) {
	defer func() {
		if p := recover(); p != nil {
			if p != errStagedEffect {
				panic(p)
			}
			record = nil
			refusal = platform.RefuseOutsideAcceptedBoundary()
		}
	}()
	if declared, _ := a.Manifest().Actions.Action(s.GetSchema().GetName()); ((declared.Approval != nil && !automation) || protocol.NeedsAsk(declared, m, automation)) &&
		draft.tenant.App(protocol.WorkApp) != nil {
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

func (t *Tenant) finishPreparedSubmission(m platform.Member, decision preparedAccepted, record *pb.ChangeRecord, refusal *platform.Error) (*pb.ChangeRecord, *platform.Error) {
	if len(decision.entry.Body) == 0 {
		return record, refusal
	}
	span := trace.SpanFromContext(t.readContext())
	span.AddEvent("storage.append")
	committed, err := t.acceptResult(decision.entry, decision.key, decision.hash)
	if err != nil {
		return nil, runtimekernel.CommitRefusal(err)
	}
	span.AddEvent("projection.apply")
	return t.finishCommittedResult(t.App(decision.entry.App), m, decision.hash, committed)
}

func (t *Tenant) submitBackground(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return decideOnImage(t, context.Background(), "submit:background", func(t *Tenant) (*pb.ChangeRecord, *platform.Error) {
		app := t.App(s.GetAuthority())
		if app == nil {
			return nil, unknown()
		}
		return t.accept(app, m, s, now, acceptOptions{automation: true})
	}, func(t *Tenant, decision preparedAccepted, record *pb.ChangeRecord, refusal *platform.Error) (*pb.ChangeRecord, *platform.Error) {
		return t.finishPreparedSubmission(m, decision, record, refusal)
	})
}

func (t *Tenant) batchApply() runtimekernel.BatchApply {
	h := runtimekernel.BatchApply{App: t.App, OwnerLedger: runtimekernel.LedgerOf(t.App), CheckBindings: build.CheckRunApplicationBindings, DefinitionType: protocol.AppType,
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

func (t *Tenant) recordApply() runtimekernel.RecordApply {
	return runtimekernel.RecordApply{App: t.App, CheckBindings: build.CheckRunApplicationBindings, DefinitionType: protocol.AppType,
		Claims: func(result acceptedformat.Record, receipt *pb.ChangeRecord) (func() error, error) {
			return t.Claims.RecordClaims(t.ID, result, receipt)
		}}
}

func (t *Tenant) acceptResult(entry Entry, key, hash string) ([]byte, error) {
	if t.quarantined() {
		return nil, fmt.Errorf("tenant %s is stopped", t.ID)
	}
	if t.readError() != nil {
		return nil, t.readError()
	}
	return t.Accept(t.readContext(), entry, key, hash, storageDelta, t.quarantine)
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

func (t *Tenant) prepareAcceptedDirectRows(rows []acceptedformat.InputRow, member string, at time.Time) (*records.Store, error) {
	return runtimekernel.ApplyDirectRows(t.Projection, rows, member, at, build.CheckRunApplicationBindings)
}

func (t *Tenant) inputAccepted(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time) (any, *platform.Error) {
	return t.AcceptInput(a, m, name, body, now, runtimekernel.InputAcceptance{CommittedInput: t.committedInput(),
		Draft: func() runtimekernel.InputDraft {
			draft := t.newStagedDecision()
			return runtimekernel.InputDraft{Decision: &draft.Decision, Sources: draft.sources,
				Decide: func(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, at time.Time) (any, *platform.Error, bool) {
					return decideAcceptedInput(a, draft, m, name, body, at)
				}}
		},
		Authorize: t.authorizeEntry,
		Prepare: func(plan acceptedformat.ChangePlan) error {
			_, err := t.PrepareChanges(plan, t.changeApply())
			return err
		},
		Flows:  t.Processes != nil,
		Stage:  t.stageEntry,
		Commit: t.acceptResult})
}

func (t *Tenant) committedInput() runtimekernel.CommittedInput {
	return runtimekernel.CommittedInput{Settlement: t.settlement(), Apply: t.applyAcceptedInput}
}

func (t *Tenant) stageEntry(entry Entry, key, hash string) bool {
	return t.stage(preparedAccepted{entry: entry, key: key, hash: hash})
}

func (t *Tenant) stage(decision preparedAccepted) bool {
	if t.preparedResult == nil {
		return false
	}
	decision.uploads = t.preparedResult.uploads
	*t.preparedResult = decision
	return true
}

func decideAcceptedInput(a platform.AcceptedInputApp, draft *stagedDecision, m platform.Member, name string, body []byte, at time.Time) (answer any, refusal *platform.Error, unsupported bool) {
	defer func() {
		if p := recover(); p != nil {
			if p != errStagedEffect {
				panic(p)
			}
			answer = nil
			refusal = platform.RefuseInputOutsideAcceptedBoundary()
			unsupported = true
		}
	}()
	answer, refusal = draft.inputApp(a, platform.NewCaller(draft, m, a.Manifest().ID, false, false),
		name, slices.Clone(body), at)
	return answer, refusal, false
}

func (t *Tenant) applyAcceptedInput(raw []byte) (acceptedformat.Input, bool, error) {
	return t.ApplyInputResult(raw, runtimekernel.InputApply{Changes: t.changeApply(), Publish: t.publishAccepted, Refused: t.ConnectorRoster.Refused,
		Input: func(app string) platform.AcceptedInputApp { return t.inputs[app] }})
}

func (t *Tenant) finishPreparedInput(m platform.Member, name string, decision preparedAccepted, answer any, refusal *platform.Error) (any, *platform.Error) {
	if len(decision.entry.Body) == 0 {
		return answer, refusal
	}
	committed, err := t.acceptResult(decision.entry, decision.key, decision.hash)
	if err != nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return t.FinishInput(t.App(decision.entry.App).(platform.AcceptedInputApp), m, name, decision.key, decision.hash, committed, t.committedInput())
}
