package host

import (
	"fmt"
	"slices"

	"platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (t *Tenant) DraftActionCapabilities(m platform.Member) ([]platform.CapabilityDescriptor, error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if refusal := t.authorizeRead(c, "GET /v1/build/drafts/actions", "read", []string{build.Builder}); refusal != nil {
		return nil, refusal
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
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
		block := authoring.ActionBlock(platform.AssetRef{App: build.ID, Kind: platform.AssetAction, Name: action.Schema}, "draft", "", action)
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
