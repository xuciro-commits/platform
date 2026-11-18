package platform

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

type ObservationContext struct {
	Definition AssetBinding
	Record     string
	Intervals  *ObservationIntervalResult
}

func (c Caller) ObservationContext() *ObservationContext {
	if runtime := c.StagedDecision(); runtime != nil && !c.Replaying {
		return runtime.ObservationContext(c)
	}
	return nil
}

type ObservationSampleSource struct {
	Connection string `json:"connection"`
	Revision   string `json:"revision"`
	Generation string `json:"generation"`
	Sequence   string `json:"sequence,omitempty"`
	Clock      string `json:"clock,omitempty"`
}

type ObservationSample struct {
	Device     string                  `json:"device"`
	Signal     string                  `json:"signal"`
	Type       string                  `json:"type"`
	Value      any                     `json:"value,omitempty"`
	Unit       string                  `json:"unit,omitempty"`
	Quality    string                  `json:"quality"`
	At         time.Time               `json:"at"`
	ReceivedAt time.Time               `json:"receivedAt"`
	Reason     string                  `json:"reason,omitempty"`
	Source     ObservationSampleSource `json:"source"`
}

type ObservationCoverage struct {
	AvailableSince time.Time `json:"availableSince"`
	CompleteUntil  time.Time `json:"completeUntil"`
	Truncated      bool      `json:"truncated"`
	Good           int64     `json:"good"`
	Bad            int64     `json:"bad"`
	Uncertain      int64     `json:"uncertain"`
	Stale          int64     `json:"stale"`
}

const ObservationMaxWindowSeconds = 366 * 24 * 60 * 60

const ObservationMaxPoints = 5000

type ObservationDefinition struct {
	Name                       string                `json:"name"`
	Title                      string                `json:"title"`
	Description                string                `json:"description,omitempty"`
	Connection                 AssetRef              `json:"connection"`
	Object                     AssetRef              `json:"object"`
	DeviceField                string                `json:"deviceField"`
	Signal                     string                `json:"signal"`
	Unit                       string                `json:"unit"`
	Path                       string                `json:"path"`
	WindowSeconds              int                   `json:"windowSeconds"`
	ExpectedRetentionSeconds   int                   `json:"expectedRetentionSeconds"`
	ExpectedGranularitySeconds int                   `json:"expectedGranularitySeconds"`
	Roles                      []string              `json:"roles"`
	Scheduled                  bool                  `json:"scheduled,omitempty"`
	RequireComplete            bool                  `json:"requireComplete,omitempty"`
	Decision                   *ObservationDecision  `json:"decision,omitempty"`
	Intervals                  *ObservationIntervals `json:"intervals,omitempty"`
}

type ObservationDecision struct {
	Mode   string          `json:"mode,omitempty" enum:"summary,intervals"`
	When   Expr            `json:"when"`
	Action AssetRef        `json:"action"`
	Inputs map[string]Expr `json:"inputs"`
}

type ObservationWindow = ExprWindowSummary

type ObservationPoint struct {
	At    time.Time `json:"at"`
	Value string    `json:"value"`
}

type ObservationResult struct {
	Ref       AssetRef                   `json:"ref"`
	Version   string                     `json:"version"`
	Object    AssetRef                   `json:"object"`
	Record    string                     `json:"record"`
	Device    string                     `json:"device"`
	Signal    string                     `json:"signal"`
	Window    ObservationWindow          `json:"window"`
	Points    []ObservationPoint         `json:"points,omitempty"`
	Sources   []string                   `json:"sources"`
	Matched   *bool                      `json:"matched,omitempty"`
	Coverage  *ObservationCoverage       `json:"coverage,omitempty"`
	Latest    *ObservationSample         `json:"latest,omitempty"`
	Intervals *ObservationIntervalResult `json:"intervals,omitempty"`
}

func (d *ObservationDefinition) UnmarshalJSON(raw []byte) error {
	type plain ObservationDefinition
	return strictTxn(raw, (*plain)(d))
}

func (d *ObservationDecision) UnmarshalJSON(raw []byte) error {
	type plain ObservationDecision
	return strictTxn(raw, (*plain)(d))
}

func (d ObservationDefinition) Check(engines ...RuleEngine) error {
	if !functionName.MatchString(d.Name) || strings.TrimSpace(d.Title) == "" || len(d.Title) > 256 || len(d.Description) > 4096 || d.Connection.Check() != nil || d.Connection.Kind != AssetConnection || d.Object.Check() != nil || d.Object.Kind != AssetObject || !functionFieldName.MatchString(d.DeviceField) || strings.TrimSpace(d.Signal) == "" || len(d.Signal) > 128 || strings.TrimSpace(d.Unit) == "" || len(d.Unit) > 64 || d.WindowSeconds < 1 || d.WindowSeconds > ObservationMaxWindowSeconds || d.ExpectedRetentionSeconds < d.WindowSeconds || d.ExpectedRetentionSeconds > 10*ObservationMaxWindowSeconds || d.ExpectedGranularitySeconds < 1 || d.ExpectedGranularitySeconds > d.WindowSeconds || len(d.Roles) == 0 || len(d.Roles) > 64 {
		return fmt.Errorf("observation needs a logical connection, object/device field, signal/unit, bounded window/retention/granularity and roles")
	}
	path, err := url.Parse(d.Path)
	if err != nil || !strings.HasPrefix(d.Path, "/") || strings.HasPrefix(d.Path, "//") || strings.ContainsAny(d.Path, "\\\r\n") || strings.Contains(strings.ToLower(d.Path), "%2f") || strings.Contains(strings.ToLower(d.Path), "%5c") || path.IsAbs() || path.Host != "" || path.User != nil || path.RawQuery != "" || path.Fragment != "" || len(d.Path) > 1024 {
		return fmt.Errorf("observation adapter path must be relative without addresses, credentials or query parameters")
	}
	for _, part := range strings.Split(path.Path, "/") {
		if part == "." || part == ".." {
			return fmt.Errorf("observation adapter path cannot traverse outside its relative endpoint")
		}
	}
	if strings.ContainsAny(path.Path, "\\\r\n") {
		return fmt.Errorf("observation adapter path contains an invalid separator")
	}
	for i, role := range d.Roles {
		if role == "" || slices.Contains(d.Roles[:i], role) {
			return fmt.Errorf("observation roles must be named and unique")
		}
	}
	if d.Intervals != nil {
		if err := d.Intervals.Check(d.WindowSeconds, engines...); err != nil {
			return err
		}
	}
	if d.Scheduled && d.Decision == nil {
		return fmt.Errorf("scheduled observation requires its declared business decision")
	}
	if d.Decision != nil {
		if !slices.Contains([]string{"", "summary", "intervals"}, d.Decision.Mode) || d.Decision.Mode == "intervals" && d.Intervals == nil {
			return fmt.Errorf("observation decision mode needs its declared source analysis")
		}
		if d.Decision.When.Kind != "boolean" || d.Decision.Action.Check() != nil || d.Decision.Action.Kind != AssetAction || d.Decision.Action.App != d.Object.App || len(d.Decision.Inputs) > 64 {
			return fmt.Errorf("observation decision needs a boolean condition and the original object's authorized action")
		}
		for name, expr := range d.Decision.Inputs {
			if !functionFieldName.MatchString(name) || expr.Kind == "set" {
				return fmt.Errorf("observation decision inputs must be named compact scalar values")
			}
		}
	}
	return nil
}
