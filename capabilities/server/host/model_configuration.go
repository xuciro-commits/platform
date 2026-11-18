package host

import (
	"context"

	"platformserver/platform"
	"platformserver/services/ai/gateway"
)

func (t *Tenant) ModelSelection(ctx context.Context, m platform.Member, purpose string) (gateway.ModelSelection, *platform.Error) {
	return recordRead(t, ctx, func(t *Tenant) (gateway.ModelSelection, *platform.Error) {
		return t.modelResolver().Selection(m, purpose, t.apps, func() ([]platform.AIModel, *platform.Error) {
			models, err := t.read(m, "ai-models")
			if err != nil {
				return nil, err
			}
			return models.([]platform.AIModel), nil
		})
	})
}
func (t *Tenant) modelResolver() gateway.Resolver {
	return gateway.ResolverFor(func(app, key string) string { return t.setting(t.automation(app, false), key) }, t.services.AIInference, t.Models)
}
