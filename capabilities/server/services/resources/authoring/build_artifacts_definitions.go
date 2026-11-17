package build

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/publication"
)

func (*Build) DefinitionMetadataKind(kind platform.AssetKind) bool {
	return dataowner.IsIntegration(kind) || slices.Contains([]platform.AssetKind{platform.AssetEvaluation, platform.AssetTestPlan, platform.AssetSampleSet}, kind)
}

func (b *Build) DefinitionDescriptor(asset platform.ReleaseAsset) (platform.Definition, error) {
	definition := platform.Definition{Ref: asset.Ref, Source: "builder", Version: asset.SourceVersion, ContractVersion: asset.ContractVersion, Requires: slices.Clone(asset.Requires), Provides: slices.Clone(asset.Provides)}
	var err error
	switch asset.Ref.Kind {
	case platform.AssetObservation:
		var observation platform.ObservationDefinition
		if err = json.Unmarshal(asset.Body, &observation); err == nil {
			err = observation.Check()
		}
		definition.Observation = &observation
	case platform.AssetSampleSet:
		var samples platform.SampleSet
		if err = json.Unmarshal(asset.Body, &samples); err == nil {
			err = samples.Check()
		}
		definition.SampleSet = &samples
	case platform.AssetEvaluation:
		var descriptor platform.EvaluationSetDescriptor
		descriptor, err = platform.ReadEvaluationSet(asset.Body)
		definition.Evaluation = &descriptor
	case platform.AssetTestPlan:
		var descriptor platform.TestPlanDescriptor
		descriptor, err = platform.ReadTestPlan(asset.Body)
		definition.TestPlan = &descriptor
	case platform.AssetAgent:
		var descriptor platform.AgentReleaseDescriptor
		if json.Unmarshal(asset.Body, &descriptor) != nil || descriptor.Check() != nil {
			return definition, fmt.Errorf("agent descriptor is unavailable")
		}
		definition.Agent = &descriptor
	}
	if err != nil {
		return definition, err
	}
	if definition.Provides == nil {
		descriptor, err := b.resolver.DescribeAsset(asset)
		if err != nil {
			return definition, err
		}
		definition.Provides = descriptor.Provides
	}
	return definition, nil
}

type definitionFamily = publication.DefinitionFamily

func definitionFamilyOf(record any) definitionFamily {
	return publication.DefinitionFamilyOf(record)
}

func setDefinitionFamily(record any, family definitionFamily) {
	publication.SetDefinitionFamily(record, family)
}

func (b *Build) definitionAssetVersion(asset platform.ReleaseAsset, kind platform.AssetKind, prefix string) int {
	return b.resolver.DefinitionAssetVersion(asset, ID, ValueDefinitionVersion, kind, prefix)
}

func restoreDefinitionPublications[T any](b *Build, inventory func() ([]T, error), schema string) error {
	records, err := inventory()
	if err != nil {
		return err
	}
	for _, record := range records {
		if definitionFamilyOf(record).Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		if err := b.ApplyAcceptedPublication(schema, raw); err != nil {
			return err
		}
	}
	return nil
}
