package host

import (
	"context"
	"fmt"

	"platformserver/services/functions/authoring"
	"platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (t *Tenant) DraftActionCapabilities(m platform.Member) ([]platform.CapabilityDescriptor, error) {
	return readWithError(t, context.Background(), func(t *Tenant) ([]platform.CapabilityDescriptor, error) { return t.scopedDraftActionCapabilities(m) })
}

func (t *Tenant) scopedDraftActionCapabilities(m platform.Member) ([]platform.CapabilityDescriptor, error) {
	if err := t.Admits(m); err != nil {
		return nil, err
	}
	c := platform.NewCaller(runtime{t: t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/build/drafts/actions", "read", []string{build.Builder}, true)); err != nil {
		return nil, err
	}
	owner := t.builders[build.ID]
	if owner == nil {
		return nil, fmt.Errorf("tenant has no builder")
	}
	actions, err := owner.SavedObjectActions(c)
	if err != nil {
		return nil, err
	}
	return authoring.DraftActionBlocks(build.ID, actions), nil
}
