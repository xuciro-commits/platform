package host

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"slices"
	"time"
)

func (d *stagedDecision) inputResult(app, name string, m platform.Member, body []byte, answer any, at time.Time) ([]byte, error) {
	if d.Failure != nil || len(d.requests) > 0 {
		return nil, fmt.Errorf("connector input contains effects not represented by its result")
	}
	result, err := runtimekernel.InputResult(d.tenant.ID, app, name, m.ID, body, at)
	if err != nil {
		return nil, err
	}
	result.Notices, result.Authorizations = d.Notices.Saved(), slices.Clone(d.Authorizations)
	if message, ok := answer.(proto.Message); ok {
		result.Answer, err = protojson.Marshal(message)
	} else {
		result.Answer, err = json.Marshal(answer)
	}
	if err != nil {
		return nil, err
	}
	if len(d.Events) > 0 {
		receipt, ok := answer.(*pb.ChangeRecord)
		if !ok || receipt.GetSubmission().GetAuthority() != app {
			return nil, fmt.Errorf("connector input has decisions without a matching answer")
		}
		result.Changes, err = d.batchResult(app, receipt.GetSubmission(), receipt, at)
		if err != nil {
			return nil, err
		}
		result.Notices = nil
		return runtimekernel.SealInput(result)
	}
	if d.control != nil || len(d.Intents) > 0 || len(d.Observations) > 0 || len(d.Allocated) > 0 || len(d.OperationCancels) > 0 {
		return nil, fmt.Errorf("connector input contains effects without a decision")
	}
	for ledger, staged := range d.Logs {
		if !proto.Equal(ledger.LastAccepted(d.tenant.ID), staged.Last(d.tenant.ID)) {
			return nil, fmt.Errorf("connector input contains an unpublished decision")
		}
	}
	plan, err := d.savedChangePlan()
	if err != nil {
		return nil, err
	}
	result.States, result.Notices, result.Unique = plan.States, plan.Notices, plan.Unique
	result.Rows, err = captureDirectRows(d, app, nil, nil, "connector input")
	if err != nil {
		return nil, err
	}
	return runtimekernel.SealInput(result)
}

func encodeAcceptedInputRefusal(tenant, app, name string, m platform.Member, body []byte, at time.Time, refusal *kernel.Error) ([]byte, error) {
	result, err := runtimekernel.InputResult(tenant, app, name, m.ID, body, at)
	if err != nil {
		return nil, err
	}
	result.Refusal = refusal
	return runtimekernel.SealInput(result)
}

func (t *Tenant) inputAccepted(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time) (any, *kernel.Error) {
	if m.ID == "" || m.Tenant != t.ID || name == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connector input has an invalid identity")
	}
	hash, hashErr := acceptedformat.InputHash(t.ID, a.Manifest().ID, name, m.ID, body)
	if hashErr != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	key := "input:" + hash
	if saved := t.Committed.Input(a.Manifest().ID + "/" + key); len(saved) != 0 {
		return t.answerAcceptedInput(a, saved)
	}
	draft := t.newStagedDecision()
	entry := t.authorizeEntry(m, a.Manifest().ID, "input:"+name, "action", now)
	draft.Authorizations = append(draft.Authorizations, acceptedformat.AuthorizationEvidence{Request: platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: "input:" + name}, Decision: entry, Roles: slices.Clone(m.RolesIn(a.Manifest().ID))})
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
	result, err := acceptedformat.DecodeInput(raw)
	if err == nil {
		_, err = t.prepareAcceptedChanges(result.ChangePlan())
	}
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	principal, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = draft.FlowVersions()
	}
	entryResult := Entry{App: a.Manifest().ID, Kind: "accepted-result", Principal: principal, Body: raw, At: now, Versions: versions}
	if t.preparedResult != nil {
		*t.preparedResult = preparedAccepted{entry: entryResult, key: key, hash: hash, uploads: t.preparedResult.uploads}
		return answer, refusal
	}
	committed, err := t.acceptResult(entryResult, key, hash)
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
		t.audit.Remember(platform.AuditEntry{At: result.At, Member: m.ID, App: result.App, Action: "input:" + name})
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
			refusal = platform.RefuseInputOutsideAcceptedBoundary()
			unsupported = true
		}
	}()
	answer, refusal = inputApp(draft.stateApp(a), platform.NewCaller(draft, m, a.Manifest().ID, false, false),
		name, slices.Clone(body), at)
	return answer, refusal, false
}

func (t *Tenant) answerAcceptedInput(a platform.AcceptedInputApp, raw []byte) (any, *kernel.Error) {
	result, err := acceptedformat.DecodeInput(raw)
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
	return t.finishCommittedInput(t.App(candidate.entry.App).(platform.AcceptedInputApp), m, name, candidate.key, candidate.hash, committed)
}
