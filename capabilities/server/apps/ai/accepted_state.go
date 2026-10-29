package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"slices"
	"time"

	"platformserver/internal/aistate"
	"platformserver/platform"
)

func (a *AI) AcceptedLedger() *platform.Ledger { return a.ledger }

func (a *AI) ForkAcceptedState() (platform.App, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fork := &AI{ledger: a.ledger, trainingHost: a.trainingHost, trainingPolicies: maps.Clone(a.trainingPolicies), trainingCandidates: maps.Clone(a.trainingCandidates),
		providers: slices.Clone(a.providers), models: slices.Clone(a.models),
		limits: maps.Clone(a.limits), usage: slices.Clone(a.usage), daily: maps.Clone(a.daily),
		recent: cloneCalls(a.recent), reservations: maps.Clone(a.reservations)}
	return fork, nil
}

func cloneCalls(source map[string][]time.Time) map[string][]time.Time {
	out := make(map[string][]time.Time, len(source))
	for member, calls := range source {
		out[member] = slices.Clone(calls)
	}
	return out
}

func (a *AI) AcceptedState() (json.RawMessage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return json.Marshal(aistate.State{Providers: a.providers, Models: a.models, Limits: a.limits, Usage: a.usage, Daily: a.daily, Reservations: a.reservations, TrainingPolicies: a.trainingPolicies, TrainingCandidates: a.trainingCandidates})
}

func (*AI) ValidateAcceptedState(raw json.RawMessage) error {
	_, err := aistate.Decode(raw)
	return err
}

func (a *AI) ApplyAcceptedState(raw json.RawMessage) error {
	state, err := aistate.Decode(raw)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.providers, a.models, a.limits = state.Providers, state.Models, state.Limits
	a.usage, a.daily, a.reservations = state.Usage, state.Daily, state.Reservations
	a.trainingPolicies, a.trainingCandidates = state.TrainingPolicies, state.TrainingCandidates
	if a.trainingPolicies == nil {
		a.trainingPolicies = map[string]platform.AITrainingPolicy{}
	}
	if a.trainingCandidates == nil {
		a.trainingCandidates = map[string]platform.AITrainingCandidate{}
	}
	if a.reservations == nil {
		a.reservations = map[string]platform.AIReservation{}
	}
	if a.limits == nil {
		a.limits = map[string]Limit{}
	}
	if a.daily == nil {
		a.daily = map[string]int{}
	}
	return nil
}

var _ platform.AcceptedStateApp = (*AI)(nil)

func (*AI) AcceptedInputs() []string { return []string{platform.AIReserveCall} }
func (*AI) DecodeAcceptedInput(raw json.RawMessage) (any, error) {
	var r platform.AIReservation
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	if r.Call == "" {
		return nil, fmt.Errorf("reservation answer has no identity")
	}
	return r, nil
}
func (a *AI) Input(c platform.Caller, name string, raw []byte, now time.Time) (any, *kernel.Error) {
	if c.ID != "app:"+ID || !c.Staging() || name != platform.AIReserveCall {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the AI meter can reserve model usage")
	}
	var input platform.AIReserveInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The model reservation is invalid")
	}
	r := input.Reservation
	if input.Cancel {
		a.mu.Lock()
		defer a.mu.Unlock()
		prior, exists := a.reservations[r.Call]
		if !exists || prior != r || prior.State != "pending" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model reservation cannot be released")
		}
		delete(a.reservations, r.Call)
		return prior, nil
	}
	if r.Call == "" || r.Member == "" || !r.At.Equal(now) || r.Input < 0 || r.Output < 0 || r.Input+r.Output <= 0 || r.Accounted != 0 || r.State != "pending" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The model reservation is invalid")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if prior, ok := a.reservations[r.Call]; ok {
		if prior != r {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The model call identity already has different input")
		}
		return prior, nil
	}
	i := slices.IndexFunc(a.models, func(model Model) bool { return model.Name() == r.Model })
	if i < 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The selected model is disabled or missing")
	}
	if input.RunTokens > 0 {
		reserved := 0
		for _, prior := range a.reservations {
			if prior.Run == r.Run && r.Run != "" {
				reserved += max(0, prior.Input+prior.Output-prior.Accounted)
			}
		}
		if r.Input+r.Output > input.RunTokens-reserved {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The model request exceeds the remaining run token budget")
		}
	}
	if why := a.allowLocked(platform.Member{ID: r.Member, Agent: r.Agent}, a.models[i], input.Defaults, r, now); why != "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, why)
	}
	a.reservations[r.Call] = r
	return r, nil
}

var _ platform.AcceptedInputApp = (*AI)(nil)
