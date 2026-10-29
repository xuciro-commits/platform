package platformserver

import (
	"platformserver/aigateway"
	controlowner "platformserver/kernel/control"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

// Directory reads remain volatile. Only an explicit model enable/configure
// decision adopts selected facts into the original AI owner's accepted state.
func (t *Tenant) ProviderModels(m platform.Member, provider string, refresh bool) ([]platform.CatalogModel, *kernel.Error, *platform.AIError) {
	if t.ai == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	c := platform.NewCaller(runtime{t}, m, platform.AIApp, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/ai/providers/{provider}/models", "read", []string{platform.AIAdmin}, true)); err != nil {
		return nil, err, nil
	}
	pv, ok := t.ai.Provider(provider)
	if !ok {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, nil
	}
	// Resolve current eligibility before consulting a cache. Disabled credentials
	// must never produce a stale successful connection check.
	secret, _, err := t.resolveCredential(pv.Secret, "ai/"+pv.ID, pv.BaseURL)
	if err != nil && (pv.Kind != "local" || pv.Secret != "") {
		return nil, nil, &platform.AIError{Status: 400, Detail: err.Error()}
	}
	version := 0
	if owner, ok := t.app(controlowner.PlatformApp).(platform.Credentials); ok {
		if credential, ok := owner.Credential(pv.Secret); ok {
			version = credential.Version
		}
	}
	models, failure := aigateway.ReadModels(t.ID, pv, secret, version, refresh, t.modelProvider(pv))
	return models, nil, failure
}
