package build

import (
	"encoding/json"
	"fmt"
	"maps"
	releasepub "platformserver/services/resources/publication"
	"reflect"
	"slices"
	"strconv"
	"strings"

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

func definitionDeclarations[T interface{ Definition() D }, D any](installed map[string]T) []D {
	names := slices.Sorted(maps.Keys(installed))
	out := make([]D, 0, len(names))
	for _, name := range names {
		out = append(out, installed[name].Definition())
	}
	return out
}

func (b *Build) definitionAssetVersion(asset platform.ReleaseAsset, kind platform.AssetKind, prefix string) int {
	return b.resolver.DefinitionAssetVersion(asset, ID, ValueDefinitionVersion, kind, prefix)
}

func prepareDefinitionPublications[T any](b *Build, assets []platform.ReleaseAsset, inventory func() ([]T, error), kind platform.AssetKind, prefix, schema, label string, resolve func(string, string) (platform.ReleaseAsset, error), freeze func(T, platform.ReleaseAsset, int) (T, error)) ([]platform.ReleasePublication, error) {
	records, err := inventory()
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != kind {
			continue
		}
		text, found := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+"."+prefix+"-")
		version, err := strconv.Atoi(text)
		if !found || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != text {
			return nil, fmt.Errorf("invalid %s version", label)
		}
		index := slices.IndexFunc(records, func(record T) bool {
			family := definitionFamilyOf(record)
			return !family.Archived && family.Name == asset.Ref.Name
		})
		if index < 0 {
			return nil, fmt.Errorf("%s has no source owner", label)
		}
		record := records[index]
		family := definitionFamilyOf(record)
		if version <= family.Version {
			prior, err := resolve(asset.Ref.Name, asset.SourceVersion)
			same := err == nil
			if same {
				if kind == platform.AssetFunction {
					same = b.data.EqualAsset(prior, asset)
				} else {
					var retained, candidate any
					same = json.Unmarshal(prior.Body, &retained) == nil && json.Unmarshal(asset.Body, &candidate) == nil && reflect.DeepEqual(retained, candidate)
				}
			}
			if !same {
				return nil, fmt.Errorf("%s retained bytes differ", label)
			}
			if len(family.Versions) != family.Version {
				return nil, fmt.Errorf("retained %s history is unavailable", label)
			}
			selected := family.Versions[version-1]
			if selected == family.Published {
				continue
			}
			family.Published = selected
			if version < family.Version {
				family.State = "draft"
			}
			setDefinitionFamily(&record, family)
		} else {
			record, err = freeze(record, asset, version)
			if err != nil {
				return nil, err
			}
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.ReleasePublication{Schema: schema, Image: raw})
	}
	return out, nil
}

func draftDefinitionAssets[T any](b *Build, id string, kind platform.AssetKind, label string, inventory func() ([]T, error), check func(T) (T, error), release func(T) (platform.ReleaseAsset, error)) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	records, problem := inventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(records, func(record T) bool {
		family := definitionFamilyOf(record)
		return family.ID == id && !family.Archived
	})
	if i < 0 {
		err = fmt.Errorf("draft %s %q not found", kind, id)
		return
	}
	record := records[i]
	family := definitionFamilyOf(record)
	if old, ok := releasepub.Read[T](family.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: kind, Name: definitionFamilyOf(old).Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: kind, Name: family.Name}
	record, err = check(record)
	if err != nil {
		return
	}
	if family.Version >= 64 {
		err = fmt.Errorf("%s version family is full", label)
		return
	}
	asset, problem := release(record)
	if problem != nil {
		err = problem
		return
	}
	after = slices.Clone(before)
	if hadPrior {
		after = slices.DeleteFunc(after, func(a platform.ReleaseAsset) bool { return a.Ref == prior })
	}
	after = append(after, asset)
	return
}

func applyDefinitionHistory[T any](image []byte, decode func([]byte) (T, error), install func(platform.Caller, T) error) error {
	selected, err := decode(image)
	if err != nil {
		return err
	}
	var record T
	if err := json.Unmarshal(image, &record); err != nil {
		return err
	}
	for _, raw := range definitionFamilyOf(record).Versions {
		version, _ := releasepub.Read[T](raw)
		if err := install(platform.Caller{Replaying: true}, version); err != nil {
			return err
		}
	}
	return install(platform.Caller{Replaying: true}, selected)
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
