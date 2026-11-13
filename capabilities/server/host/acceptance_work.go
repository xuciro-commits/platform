package host

import (
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/ai/gateway"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
	"slices"
	"time"
)

func (t *Tenant) usage() runtimekernel.UsageApply {
	if t.ai == nil {
		return runtimekernel.UsageApply{}
	}
	return runtimekernel.UsageApply{Usage: t.ai.Snapshot, Meter: t.ai.Meter}
}

func (t *Tenant) usagePredecessor() (string, error) { return t.usage().Predecessor() }

func (t *Tenant) usageAcceptance() aigateway.UsageAcceptance {
	return aigateway.UsageAcceptance{Tenant: t.ID,
		Prior: func() []platform.AIUsage { return t.ai.Usage() },
		Caller: func(m platform.Member) platform.Caller {
			return platform.NewCaller(runtime{t: t, scoped: true}, m, platform.AIApp, false, true)
		},
		Authorize: func(c platform.Caller, valid bool) (platform.AuthzDecision, *kernel.Error) {
			decision := t.authorizeNative(c, "usage:observe", "derive", nil, valid)
			return decision, authorizationRefusal(decision)
		},
		Predecessor: t.usagePredecessor,
		Apply:       func(raw []byte) error { _, err := t.ApplyUsageResult(raw, t.usage()); return err },
		Quarantine:  t.quarantine,
		Applied:     func() { t.acceptedResultApplied(); t.operationsChanged(platform.AIApp) }}
}

func (t *Tenant) acceptUsage(m platform.Member, u platform.AIUsage) *kernel.Error {
	r, raw, refusal := t.usageAcceptance().Seal(m, u)
	if refusal != nil || raw == nil {
		return refusal
	}
	candidate := preparedAccepted{entry: runtimekernel.AcceptedEntry(platform.AIApp, m, raw, r.At, nil), key: r.Key, hash: r.RequestHash}
	if t.stage(candidate) {
		return nil
	}
	return t.finishUsage(candidate)
}

func (t *Tenant) finishUsage(candidate preparedAccepted) *kernel.Error {
	committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage was not accepted")
	}
	return t.usageAcceptance().Finish(committed, candidate.key, candidate.hash)
}

func (t *Tenant) acceptWork(task *automation.Task, now time.Time) (outcome string) {
	if task.Event != nil {
		end := t.begin("deliver "+task.Event.Record.GetSubmission().GetSchema().GetName()+" to "+task.App, task.Event.Span,
			attribute.String("platform.app", task.App), attribute.String("platform.target", runtimekernel.Target(task.Event.Record.GetSubmission())))
		defer func() { end(outcome); monitoring.Counted(t.ID, "delivery", task.App, outcome) }()
	}
	defer func() {
		if p := recover(); p != nil {
			t.hops = 0
			t.quarantine(fmt.Errorf("accepted work: %v", p))
			outcome = pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	}()
	return automation.WorkAcceptance{Tenant: t.ID, Fork: t.forkWorkState, Retry: t.retryOf,
		Bind: func(app string) ([]platform.AssetBinding, error) {
			return t.bindApplications(platform.AssetRef{App: app, Kind: platform.AssetApp, Name: app}, "", "", app)
		},
		Draft: func(task *automation.Task, now time.Time) automation.WorkDraft {
			draft := t.newStagedDecision()
			draft.At = now
			if task.Event != nil {
				draft.Hops = task.Event.Hops + 1
			}
			c := platform.NewCaller(draft, platform.Member{ID: "app:" + task.App, Tenant: t.ID}, task.App, false, true)
			return automation.WorkDraft{
				Decide: func() (*kernel.Error, []acceptedformat.AuthorizationEvidence) {
					refusal := decideWork(t, draft, c, task, now)
					return refusal, slices.Clone(draft.Authorizations)
				},
				Reset: func() { draft = t.newStagedDecision(); draft.At = now },
				Failed: func(title, why, id string) {
					t.failedWork(func() platform.Caller {
						return platform.NewCaller(draft, platform.Member{ID: "app:" + controlowner.PlatformApp, Tenant: t.ID}, controlowner.PlatformApp, false, true)
					}, title, why, id, now)
				},
				Decision: func(result *acceptedformat.Work) error { return draft.WorkDecision(draft.sources(), result, now) },
				Commit: func(result acceptedformat.Work) string {
					conflict := pb.ErrorCode_ERROR_CODE_CONFLICT.String()
					raw, err := runtimekernel.SealWork(result)
					if err != nil {
						return conflict
					}
					if _, err := t.prepareAcceptedChanges(result.ChangePlan()); err != nil {
						return conflict
					}
					entry := runtimekernel.AcceptedEntry(task.App, c.Member, raw, now, draft.FlowVersions())
					if t.stage(preparedAccepted{entry: entry, key: result.Key, hash: result.RequestHash}) {
						return result.Outcome
					}
					committed, err := t.acceptResult(entry, result.Key, result.RequestHash)
					if err != nil {
						return conflict
					}
					return t.finishCommittedWork(task.App, result.Key, result.RequestHash, committed)
				}}
		}}.Accept(task, now)
}

func (t *Tenant) finishCommittedWork(app, key, hash string, committed []byte) string {
	return t.FinishWork(app, key, hash, committed, runtimekernel.CommittedWork{Settlement: t.settlement(), Apply: t.applyAcceptedWork})
}

func decideWork(t *Tenant, d *stagedDecision, c platform.Caller, task *automation.Task, now time.Time) (refusal *kernel.Error) {
	defer func() {
		t.hops = 0
		if p := recover(); p != nil {
			refusal = platform.RefuseOutsideAcceptedBoundary()
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The owner failed while handling this work: {reason}", fmt.Sprint(p))
			}
		}
	}()
	owner := t.App(task.App)
	if owner == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", "verb", "The operation is unavailable in its owner declaration")
	}
	permission, declared := "job:"+task.Job.Name, false
	if task.Kind == "job" {
		declared = slices.ContainsFunc(owner.Manifest().Jobs, func(job platform.Job) bool { return job.Name == task.Job.Name })
	} else if task.Event != nil {
		permission = "delivery:" + task.Event.Record.GetSubmission().GetSchema().GetName()
		declared = len(owner.Manifest().Subscribes) > 0 || slices.Contains(t.listeners, task.App)
	}
	if refusal := d.authorizeNative(c, permission, "action", owner.Manifest().AllRoles(), declared); refusal != nil {
		return refusal
	}
	if task.Kind == "job" {
		refusal = d.stateApp(t.App(task.App)).(platform.Runner).Run(c, task.Job.Name, now)
	} else {
		t.hops = task.Event.Hops + 1
		if listener, ok := d.stateApp(t.App(task.App)).(platform.Listener); ok {
			names := append([]string{task.Event.Record.GetSubmission().GetSchema().GetName()}, t.router().Events(task.Event.Event)...)
			if task.Event.Plan != nil {
				names = task.Event.Plan.Names
			}
			refusal = listener.Listen(c, task.Event.Event, names, now)
		} else {
			refusal = d.stateApp(t.App(task.App)).(platform.Subscriber).Handle(c, task.Event.Event)
		}
	}
	if refusal == nil {
		refusal = d.Failure
	}
	if refusal == nil {
		refusal = d.answerRequests(now)
	}
	if refusal == nil {
		refusal = d.stageObservers()
	}
	return refusal
}

func (t *Tenant) applyAcceptedWork(raw []byte) (bool, error) {
	return t.ApplyWorkResult(raw, runtimekernel.WorkApply{Changes: t.changeApply(), Works: t.Works, App: t.App, Published: t.publishAcceptedChanges,
		Task: t.work.Restorer(t.delivered)})
}

func (t *Tenant) forkWorkState() *kernel.Works { return runtimekernel.ForkWorks(t.Works) }
