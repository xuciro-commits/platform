// Package aigateway owns admission to model inference, separate from business
// execution and the original AI owner's metering authority.
package aigateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"platformserver/platform"
	"slices"
	"strings"
)

func Origin(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("The model provider needs an HTTP origin without credentials or query parameters")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}

func CheckPolicy(policy platform.AIInferencePolicy) error {
	seen := map[string]bool{}
	for _, site := range policy.Runtime {
		origin, err := Origin(site.Origin)
		if err != nil || strings.TrimSuffix(site.Origin, "/") != origin || site.Provider == "" || site.Location != "local" && site.Location != "edge" {
			return fmt.Errorf("Each runtime inference approval needs an exact provider, origin and local or edge location")
		}
		if seen[site.Provider+"|"+origin] {
			return fmt.Errorf("The runtime inference approval is duplicated")
		}
		seen[site.Provider+"|"+origin] = true
	}
	return nil
}

// Bind is called by a trusted accepting owner, never from a model or HTTP stage
// field. The complete policy revision is pinned alongside the original model.
func Bind(policy platform.AIInferencePolicy, provider platform.AIProvider, binding platform.AIModelBinding, stage string) (platform.AIModelBinding, error) {
	if err := CheckPolicy(policy); err != nil {
		return binding, err
	}
	if stage != platform.AIStageDesign && stage != platform.AIStageRuntime {
		return binding, fmt.Errorf("The model request has no trusted execution stage; start a new request")
	}
	origin, err := Origin(provider.BaseURL)
	if err != nil {
		return binding, err
	}
	sites := slices.Clone(policy.Runtime)
	slices.SortFunc(sites, func(a, b platform.AIInferenceSite) int {
		return strings.Compare(a.Provider+"|"+a.Origin, b.Provider+"|"+b.Origin)
	})
	raw, _ := json.Marshal(struct {
		Version string
		Sites   []platform.AIInferenceSite
	}{"ai-stage.v1", sites})
	digest := sha256.Sum256(raw)
	binding.Stage, binding.PolicyRevision, binding.ProviderOrigin, binding.InferenceLocation = stage, hex.EncodeToString(digest[:]), origin, "cloud"
	for _, site := range sites {
		if site.Provider == provider.ID && strings.TrimSuffix(site.Origin, "/") == origin {
			binding.InferenceLocation = site.Location
			break
		}
	}
	if stage == platform.AIStageRuntime && binding.InferenceLocation == "cloud" {
		return binding, fmt.Errorf("Runtime inference requires an operator-approved local or edge provider; cloud models are limited to design tasks")
	}
	return binding, nil
}

func Check(policy platform.AIInferencePolicy, provider platform.AIProvider, binding platform.AIModelBinding) error {
	current, err := Bind(policy, provider, binding, binding.Stage)
	if err != nil {
		return err
	}
	if binding.PolicyRevision == "" || current.PolicyRevision != binding.PolicyRevision || current.ProviderOrigin != binding.ProviderOrigin || current.InferenceLocation != binding.InferenceLocation {
		return fmt.Errorf("The inference deployment policy changed; start a new request")
	}
	return nil
}
