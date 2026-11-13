package host

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"slices"
	"time"
)

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
			_, err := t.prepareAcceptedChanges(plan)
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
	return t.ApplyInputResult(raw, runtimekernel.InputApply{Changes: t.changeApply(), Published: t.publishAcceptedChanges, Refused: t.refused,
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
