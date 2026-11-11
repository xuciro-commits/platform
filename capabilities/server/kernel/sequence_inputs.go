package kernel

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func InputResult(tenant, app, name, member string, body []byte, at time.Time) (acceptedformat.Input, error) {
	hash, err := acceptedformat.InputHash(tenant, app, name, member, body)
	if err != nil {
		return acceptedformat.Input{}, err
	}
	return acceptedformat.Input{Version: 2, Kind: "input-result", Tenant: tenant, App: app,
		Name: name, Member: member, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash}, nil
}

func SealInput(result acceptedformat.Input) ([]byte, error) {
	var err error
	result.Digest, err = acceptedformat.DigestInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := acceptedformat.DecodeInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

type InputApply struct {
	Changes   ChangeApply
	Input     func(app string) platform.AcceptedInputApp
	Published func(*PreparedChanges)
	Refused   func(member, name string, refusal *kernel.Error, at time.Time)
}

func (s *Sequence) ApplyInputResult(raw []byte, h InputApply) (acceptedformat.Input, bool, error) {
	result, err := acceptedformat.DecodeInput(raw)
	if err != nil {
		return result, false, err
	}
	app := h.Input(result.App)
	if result.Tenant != s.ID || app == nil || !slices.Contains(app.AcceptedInputs(), result.Name) {
		return result, false, fmt.Errorf("connector result belongs to another input")
	}
	if result.Refusal == nil {
		if _, err := app.DecodeAcceptedInput(result.Answer); err != nil {
			return result, false, fmt.Errorf("connector result has an invalid answer: %w", err)
		}
	}
	if prior := s.Committed.Input(result.App + "/" + result.Key); len(prior) > 0 {
		saved, err := acceptedformat.DecodeInput(prior)
		if err != nil || saved.Digest != result.Digest {
			return result, false, fmt.Errorf("connector input key belongs to another result")
		}
		return result, false, nil
	}
	prepared, err := s.PrepareChanges(result.ChangePlan(), h.Changes)
	if err != nil {
		return result, false, err
	}
	if err := s.PromoteChanges(prepared, h.Changes); err != nil {
		return result, false, err
	}
	h.Published(prepared)
	if result.Refusal != nil {
		h.Refused(result.Member, result.Name, result.Refusal, result.At)
	}
	s.Committed.SaveInput(result.App+"/"+result.Key, raw)
	return result, true, nil
}
