package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func alertReleaseAsset(a construction.Alert, version int) (platform.ReleaseAsset, error) {
	if version < 1 || version > 64 {
		return platform.ReleaseAsset{}, fmt.Errorf("an alert may retain at most 64 published versions")
	}
	return platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetAlert, Name: a.Name}, ContractVersion: 1, SourceVersion: construction.ValueDefinitionVersion + ".alert-" + strconv.Itoa(version), Body: platform.Raw(a.Definition())})
}

func sameRuleAsset(left, right platform.ReleaseAsset) bool {
	var a, b any
	return json.Unmarshal(platform.Raw(left), &a) == nil && json.Unmarshal(platform.Raw(right), &b) == nil && reflect.DeepEqual(a, b)
}

func alertFromAsset(asset platform.ReleaseAsset) (construction.Alert, int, error) {
	var d platform.AlertRule
	text, found := strings.CutPrefix(asset.SourceVersion, construction.ValueDefinitionVersion+".alert-")
	version, err := strconv.Atoi(text)
	if !found || err != nil || strconv.Itoa(version) != text || version < 1 || version > 64 || asset.Ref.App != construction.ID || asset.Ref.Kind != platform.AssetAlert || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &d) != nil || d.Name != asset.Ref.Name || d.Check() != nil || platform.CheckAssetDependencies(asset) != nil {
		return construction.Alert{}, 0, fmt.Errorf("invalid alert asset %s", asset.Ref)
	}
	return construction.Alert{Name: d.Name, Title: d.Title, Object: d.Object.Name, Field: d.Field, Operator: d.Operator, Value: d.Value, Message: d.Message, Role: d.Role, Every: d.Every, Active: d.Active}, version, nil
}

// Only records written before alert publication existed use their original
// bytes. The first edit freezes that version before changing the draft.
func legacyAlert(a construction.Alert) bool {
	return a.State == "" && a.Version == 0 && a.Published == ""
}

func freezeLegacyAlert(a *construction.Alert) error {
	if !legacyAlert(*a) {
		return nil
	}
	asset, err := alertReleaseAsset(*a, 1)
	if err != nil {
		return err
	}
	return retainAlertAsset(a, asset)
}

func retainAlertAsset(a *construction.Alert, asset platform.ReleaseAsset) error {
	_, version, err := alertFromAsset(asset)
	if err != nil {
		return err
	}
	raw := string(platform.Raw(asset))
	if !slices.Contains(a.Versions, raw) {
		a.Versions = append(slices.Clone(a.Versions), raw)
	}
	a.Version = max(a.Version, version)
	a.DefinitionVersion = version
	a.State, a.Published = "published", raw
	return nil
}

func alertImage(image []byte) (construction.Alert, platform.ReleaseAsset, error) {
	var a construction.Alert
	var asset platform.ReleaseAsset
	if json.Unmarshal(image, &a) != nil || a.ID == "" || a.State != "published" || json.Unmarshal([]byte(a.Published), &asset) != nil {
		return a, asset, fmt.Errorf("accepted alert has no immutable publication")
	}
	frozen, version, err := alertFromAsset(asset)
	if err != nil || frozen.Name != a.Name || version != a.DefinitionVersion || version > a.Version || a.Version > 64 {
		return a, asset, fmt.Errorf("accepted alert identity differs from its publication")
	}
	return a, asset, nil
}

func (b *Build) alertAssets() ([]platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[construction.Alert](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return nil, err
	}
	var assets []platform.ReleaseAsset
	for _, a := range rows {
		if a.Archived {
			continue
		}
		if err := freezeLegacyAlert(&a); err != nil {
			return nil, err
		}
		if a.Published == "" {
			continue
		}
		_, asset, err := alertImage(platform.Raw(a))
		if err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, nil
}

func (b *Build) AlertRules(object string) ([]construction.Alert, error) {
	rows, err := readDefinitionInventory[construction.Alert](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return nil, err
	}
	var rules []construction.Alert
	for _, record := range rows {
		if record.Archived {
			continue
		}
		if err := freezeLegacyAlert(&record); err != nil {
			return nil, err
		}
		if record.Published == "" {
			continue
		}
		_, asset, err := alertImage(platform.Raw(record))
		if err != nil {
			return nil, err
		}
		a, _, err := alertFromAsset(asset)
		if err != nil {
			return nil, err
		}
		if a.Active && a.Object == object {
			a.Record = record.Record
			rules = append(rules, a)
		}
	}
	return rules, nil
}

func (b *Build) alertPinned(ref platform.AssetRef, version string) (platform.ReleaseAsset, error) {
	rows, err := readDefinitionInventory[construction.Alert](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	for _, record := range rows {
		if err := freezeLegacyAlert(&record); err != nil {
			return platform.ReleaseAsset{}, err
		}
		for _, raw := range append(slices.Clone(record.Versions), record.Published) {
			var asset platform.ReleaseAsset
			if json.Unmarshal([]byte(raw), &asset) == nil && asset.Ref == ref && asset.SourceVersion == version {
				_, _, err := alertFromAsset(asset)
				return asset, err
			}
		}
	}
	return platform.ReleaseAsset{}, fmt.Errorf("alert %s@%s is not retained", ref, version)
}

func (b *Build) alertDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	a, ok := platform.Get[construction.Alert](b.host.Automation(platform.Caller{}, construction.ID), id)
	if !ok || a.Archived {
		err = fmt.Errorf("alert draft %s was not found", id)
		return
	}
	if err = freezeLegacyAlert(&a); err != nil {
		return
	}
	if a.Published != "" {
		_, asset, problem := alertImage(platform.Raw(a))
		if problem != nil {
			err = problem
			return
		}
		prior, hadPrior = asset.Ref, true
	}
	if err = b.checkAlert(a); err != nil {
		return
	}
	next = platform.AssetRef{App: construction.ID, Kind: platform.AssetAlert, Name: a.Name}
	asset, problem := alertReleaseAsset(a, a.Version+1)
	if problem != nil {
		err = problem
		return
	}
	after = slices.DeleteFunc(slices.Clone(before), func(asset platform.ReleaseAsset) bool { return asset.Ref == next || hadPrior && asset.Ref == prior })
	after = append(after, asset)
	return
}

func (b *Build) prepareAlertPublications(assets []platform.ReleaseAsset) ([]construction.ReleasePublication, error) {
	rows, err := readDefinitionInventory[construction.Alert](b.host.Automation(platform.Caller{}, construction.ID))
	if err != nil {
		return nil, err
	}
	var out []construction.ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != construction.ID || asset.Ref.Kind != platform.AssetAlert {
			continue
		}
		frozen, version, err := alertFromAsset(asset)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(rows, func(a construction.Alert) bool { return !a.Archived && a.Name == frozen.Name })
		if i < 0 {
			return nil, fmt.Errorf("alert %s has no target draft", asset.Ref)
		}
		record := rows[i]
		if err := freezeLegacyAlert(&record); err != nil {
			return nil, err
		}
		if version <= record.Version {
			retained, err := b.alertPinned(asset.Ref, asset.SourceVersion)
			if err != nil || !sameRuleAsset(retained, asset) {
				return nil, fmt.Errorf("retained alert bytes differ")
			}
		} else if record.Version != 0 && version != record.Version+1 {
			return nil, fmt.Errorf("alert needs its next version or retained bytes")
		}
		if err := retainAlertAsset(&record, asset); err != nil {
			return nil, err
		}
		out = append(out, construction.ReleasePublication{Schema: SchemaAlertPublish, Image: platform.Raw(record)})
	}
	return out, nil
}
