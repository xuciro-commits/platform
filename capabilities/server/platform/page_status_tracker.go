package platform

import (
	"fmt"
	"slices"
)

// PageStatusTracker is a read-only presentation of original lifecycle states.
type PageStatusTracker struct {
	Field  string   `json:"field"`
	Stages []string `json:"stages"`
}

func (d *PageDocument) checkStatusTracker(s Section) error {
	if s.Widget != "status-tracker" {
		if s.StatusTracker != nil {
			return fmt.Errorf("status tracker configuration needs its widget")
		}
		return nil
	}
	limits := pageWidgets.Runtime.StatusTracker
	value := s.StatusTracker
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || value == nil || value.Field == "" || len(value.Stages) == 0 || len(value.Stages) > limits.MaxStages || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Query.Name != "" || s.Relation != "" {
		return fmt.Errorf("status tracker needs its profile and bounded original states")
	}
	seen := map[string]bool{}
	for _, stage := range value.Stages {
		if stage == "" || seen[stage] {
			return fmt.Errorf("status tracker needs unique state identities")
		}
		seen[stage] = true
	}
	return nil
}

func (s Section) CheckStatusTracker(info EntityInfo) error {
	if s.Widget != "status-tracker" {
		return nil
	}
	l := info.Lifecycle
	value := s.StatusTracker
	if l == nil || value == nil || value.Field != l.Field {
		return fmt.Errorf("status tracker needs its original lifecycle field")
	}
	field, ok := info.Field(value.Field)
	if !ok || !slices.Contains([]string{"text", "choice"}, field.Type) {
		return fmt.Errorf("status tracker lifecycle field is unavailable")
	}
	for _, stage := range value.Stages {
		if !slices.ContainsFunc(l.States, func(s State) bool { return s.Name == stage }) {
			return fmt.Errorf("status tracker state is unavailable")
		}
	}
	return nil
}
