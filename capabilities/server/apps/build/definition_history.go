package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// definitionFamily contains only the owner metadata shared by these
// retained declaration families. Their declaration checks remain type specific.
type definitionFamily struct {
	platform.Record
	Name, State string
	Version     int
	Published   string
	Versions    []string
}

func definitionFamilyOf(record any) definitionFamily {
	switch r := record.(type) {
	case Query:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case construction.Function:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case PropertyType:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case LinkType:
		return definitionFamily{r.Record, r.Name, r.State, r.Version, r.Published, r.Versions}
	case SampleSetRecord:
		return definitionFamily{r.Record, r.Name, r.State, r.DefinitionVersion, r.Published, r.Versions}
	case MigrationPlanRecord:
		return definitionFamily{r.Record, r.Name, r.State, r.DefinitionVersion, r.Published, r.Versions}
	}
	panic("unsupported retained definition")
}

func setDefinitionFamily(record any, family definitionFamily) {
	switch r := record.(type) {
	case *Query:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	case *construction.Function:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	case *PropertyType:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	case *LinkType:
		r.State, r.Version, r.Published, r.Versions = family.State, family.Version, family.Published, family.Versions
	default:
		panic("unsupported retained definition")
	}
}

func portableDefinitionAssets[T, D any](inventory func() ([]T, error), decode func(platform.ReleaseAsset) (D, int, error), label string) ([]platform.ReleaseAsset, error) {
	rows, err := inventory()
	if err != nil {
		return nil, err
	}
	out := []platform.ReleaseAsset{}
	for _, record := range rows {
		family := definitionFamilyOf(record)
		if family.Archived || family.Published == "" {
			continue
		}
		var asset platform.ReleaseAsset
		if json.Unmarshal([]byte(family.Published), &asset) != nil {
			return nil, fmt.Errorf("Invalid retained %s", label)
		}
		if _, _, err := decode(asset); err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

func pinnedDefinitionAsset[T, D any](inventory func() ([]T, error), decode func(platform.ReleaseAsset) (D, int, error), ref platform.AssetRef, version, label string) (platform.ReleaseAsset, error) {
	rows, err := inventory()
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		for _, raw := range definitionFamilyOf(record).Versions {
			var asset platform.ReleaseAsset
			if json.Unmarshal([]byte(raw), &asset) == nil && asset.Ref == ref && asset.SourceVersion == version {
				_, _, err := decode(asset)
				return asset, err
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("The %s version is not retained", label)
}

func portableDefinitionInstallation[T, D any](image json.RawMessage, typ, schema, label string, decode func(platform.ReleaseAsset) (D, int, error)) (platform.AssetInstallation, error) {
	var record T
	var asset platform.ReleaseAsset
	if json.Unmarshal(image, &record) != nil {
		return platform.AssetInstallation{}, fmt.Errorf("Invalid %s installation", label)
	}
	family := definitionFamilyOf(record)
	if family.ID == "" || family.State != "published" || json.Unmarshal([]byte(family.Published), &asset) != nil {
		return platform.AssetInstallation{}, fmt.Errorf("Invalid %s installation", label)
	}
	_, version, err := decode(asset)
	if err != nil || version != family.Version || asset.Ref.Name != family.Name || len(family.Versions) != version || family.Versions[len(family.Versions)-1] != family.Published {
		return platform.AssetInstallation{}, fmt.Errorf("Invalid %s version history", label)
	}
	for i, raw := range family.Versions {
		var prior platform.ReleaseAsset
		if json.Unmarshal([]byte(raw), &prior) != nil {
			return platform.AssetInstallation{}, fmt.Errorf("Invalid %s version history", label)
		}
		if _, ordinal, err := decode(prior); err != nil || ordinal != i+1 || prior.Ref.Name != family.Name {
			return platform.AssetInstallation{}, fmt.Errorf("Invalid %s version history", label)
		}
	}
	return platform.AssetInstallation{Ref: asset.Ref, Schema: schema, Record: typ + "/" + family.ID, Image: slices.Clone(image)}, nil
}

// Type-specific checks have already prepared the declaration. Its original
// installer still owns runtime changes; staged decisions retain only metadata.
func publishRetainedDefinition[T any](c platform.Caller, record *T, limitMessage string, install func(platform.Caller, T) error) *kernel.Error {
	family := definitionFamilyOf(*record)
	if family.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, limitMessage)
	}
	family.Version++
	family.State = "published"
	setDefinitionFamily(record, family)
	family.Published = published(*record)
	family.Versions = append(family.Versions, family.Published)
	setDefinitionFamily(record, family)
	if !c.Staging() {
		if err := install(c, *record); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	return nil
}

func definitionImage[T interface{ Definition() D }, D interface{ Check() error }](image []byte, label string, keeps func(T, T) bool) (selected T, err error) {
	var record T
	if err := json.Unmarshal(image, &record); err != nil {
		return selected, err
	}
	family := definitionFamilyOf(record)
	if !slices.Contains([]string{"draft", "published"}, family.State) || family.Version < 1 || family.Version > 64 || len(family.Versions) != family.Version || !slices.Contains(family.Versions, family.Published) {
		return selected, fmt.Errorf("accepted %s has an invalid version family", label)
	}
	var latest T
	for i, raw := range family.Versions {
		f, ok := wasPublished[T](raw)
		entry := definitionFamilyOf(f)
		if !ok || entry.State != "published" || entry.ID != family.ID || entry.Version != i+1 || entry.Published != "" || len(entry.Versions) != 0 || f.Definition().Check() != nil || i > 0 && !keeps(latest, f) {
			var zero T
			return zero, fmt.Errorf("accepted %s has a malformed version %d", label, i+1)
		}
		if raw == family.Published {
			selected = f
		}
		latest = f
	}
	return selected, nil
}

func retainedDefinition[T interface{ Definition() D }, D any](installed map[string]T, inventory func() ([]T, error), decode func([]byte) (T, error), name string, version int) (out D, ordinal int, ok bool) {
	if record, found := installed[name]; found {
		family := definitionFamilyOf(record)
		if version == 0 || version == family.Version {
			return record.Definition(), family.Version, true
		}
	}
	if version == 0 {
		return out, 0, false
	}
	records, err := inventory()
	if err != nil {
		return out, 0, false
	}
	for _, record := range records {
		family := definitionFamilyOf(record)
		if version < 1 || version > len(family.Versions) {
			continue
		}
		raw, _ := json.Marshal(record)
		latest, err := decode(raw)
		if err != nil || definitionFamilyOf(latest).Name != name {
			continue
		}
		f, found := wasPublished[T](family.Versions[version-1])
		if found && definitionFamilyOf(f).Version == version {
			return f.Definition(), version, true
		}
	}
	return out, 0, false
}

func definitionDeclarations[T interface{ Definition() D }, D any](installed map[string]T) []D {
	names := slices.Sorted(maps.Keys(installed))
	out := make([]D, 0, len(names))
	for _, name := range names {
		out = append(out, installed[name].Definition())
	}
	return out
}

func retainedDefinitionAsset[D any](source, prefix, label string, kind platform.AssetKind, name, versionText string, lookup func(string, int) (D, int, bool)) (platform.ReleaseAsset, error) {
	ordinalText, found := strings.CutPrefix(versionText, source+"."+prefix+"-")
	version, err := strconv.Atoi(ordinalText)
	if !found || err != nil || strconv.Itoa(version) != ordinalText {
		return platform.ReleaseAsset{}, fmt.Errorf("invalid retained %s version %s", label, versionText)
	}
	definition, ordinal, ok := lookup(name, version)
	if !ok || ordinal != version || version < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("%s %s version %d is not retained", kind, name, version)
	}
	return definitionReleaseAsset(platform.AssetRef{App: construction.ID, Kind: kind, Name: name}, versionText, definition)
}

func definitionReleaseAsset(ref platform.AssetRef, version string, definition any) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(definition)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	contract := 1
	if link, ok := any(definition).(platform.LinkType); ok {
		contract = link.Contract()
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: contract, SourceVersion: version, Body: body})
}

func (b *Build) definitionAssetVersion(asset platform.ReleaseAsset, kind platform.AssetKind, prefix string) int {
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+"."+prefix+"-")
	version, err := strconv.Atoi(ordinal)
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal || asset.Ref.App != construction.ID || asset.Ref.Kind != kind || platform.CheckAssetDependencies(asset) != nil {
		return 0
	}
	return version
}

func prepareDefinitionPublications[T any](b *Build, assets []platform.ReleaseAsset, inventory func() ([]T, error), kind platform.AssetKind, prefix, schema, label string, resolve func(string, string) (platform.ReleaseAsset, error), freeze func(T, platform.ReleaseAsset, int) (T, error)) ([]construction.ReleasePublication, error) {
	records, err := inventory()
	if err != nil {
		return nil, err
	}
	var out []construction.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != construction.ID || asset.Ref.Kind != kind {
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
					same = equalIntegrationAsset(prior, asset)
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
		out = append(out, construction.ReleasePublication{Schema: schema, Image: raw})
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
	if old, ok := wasPublished[T](family.Published); ok {
		prior = platform.AssetRef{App: construction.ID, Kind: kind, Name: definitionFamilyOf(old).Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: construction.ID, Kind: kind, Name: family.Name}
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
		version, _ := wasPublished[T](raw)
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

// Installed snapshots come from the validated owner cache, never editable rows.
func installedDefinitionAssets[T any](installed map[string]T, release func(T) (platform.ReleaseAsset, error)) ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	for _, name := range slices.Sorted(maps.Keys(installed)) {
		asset, err := release(installed[name])
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}
