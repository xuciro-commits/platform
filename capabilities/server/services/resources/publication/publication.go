package publication

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

type Family struct {
	ID        string
	Name      string
	State     string
	Version   int
	Published string
	Versions  []string
	Archived  bool
}

func Read[T any](raw string) (T, bool) {
	var out T
	if raw == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return out, false
	}
	return out, true
}

func Inventory[T any](c platform.Caller) ([]T, error) {
	var all []T
	for {
		rows, count, err := c.FindOf(reflect.TypeFor[T](), platform.Query{Limit: 500, Offset: len(all), Sort: []string{"id"}})
		if err != nil {
			return nil, err
		}
		if count > 1000 {
			return nil, fmt.Errorf("builder release inventory exceeds 1000 definitions of one kind")
		}
		for _, row := range rows {
			all = append(all, row.(T))
		}
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("builder definition inventory is incomplete")
		}
	}
}

func PublishRetained[T any](c platform.Caller, record *T, get func(*T) Family, set func(*T, Family), limit string, image func(*T) string, install func(platform.Caller, T) error) *kernel.Error {
	family := get(record)
	if family.Version >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, limit)
	}
	family.Version++
	family.State = "published"
	set(record, family)
	family.Published = image(record)
	family.Versions = append(family.Versions, family.Published)
	set(record, family)
	if !c.Staging() {
		if err := install(c, *record); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
	}
	return nil
}

func Decode[T any](image []byte, label string, get func(T) Family, check func(T) error, keeps func(T, T) bool) (selected T, err error) {
	var record T
	if err := json.Unmarshal(image, &record); err != nil {
		return selected, err
	}
	family := get(record)
	if !slices.Contains([]string{"draft", "published"}, family.State) || family.Version < 1 || family.Version > 64 || len(family.Versions) != family.Version || !slices.Contains(family.Versions, family.Published) {
		return selected, fmt.Errorf("accepted %s has an invalid version family", label)
	}
	var latest T
	for i, raw := range family.Versions {
		f, ok := Read[T](raw)
		entry := get(f)
		if !ok || entry.State != "published" || entry.ID != family.ID || entry.Version != i+1 || entry.Published != "" || len(entry.Versions) != 0 || check(f) != nil || i > 0 && !keeps(latest, f) {
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

func Retained[D any, T any](installed map[string]T, inventory func() ([]T, error), decode func([]byte) (T, error), name string, version int, get func(T) Family, definition func(T) D) (out D, ordinal int, ok bool) {
	if record, found := installed[name]; found {
		family := get(record)
		if version == 0 || version == family.Version {
			return definition(record), family.Version, true
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
		family := get(record)
		if version < 1 || version > len(family.Versions) {
			continue
		}
		raw, _ := json.Marshal(record)
		latest, err := decode(raw)
		if err != nil || get(latest).Name != name {
			continue
		}
		f, found := Read[T](family.Versions[version-1])
		if found && get(f).Version == version {
			return definition(f), version, true
		}
	}
	return out, 0, false
}

func ReleaseAsset(ref platform.AssetRef, version string, definition any) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(definition)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	contract := 1
	if link, ok := definition.(platform.LinkType); ok {
		contract = link.Contract()
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: contract, SourceVersion: version, Body: body})
}

func RetainedAsset[D any](app, source, prefix, label string, kind platform.AssetKind, name, versionText string, lookup func(string, int) (D, int, bool)) (platform.ReleaseAsset, error) {
	ordinalText, found := strings.CutPrefix(versionText, source+"."+prefix+"-")
	version, err := strconv.Atoi(ordinalText)
	if !found || err != nil || strconv.Itoa(version) != ordinalText {
		return platform.ReleaseAsset{}, fmt.Errorf("invalid retained %s version %s", label, versionText)
	}
	definition, ordinal, ok := lookup(name, version)
	if !ok || ordinal != version || version < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("%s %s version %d is not retained", kind, name, version)
	}
	return ReleaseAsset(platform.AssetRef{App: app, Kind: kind, Name: name}, versionText, definition)
}

func Prepare[T any](app, sourceVersion string, assets []platform.ReleaseAsset, kind platform.AssetKind, prefix, schema string, inventory func() ([]T, error), get func(T) Family, set func(*T, Family), resolve func(string, string) (platform.ReleaseAsset, error), freeze func(T, platform.ReleaseAsset, int) (T, error)) ([]platform.ReleasePublication, error) {
	records, err := inventory()
	if err != nil {
		return nil, err
	}
	var out []platform.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != app || asset.Ref.Kind != kind {
			continue
		}
		text, found := strings.CutPrefix(asset.SourceVersion, sourceVersion+"."+prefix+"-")
		version, err := strconv.Atoi(text)
		if !found || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != text {
			return nil, fmt.Errorf("invalid %s version", kind)
		}
		index := slices.IndexFunc(records, func(record T) bool {
			family := get(record)
			return !family.Archived && family.Name == asset.Ref.Name
		})
		if index < 0 {
			return nil, fmt.Errorf("%s has no source owner", kind)
		}
		record := records[index]
		family := get(record)
		if version <= family.Version {
			prior, err := resolve(asset.Ref.Name, asset.SourceVersion)
			same := err == nil && EqualAsset(prior, asset)
			if !same {
				return nil, fmt.Errorf("%s retained bytes differ", kind)
			}
			if len(family.Versions) != family.Version {
				return nil, fmt.Errorf("retained %s history is unavailable", kind)
			}
			selected := family.Versions[version-1]
			if selected == family.Published {
				continue
			}
			family.Published = selected
			if version < family.Version {
				family.State = "draft"
			}
			set(&record, family)
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

func EqualAsset(a, z platform.ReleaseAsset) bool {
	if a.Ref != z.Ref || a.SourceVersion != z.SourceVersion || a.ContractVersion != z.ContractVersion || !slices.Equal(a.Requires, z.Requires) {
		return false
	}
	if a.Provides != nil && z.Provides != nil && !reflect.DeepEqual(a.Provides, z.Provides) {
		return false
	}
	var left, right any
	return json.Unmarshal(a.Body, &left) == nil && json.Unmarshal(z.Body, &right) == nil && reflect.DeepEqual(left, right)
}

func InstalledAssets[T any](installed map[string]T, get func(T) Family, release func(T) (platform.ReleaseAsset, error)) ([]platform.ReleaseAsset, error) {
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
