// Package aistate owns the AI owner's persisted format and pure metering.
// It has no live reservations, ledger, transport or submission entry point.
package aistate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"platformserver/platform"
	"strings"
	"time"
)

const UsageKept = 5000

type State struct {
	Providers []platform.AIProvider       `json:"providers"`
	Models    []platform.AIModel          `json:"models"`
	Limits    map[string]platform.AILimit `json:"limits,omitempty"`
	Usage     []platform.AIUsage          `json:"usage"`
	Daily     map[string]int              `json:"daily,omitempty"`
}

// Decode validates the original saved format without accepting unknown fields
// or trailing values. Both application state and SQL projections use it.
func Decode(raw json.RawMessage) (State, error) {
	var state State
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return State{}, fmt.Errorf("accepted AI state has trailing data")
	}
	if err := state.Validate(); err != nil {
		return State{}, err
	}
	return state, nil
}

func (s State) Validate() error {
	providers, models := map[string]bool{}, map[string]bool{}
	for _, provider := range s.Providers {
		if provider.ID == "" || providers[provider.ID] {
			return fmt.Errorf("accepted AI state has an invalid provider")
		}
		providers[provider.ID] = true
	}
	for _, model := range s.Models {
		if !providers[model.Provider] || model.Model == "" || models[model.Name()] ||
			model.Access != "everyone" && model.Access != "users" || model.DailyTokens < 0 {
			return fmt.Errorf("accepted AI state has an invalid model")
		}
		if model.Revision < 0 {
			return fmt.Errorf("accepted AI state has an invalid revision")
		}
		if _, err := model.Resolve("", platform.ModelOptions{}, 0); err != nil {
			return fmt.Errorf("accepted AI model: %w", err)
		}
		models[model.Name()] = true
	}
	for member, limit := range s.Limits {
		if member == "" || member != limit.Member || limit.DailyTokens < 0 || limit.PerMinute < 0 {
			return fmt.Errorf("accepted AI state has an invalid limit")
		}
	}
	if len(s.Usage) > UsageKept {
		return fmt.Errorf("accepted AI state exceeds retained usage")
	}
	return nil
}

// Meter updates only persistent usage and daily totals. Callers own the state;
// the AI instance separately updates its transient reservations.
func (s *State) Meter(u platform.AIUsage) {
	if s.Daily == nil {
		s.Daily = map[string]int{}
	}
	s.Usage = append(s.Usage, u)
	if len(s.Usage) > UsageKept {
		s.Usage = s.Usage[len(s.Usage)-UsageKept:]
	}
	day := u.At.UTC().Format(time.DateOnly)
	if _, today := s.Daily[day+"|"+u.Member]; !today { // a new day: keep only it and the one before
		yesterday := u.At.UTC().AddDate(0, 0, -1).Format(time.DateOnly)
		for k := range s.Daily {
			if !strings.HasPrefix(k, day) && !strings.HasPrefix(k, yesterday) {
				delete(s.Daily, k)
			}
		}
	}
	s.Daily[day+"|"+u.Member] += u.Input + u.Output
	s.Daily[day+"|model:"+u.Model] += u.Input + u.Output
}

// ApplyUsageState decodes into private values and applies the same meter used
// by the AI owner. The input image and all original format names stay intact.
func ApplyUsageState(raw json.RawMessage, u platform.AIUsage) (json.RawMessage, error) {
	state, err := Decode(raw)
	if err != nil {
		return nil, err
	}
	state.Meter(u)
	return json.Marshal(state)
}
