package platformserver

import (
	"encoding/json"
	"fmt"

	resultformat "platformserver/internal/acceptance/result"

	"slices"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
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
