package host

import (
	"platformserver/services/ai/gateway"
	"platformserver/services/ai/gateway/providers"
	controlowner "platformserver/services/security/control"

	"platformkernel/kernel"

	"platformserver/platform"
)

func (t *Tenant) ProviderModels(m platform.Member, provider string, refresh bool) ([]platform.CatalogModel, *kernel.Error, *platform.AIError) {
	return aigateway.ProviderCatalog{Tenant: t.ID, Available: t.ai != nil,
		Authorize: func() *kernel.Error {
			c := platform.NewCaller(runtime{t: t}, m, platform.AIApp, false, false)
			return authorizationRefusal(t.authorizeNative(c, "GET /v1/ai/providers/{provider}/models", "read", []string{platform.AIAdmin}, true))
		},
		Provider: func(id string) (platform.AIProvider, bool) { return t.ai.Provider(id) },
		Credential: func(pv platform.AIProvider) ([]byte, error) {
			secret, _, err := t.resolveCredential(pv.Secret, "ai/"+pv.ID, pv.BaseURL)
			return secret, err
		},
		Version: func(secret string) int {
			if owner, ok := t.App(controlowner.PlatformApp).(platform.Credentials); ok {
				if credential, ok := owner.Credential(secret); ok {
					return credential.Version
				}
			}
			return 0
		},
		Client: func(pv platform.AIProvider) providers.Client { return t.modelProvider(pv) },
	}.Models(provider, refresh)
}
