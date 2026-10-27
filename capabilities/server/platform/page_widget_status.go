package platform

import (
	"fmt"
	"slices"
)

// page_alert.go

type PageAlertBanner struct {
	Threshold string `json:"threshold"`
	Tone      string `json:"tone"`
	Message   string `json:"message"`
}

func (d *PageDocument) checkAlertBanner(s Section) error {
	if s.Widget != "alert-banner" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	limits := pageWidgets.Runtime.AlertBanner
	c := s.AlertBanner
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil {
		return fmt.Errorf("alert needs its profile, original scoped decimal value and configuration")
	}
	threshold, err := ParseDecimal(c.Threshold)
	if err != nil || threshold.Value != c.Threshold || !slices.Contains(limits.Tones, c.Tone) || c.Message == "" || len(c.Message) > limits.MaxMessageBytes {
		return fmt.Errorf("alert needs a canonical threshold, declared tone and bounded plain message")
	}
	return nil
}

// page_notice.go

type PageNotice struct {
	Title   *string `json:"title,omitempty"`
	Message string  `json:"message"`
	Tone    string  `json:"tone"`
}

func (d *PageDocument) checkNotice(s Section) error {
	if s.Widget != "notice" {
		return nil
	}
	c := s.Notice
	limits := pageWidgets.Runtime.Notice
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || c.Title != nil && len(*c.Title) > limits.MaxTitleBytes || len(c.Message) > limits.MaxMessageBytes || !slices.Contains(limits.Tones, c.Tone) {
		return fmt.Errorf("notice needs its profile, bounded plain text and declared tone without business bindings")
	}
	return nil
}

// page_progress.go

func (d *PageDocument) checkProgress(s Section) error {
	if s.Widget != "progress" {
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Progress.RequiredUIProfile) || s.ProgressValueVariable == "" || (s.ProgressTotalVariable != "") == (s.ProgressTotal != "") || len(s.ProgressLabel) > 1024 {
		return fmt.Errorf("progress needs its profile, numerator and exactly one denominator")
	}
	if s.ProgressTotal != "" {
		v, err := ParseDecimal(s.ProgressTotal)
		if err != nil || v.Value != s.ProgressTotal || v.Rat().Sign() <= 0 {
			return fmt.Errorf("fixed progress denominator needs a positive canonical decimal")
		}
	}
	return nil
}

// page_status_tracker.go

// PageStatusTracker is a read-only presentation of original lifecycle states.
type PageStatusTracker struct {
	Field  string   `json:"field"`
	Stages []string `json:"stages"`
}

func (d *PageDocument) checkStatusTracker(s Section) error {
	if s.Widget != "status-tracker" {
		return nil
	}
	limits := pageWidgets.Runtime.StatusTracker
	value := s.StatusTracker
	if s.Model != nil && value == nil {
		return nil
	} // the model-aware schema check resolves lifecycle defaults
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || value == nil || value.Field == "" || len(value.Stages) == 0 && s.Model == nil || len(value.Stages) > limits.MaxStages {
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
