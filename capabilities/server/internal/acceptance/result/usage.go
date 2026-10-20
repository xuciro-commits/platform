package result

import (
	"fmt"
	"platformserver/internal/acceptance"
	"platformserver/platform"
	"time"
)

type Usage struct {
	Authorizations []AuthorizationEvidence `json:"authorizations,omitempty"`
	Version        int                     `json:"version"`
	Kind           string                  `json:"kind"`
	Tenant         string                  `json:"tenant"`
	App            string                  `json:"app"`
	Member         string                  `json:"member"`
	Key            string                  `json:"key"`
	At             time.Time               `json:"at"`
	RequestHash    string                  `json:"requestHash"`
	Digest         string                  `json:"digest"`
	Before         string                  `json:"before"`
	Usage          platform.AIUsage        `json:"usage"`
}

func ValidUsage(u platform.AIUsage) bool {
	remaining := u.Input
	for _, count := range []*int{u.CacheReadInput, u.CacheWriteInput} {
		if count != nil {
			if *count < 0 || *count > remaining {
				return false
			}
			remaining -= *count
		}
	}
	return u.Member != "" && u.Model != "" && !u.At.IsZero() && u.Input >= 0 && u.Output >= 0 && u.Cost >= 0 && u.Millis >= 0
}

func UsageHash(before string, u platform.AIUsage) (string, error) {
	return acceptance.Digest([]any{before, u})
}

func DigestUsage(r Usage) (string, error) { r.Digest = ""; return acceptance.Digest(r) }

func DecodeUsage(raw []byte) (Usage, error) {
	var r Usage
	if len(raw) == 0 || len(raw) > acceptance.MaxResultBytes {
		return r, fmt.Errorf("usage result is empty or too large")
	}
	if err := acceptance.DecodeJSON(raw, &r, "usage result"); err != nil {
		return r, err
	}
	if err := ValidateAuthorizationEvidence(r.Tenant, r.Authorizations); err != nil {
		return r, err
	}

	hash, err := UsageHash(r.Before, r.Usage)
	digest, digestErr := DigestUsage(r)
	if err != nil || digestErr != nil || r.Version != 1 || r.Kind != "usage-result" || r.Tenant == "" || r.App != platform.AIApp || r.Before == "" ||
		r.Member != r.Usage.Member || !ValidUsage(r.Usage) || !r.At.Equal(r.Usage.At) || r.RequestHash != hash || r.Key != "usage:"+hash || r.Digest != digest {
		return r, fmt.Errorf("usage result has inconsistent identity, reading or digest")
	}
	return r, nil
}
