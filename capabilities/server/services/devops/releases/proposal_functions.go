package releases

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
	"platformserver/services/ontology"
)

func CheckProposalFunctions(candidate platform.ReleaseCandidate) error {
	functions := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetFunction {
			functions[asset.Ref] = asset
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object ontology.Object
		if json.Unmarshal(asset.Body, &object) != nil {
			return fmt.Errorf("Proposal object declaration is invalid")
		}
		for _, action := range object.Actions {
			if action.Adoption == nil {
				continue
			}
			ref := platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetFunction, Name: action.Adoption.Function.Name}
			functionAsset, ok := functions[ref]
			if !ok {
				return fmt.Errorf("Application adoption needs its declared function in the candidate")
			}
			definition, version, err := functionauthoring.AssetDefinition(functionAsset)
			if err != nil {
				return err
			}
			if action.Adoption.Function.Version != 0 && action.Adoption.Function.Version != version {
				return fmt.Errorf("Application adoption function version differs from the candidate")
			}
			inputs := []platform.Field{}
			for _, input := range action.Inputs {
				inputs = append(inputs, input.Field())
			}
			if err := action.Adoption.CheckMapping(definition, inputs); err != nil {
				return err
			}
		}
	}
	return nil
}
