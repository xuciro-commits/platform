package aigateway

import (
	"encoding/json"
	"slices"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func EffectUsage(usage platform.AIUsage, effect platform.Effect) platform.AIUsage {
	usage.Applications = slices.Clone(effect.Applications)
	usage.Run = effect.ID
	if effect.Endpoint == acceptedformat.ModelEndpoint {
		var ask ModelAsk
		if json.Unmarshal([]byte(effect.Body), &ask) == nil && ask.Function != nil {
			owner := ask.Function.Owner
			if owner == "" {
				owner = effect.App
			}
			ref := platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: ask.Function.Definition.Name}
			usage.Resource = &ref
		}
	}
	return usage
}
