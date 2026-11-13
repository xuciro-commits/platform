package kernel

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func AcceptedEntry(app string, member platform.Member, body []byte, at time.Time, versions map[string]int) persistence.Entry {
	principal, _ := json.Marshal(member)
	return persistence.Entry{App: app, Kind: "accepted-result", Principal: principal, Body: body, At: at, Versions: versions}
}

type CommittedWork struct {
	Settlement
	Apply func([]byte) (bool, error)
}

func (s *Sequence) FinishWork(app, key, hash string, committed []byte, h CommittedWork) string {
	conflict := pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	saved, err := acceptedformat.DecodeWork(committed)
	if err != nil || saved.Tenant != s.ID || saved.App != app || saved.Key != key || saved.RequestHash != hash {
		h.Quarantine(fmt.Errorf("committed work differs from input: %v", err))
		return conflict
	}
	if _, err := h.Apply(committed); err != nil {
		h.Quarantine(err)
		return conflict
	}
	h.Settled(saved.At, saved.App, len(saved.Changes) != 0 || len(saved.States) != 0 || saved.Notices != nil)
	return saved.Outcome
}

type CommittedInput struct {
	Settlement
	Apply    func([]byte) (acceptedformat.Input, bool, error)
	Remember func(platform.AuditEntry)
}

func (s *Sequence) AnswerInput(a platform.AcceptedInputApp, raw []byte, quarantine func(error)) (any, *kernel.Error) {
	conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	result, err := acceptedformat.DecodeInput(raw)
	if err != nil || result.Tenant != s.ID || result.App != a.Manifest().ID {
		quarantine(fmt.Errorf("saved connector answer is incompatible: %v", err))
		return nil, conflict
	}
	if result.Refusal != nil {
		return nil, result.Refusal
	}
	answer, err := a.DecodeAcceptedInput(result.Answer)
	if err != nil {
		quarantine(fmt.Errorf("saved connector answer cannot be decoded: %w", err))
		return nil, conflict
	}
	return answer, nil
}

func (s *Sequence) FinishInput(a platform.AcceptedInputApp, m platform.Member, name, key, hash string, committed []byte, h CommittedInput) (answer any, refusal *kernel.Error) {
	conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	defer func() {
		if failure := recover(); failure != nil {
			h.Quarantine(fmt.Errorf("apply committed connector result: %v", failure))
			answer = nil
			refusal = conflict
		}
	}()
	result, applied, err := h.Apply(committed)
	if err != nil || result.Key != key || result.RequestHash != hash || result.Member != m.ID ||
		result.Name != name || result.App != a.Manifest().ID {
		h.Quarantine(fmt.Errorf("committed connector input differs from request: %v", err))
		return nil, conflict
	}
	h.Applied()
	if applied && result.Refusal == nil {
		h.Remember(platform.AuditEntry{At: result.At, Member: m.ID, App: result.App, Action: "input:" + name})
		h.Enqueue(result.At)
		h.ChangedOwner(result.App)
	}
	return s.AnswerInput(a, committed, h.Quarantine)
}

type InputDraft struct {
	Decision *Decision
	Sources  func() ResultSources
	Decide   func(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, at time.Time) (any, *kernel.Error, bool)
}

type InputAcceptance struct {
	CommittedInput
	Draft     func() InputDraft
	Authorize func(m platform.Member, app, permission, kind string, at time.Time) platform.AuthzDecision
	Prepare   func(acceptedformat.ChangePlan) error
	Flows     bool
	Stage     func(entry persistence.Entry, key, hash string) bool
	Commit    func(entry persistence.Entry, key, hash string) ([]byte, error)
}

func (s *Sequence) AcceptInput(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time, h InputAcceptance) (any, *kernel.Error) {
	app := a.Manifest().ID
	if m.ID == "" || m.Tenant != s.ID || name == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connector input has an invalid identity")
	}
	hash, hashErr := acceptedformat.InputHash(s.ID, app, name, m.ID, body)
	if hashErr != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	key := "input:" + hash
	if saved := s.Committed.Input(app + "/" + key); len(saved) != 0 {
		return s.AnswerInput(a, saved, h.Quarantine)
	}
	draft := h.Draft()
	entry := h.Authorize(m, app, "input:"+name, "action", now)
	draft.Decision.Authorizations = append(draft.Decision.Authorizations, acceptedformat.AuthorizationEvidence{Request: platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: "input:" + name}, Decision: entry, Roles: slices.Clone(m.RolesIn(app))})
	answer, refusal, unsupported := draft.Decide(a, m, name, body, now)
	if unsupported {
		return nil, refusal
	}
	var raw []byte
	var err error
	if refusal != nil {
		var result acceptedformat.Input
		if result, err = InputResult(s.ID, app, name, m.ID, body, now); err == nil {
			result.Refusal = refusal
			raw, err = SealInput(result)
		}
	} else {
		raw, err = draft.Decision.InputDecision(draft.Sources(), app, name, m, body, answer, now)
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input cannot produce a bounded accepted result")
	}
	result, err := acceptedformat.DecodeInput(raw)
	if err == nil {
		err = h.Prepare(result.ChangePlan())
	}
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	var versions map[string]int
	if h.Flows {
		versions = draft.Decision.FlowVersions()
	}
	candidate := AcceptedEntry(app, m, raw, now, versions)
	if h.Stage(candidate, key, hash) {
		return answer, refusal
	}
	committed, err := h.Commit(candidate, key, hash)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return s.FinishInput(a, m, name, key, hash, committed, h.CommittedInput)
}
