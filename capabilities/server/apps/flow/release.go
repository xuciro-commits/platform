package flow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"platformserver/platform"
)

// Go callbacks execute only in the original compiled owner. The descriptor
// freezes their presence, control edges and budgets, and pins that owner code.
type nativeStepPolicy struct {
	Name        string                `json:"name"`
	Retry       *platform.RetryPolicy `json:"retry,omitempty"`
	Timeout     time.Duration         `json:"timeout,omitempty"`
	WorkingDays int                   `json:"workingDays,omitempty"`
	OnTimeout   string                `json:"onTimeout,omitempty"`
	Fault       string                `json:"fault,omitempty"`
	NoRetry     bool                  `json:"noRetry,omitempty"`
	All         []string              `json:"all,omitempty"`
	Any         []string              `json:"any,omitempty"`
	Next        string                `json:"next,omitempty"`
	Output      bool                  `json:"output,omitempty"`
	Choose      bool                  `json:"choose,omitempty"`
	Undo        bool                  `json:"undo,omitempty"`
	End         bool                  `json:"end,omitempty"`
	LoopControl string                `json:"loopControl,omitempty"`
}

func (f *Flows) NativeFlowAsset(manifest platform.Manifest, name string, version int, requires []platform.AssetRef) (platform.ReleaseAsset, error) {
	d := f.def(manifest.ID+"."+name, version)
	if d == nil || !slices.ContainsFunc(manifest.Flows, func(flow platform.Flow) bool { return flow.Name == name && flow.Version == version }) {
		return platform.ReleaseAsset{}, fmt.Errorf("native flow version is not declared by its original owner")
	}
	definition := struct {
		platform.FlowDefinition
		Owners []string          `json:"owners"`
		From   map[string]string `json:"from,omitempty"`
		Start  struct {
			On       []string      `json:"on,omitempty"`
			Type     string        `json:"type,omitempty"`
			Every    time.Duration `json:"every,omitempty"`
			OnBehalf string        `json:"onBehalf,omitempty"`
			Begin    bool          `json:"begin,omitempty"`
			When     bool          `json:"when,omitempty"`
		} `json:"startDeclaration"`
		Mode     string             `json:"mode"`
		Policies []nativeStepPolicy `json:"policies"`
	}{FlowDefinition: flowDefinitionOf(d), Owners: slices.Clone(d.Owners), From: d.From}
	definition.Start.On, definition.Start.Type, definition.Start.Every, definition.Start.OnBehalf = slices.Clone(d.Start.On), d.Start.Type, d.Start.Every, d.Start.OnBehalf
	definition.Start.Begin, definition.Start.When = d.Start.Begin != nil, d.Start.When != nil
	for _, step := range d.Steps {
		definition.Policies = append(definition.Policies, nativeStepPolicy{Name: step.Name, Retry: step.Retry, Timeout: step.Timeout, WorkingDays: step.WorkingDays, OnTimeout: step.OnTimeout, Fault: step.Fault, NoRetry: step.NoRetry, All: slices.Clone(step.All), Any: slices.Clone(step.Any), Next: step.Next, Output: step.Output != nil, Choose: step.Choose != nil, Undo: step.Undo != nil, End: step.End, LoopControl: step.LoopControl})
	}
	switch {
	case d.Start.Manual:
		definition.Mode = "manual"
	case d.Start.Every > 0:
		definition.Mode = "scheduled"
	case d.Start.Type != "":
		definition.Mode = "state"
	default:
		definition.Mode = "event"
	}
	raw, err := json.Marshal(definition)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	descriptor := platform.FlowReleaseDescriptor{Native: true, Name: manifest.ID + "." + name, Definition: raw}
	for _, ref := range requires {
		switch ref.Kind {
		case platform.AssetAction:
			descriptor.Actions = append(descriptor.Actions, ref)
		case platform.AssetFunction:
			descriptor.Functions = append(descriptor.Functions, platform.AssetBinding{Ref: ref, SourceVersion: manifest.Version})
		case platform.AssetCompute:
			descriptor.Operations = append(descriptor.Operations, platform.AssetBinding{Ref: ref, SourceVersion: manifest.Version})
		case platform.AssetQuery:
			descriptor.Queries = append(descriptor.Queries, platform.AssetBinding{Ref: ref, SourceVersion: manifest.Version})
		default:
			descriptor.Dependencies = append(descriptor.Dependencies, ref)
		}
	}
	if d.Subject != "" {
		descriptor.Subject = platform.AssetRef{App: manifest.ID, Kind: platform.AssetObject, Name: d.Subject}
	}
	raw, err = json.Marshal(descriptor)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetFlow, Name: descriptor.Name}, ContractVersion: 1, SourceVersion: manifest.Version + ".native-flow-" + strconv.Itoa(version), Body: raw})
}
