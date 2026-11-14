package host

import (
	"context"

	"platformserver/platform"
	"platformserver/services/ai/gateway"
)

func (t *Tenant) ModelSelection(ctx context.Context, m platform.Member, purpose string) (aigateway.ModelSelection, *platform.Error) {
	return recordRead(t, ctx, func(t *Tenant) (aigateway.ModelSelection, *platform.Error) {
		return t.modelResolver().Selection(m, purpose, t.apps, func() ([]platform.AIModel, *platform.Error) {
			models, err := t.read(m, "ai-models")
			if err != nil {
				return nil, err
			}
			return models.([]platform.AIModel), nil
		})
	})
}
func (t *Tenant) modelResolver() aigateway.Resolver {
	return aigateway.ResolverFor(func(app, key string) string { return t.setting(t.automation(app, false), key) }, t.services.AIInference, t.ai)
}
