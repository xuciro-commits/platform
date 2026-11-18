package host

import (
	"platformserver/services/ai/gateway"
	"platformserver/services/ai/gateway/providers"

	"platformserver/platform"
)

func (t *Tenant) ProviderModels(m platform.Member, provider string, refresh bool) ([]platform.CatalogModel, *platform.Error, *platform.AIError) {
	return aigateway.ProviderCatalog{Tenant: t.ID, Available: t.Models != nil,
		Authorize: func() *platform.Error {
			c := platform.NewCaller(runtime{t: t}, m, platform.AIApp, false, false)
			return authorizationRefusal(t.authorizeNative(c, "GET /v1/ai/providers/{provider}/models", "read", []string{platform.AIAdmin}, true))
		},
		Provider: func(id string) (platform.AIProvider, bool) { return t.Models.Provider(id) },
		Credential: func(pv platform.AIProvider) ([]byte, error) {
			secret, _, err := t.resolveCredential(pv.Secret, "ai/"+pv.ID, pv.BaseURL)
			return secret, err
		},
		Version: func(secret string) int {
			if t.IdentityOwner != nil {
				if credential, ok := t.IdentityOwner.Credential(secret); ok {
					return credential.Version
				}
			}
			return 0
		},
		Client: func(pv platform.AIProvider) providers.Client { return t.modelProvider(pv) },
	}.Models(provider, refresh)
}
