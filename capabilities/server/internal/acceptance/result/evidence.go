package result

import (
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
)

type AuthorizationEvidence struct {
	Request  platform.AuthzRequest  `json:"request"`
	Decision platform.AuthzDecision `json:"decision"`
	Roles    []string               `json:"roles"`
}

// DecisionDefinition is the original decision's execution contract and resource
// memberships, never a second application or decision state.
type DecisionDefinition struct {
	Observation       *ObservationDecisionEvidence `json:"observation,omitempty"`
	Definition        platform.AssetBinding        `json:"definition"`
	Contract          platform.SchemaVersion       `json:"contract"`
	Closure           string                       `json:"closure,omitempty"`
	DependenciesState string                       `json:"dependenciesState"`
	Release           string                       `json:"release,omitempty"`
	Dependencies      []platform.AssetBinding      `json:"dependencies"`
	Applications      []platform.AssetBinding      `json:"applications"`
}

// ObservationDecisionEvidence retains the compact window and source identity;
// raw points never enter the accepted result.
type ObservationDecisionEvidence struct {
	Key        string                     `json:"key"`
	Definition platform.AssetBinding      `json:"definition"`
	Record     string                     `json:"record"`
	Device     string                     `json:"device"`
	Signal     string                     `json:"signal"`
	Window     platform.ObservationWindow `json:"window"`
}

// ValidateAuthorizationEvidence checks only the saved shape during recovery,
// never current grants or policies. Results predating evidence remain valid.
func ValidateAuthorizationEvidence(tenant string, evidence []AuthorizationEvidence) error {
	order := []string{"tenant", "principal", "deny", "verb", "row", "fields", "derived", "markings", "allow"}
	for _, item := range evidence {
		q, decision := item.Request, item.Decision
		if q.Principal == "" || q.Tenant != tenant || !slices.Contains([]string{"read", "write", "create", "archive", "action", "derive", "export", "protocol"}, q.Kind) || decision.Reason == "" || len(decision.Layers) == 0 || len(decision.Layers) > len(order) {
			return fmt.Errorf("saved authorization identity or layers are invalid")
		}
		for i, layer := range decision.Layers {
			if layer.Name != order[i] || layer.Reason == "" || !layer.Allowed && i != len(decision.Layers)-1 {
				return fmt.Errorf("saved authorization layer order is invalid")
			}
		}
		last := decision.Layers[len(decision.Layers)-1]
		if decision.Allow != last.Allowed || decision.Allow && len(decision.Layers) != len(order) {
			return fmt.Errorf("saved authorization outcome differs from its layers")
		}
	}
	return nil
}

func (b DecisionDefinition) Check(app, schema string) error {
	if b.Observation != nil {
		if err := b.Observation.Check(); err != nil {
			return err
		}
	}
	if b.Definition.Ref != (platform.AssetRef{App: app, Kind: platform.AssetAction, Name: schema}) || b.Definition.SourceVersion == "" || b.Contract.Check() != nil || len(b.Dependencies) > 1000 || len(b.Applications) > 1000 || slices.ContainsFunc(b.Applications, func(binding platform.AssetBinding) bool { return binding.Ref.Kind != platform.AssetApp }) {
		return fmt.Errorf("decision definition differs from its receipt")
	}
	for _, bindings := range [][]platform.AssetBinding{b.Dependencies, b.Applications} {
		seen := map[platform.AssetRef]bool{}
		for _, binding := range bindings {
			if binding.Ref.Check() != nil || binding.SourceVersion == "" || seen[binding.Ref] {
				return fmt.Errorf("decision dependency has an invalid or duplicate binding")
			}
			seen[binding.Ref] = true
		}
	}
	return nil
}

func (e ObservationDecisionEvidence) Check() error {
	if !strings.HasPrefix(e.Key, "observation:") || len(e.Key) > 200 || e.Definition.Ref.Kind != platform.AssetObservation || e.Definition.Ref.Check() != nil || e.Definition.SourceVersion == "" || e.Record == "" || e.Device == "" || e.Signal == "" {
		return fmt.Errorf("observation decision evidence needs its original source and record")
	}
	return e.Window.Check()
}
