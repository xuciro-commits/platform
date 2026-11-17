package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"platformserver/platform"
	dataowner "platformserver/services/data"
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

type definitionFamily struct {
	platform.Record
	Name, State string
	Version     int
	Published   string
	Versions    []string
}

func definitionFamilyOf(record any) definitionFamily {
	value := reflect.ValueOf(record)
	version, ok := definitionVersionField(value)
	if !ok || value.Kind() != reflect.Struct || !value.FieldByName("Name").IsValid() {
		panic("unsupported retained definition")
	}
	return definitionFamily{
		Record:    value.FieldByName("Record").Interface().(platform.Record),
		Name:      value.FieldByName("Name").String(),
		State:     value.FieldByName("State").String(),
		Version:   int(version.Int()),
		Published: value.FieldByName("Published").String(),
		Versions:  value.FieldByName("Versions").Interface().([]string),
	}
}

func setDefinitionFamily(record any, family definitionFamily) {
	value := reflect.ValueOf(record)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		panic("unsupported retained definition")
	}
	value = value.Elem()
	version, ok := definitionVersionField(value)
	if !ok {
		panic("unsupported retained definition")
	}
	value.FieldByName("Record").Set(reflect.ValueOf(family.Record))
	value.FieldByName("State").SetString(family.State)
	version.SetInt(int64(family.Version))
	value.FieldByName("Published").SetString(family.Published)
	value.FieldByName("Versions").Set(reflect.ValueOf(family.Versions))
}

func definitionVersionField(value reflect.Value) (reflect.Value, bool) {
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	if version := value.FieldByName("Version"); version.IsValid() {
		return version, true
	}
	if version := value.FieldByName("DefinitionVersion"); version.IsValid() {
		return version, true
	}
	return reflect.Value{}, false
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
