package platformserver

import (
	"fmt"
	"slices"

	"platformserver/internal/host"
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

// DraftActionCapabilities is a builder-only view of saved authoring metadata.
// It does not add these actions to Definitions, Capabilities or execution.
func (t *Tenant) DraftActionCapabilities(m platform.Member) ([]platform.CapabilityDescriptor, error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/build/drafts/actions", "read", []string{build.Builder}, true)); err != nil {
		return nil, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(host.ConstructionDrafts)
	if !ok {
		return nil, fmt.Errorf("tenant has no builder")
	}
	actions, err := owner.SavedObjectActions(c)
	if err != nil {
		return nil, err
	}
	result := []platform.CapabilityDescriptor{}
	for _, action := range actions {
		if action.Automation {
			continue
		}
		block := actionBlock(platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: action.Schema}, "draft", "", action)
		block.Side = platform.CapabilityAuthoritative
		block.Parameters = slices.Clone(action.Payload)
		input := platform.CapabilityFieldsSchema(action.Payload)
		if input.Type != "" {
			block.Input = &input
		}
		result = append(result, block)
	}
	return result, nil
}
