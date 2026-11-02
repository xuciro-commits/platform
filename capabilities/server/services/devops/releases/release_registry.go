package releases

import (
	"encoding/json"
	"fmt"
	"maps"

	"platformserver/platform"

	"slices"
	"strings"
)

func ImmutableCodeAssetMatches(current, retained platform.ReleaseAsset) (bool, error) {
	if retained.Ref != current.Ref || retained.SourceVersion != current.SourceVersion || retained.ContractVersion != current.ContractVersion {
		return false, nil
	}
	if retained.Provides != nil {
		before, err := platform.CanonicalDigest(current)
		if err != nil {
			return false, err
		}
		after, err := platform.CanonicalDigest(retained)
		return err == nil && before == after, err
	}
	if err := platform.CheckAssetDependencies(retained); err != nil {
		return false, nil
	}
	if err := platform.CheckAssetDependencies(current); err != nil {
		return false, err
	}
	for _, dependency := range retained.Requires {
		if !slices.Contains(current.Requires, dependency) {
			return false, nil
		}
	}
	var original, selected map[string]json.RawMessage
	if err := json.Unmarshal(retained.Body, &original); err != nil {
		return false, err
	}
	if err := json.Unmarshal(current.Body, &selected); err != nil {
		return false, err
	}
	for _, key := range []string{"objectBindings", "protocolBindings", "toolBindings"} {
		if _, exists := original[key]; !exists {
			delete(selected, key)
		}
	}
	if current.Ref.Kind == platform.AssetApp {
		var before, after []platform.AssetRef
		if err := json.Unmarshal(original["resources"], &before); err != nil && len(original["resources"]) != 0 {
			return false, err
		}
		if err := json.Unmarshal(selected["resources"], &after); err != nil && len(selected["resources"]) != 0 {
			return false, err
		}
		after = slices.DeleteFunc(after, func(ref platform.AssetRef) bool {
			observation := ref.Kind == platform.AssetObservation || ref.App == "build" && (ref.Name == "build.observation" || strings.HasPrefix(ref.Name, "build.observation."))
			return (ref.Kind == platform.AssetProtocol || ref.Kind == platform.AssetInterface || observation) && !slices.Contains(before, ref)
		})
		if len(after) == 0 && len(original["resources"]) == 0 {
			delete(selected, "resources")
		} else if len(selected["resources"]) != 0 {
			var err error
			selected["resources"], err = json.Marshal(after)
			if err != nil {
				return false, err
			}
		}
	}
	before, err := platform.CanonicalDigest(original)
	if err != nil {
		return false, err
	}
	after, err := platform.CanonicalDigest(selected)
	return err == nil && before == after, err
}

func UniqueRefs(refs []platform.AssetRef) []platform.AssetRef {
	out := []platform.AssetRef{}
	for _, ref := range refs {
		if !slices.Contains(out, ref) {
			out = append(out, ref)
		}
	}
	slices.SortFunc(out, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return out
}

func RetainedDefinitionVersions[T any](history map[string]T, previousVersion string, previous *T, version string, current T) (map[string]T, bool) {
	versions := maps.Clone(history)
	if versions == nil {
		versions = map[string]T{}
	}
	if previous != nil {
		versions[previousVersion] = *previous
	}
	if prior, ok := versions[version]; ok && string(platform.Raw(prior)) != string(platform.Raw(current)) {
		return nil, false
	}
	return versions, true
}

func CheckApplicationExtension(ref platform.AssetRef, a platform.Application, definitions []platform.Definition) error {
	if err := a.CheckExtension(); err != nil {
		return err
	}
	if a.Extends == nil {
		return nil
	}
	if *a.Extends == ref {
		return fmt.Errorf("an application cannot extend itself")
	}
	if !slices.ContainsFunc(definitions, func(definition platform.Definition) bool {
		return definition.Ref == *a.Extends && definition.Source == "code" && definition.Application != nil
	}) {
		return fmt.Errorf("application %s can only extend an installed code application", ref)
	}
	entry := a.Entry
	if entry == "" {
		entry = a.Extends.App
	}
	for _, other := range definitions {
		if other.Ref == ref || other.Application == nil || other.Application.Extends == nil {
			continue
		}
		otherEntry := other.Application.Entry
		if otherEntry == "" {
			otherEntry = other.Application.Extends.App
		}
		if otherEntry != entry {
			continue
		}
		for _, replacement := range a.Replacements {
			if slices.ContainsFunc(other.Application.Replacements, func(previous platform.ApplicationPageReplacement) bool { return previous.View == replacement.View }) {
				return fmt.Errorf("application view %s/%s is already replaced by %s", entry, replacement.View, other.Ref)
			}
		}
	}
	return nil
}

func ObjectDefinitions(app string, info platform.EntityInfo, actions []platform.Action, pages []platform.Page, protocols ...platform.Provision) []platform.Definition {
	definitions := []platform.Definition{}
	object := platform.AssetRef{App: app, Kind: platform.AssetObject, Name: info.Type}
	entity := info
	definitions = append(definitions, platform.Definition{Ref: object, Source: "tenant", Version: "1", ContractVersion: 1, Entity: &entity})
	for _, action := range actions {
		a := action
		definitions = append(definitions, platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetAction, Name: a.Schema}, Source: "tenant", Version: "1",
			ContractVersion: 1, Action: &a})
	}
	for _, page := range pages {
		p := page
		definitions = append(definitions, platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetPage, Name: p.Name}, Source: "tenant", Version: "1",
			ContractVersion: 1, Page: &p})
	}
	for _, provision := range protocols {
		protocol := platform.ProtocolReleaseDescriptor{Name: provision.Protocol.Name, Version: provision.Protocol.Version, Actions: slices.Clone(provision.Protocol.Actions), Reads: slices.Clone(provision.Protocol.Reads), ActionBindings: map[string]platform.AssetRef{}, ActionRoles: map[string][]string{}, ReadBindings: map[string]platform.AssetRef{}}
		for _, action := range protocol.Actions {
			mapped := provision.Actions[action.Schema]
			protocol.ActionBindings[action.Schema] = platform.AssetRef{App: app, Kind: platform.AssetAction, Name: mapped}
			for _, actual := range actions {
				if actual.Schema == mapped {
					protocol.ActionRoles[action.Schema] = slices.Clone(actual.Roles)
				}
			}
		}
		for _, read := range protocol.Reads {
			protocol.ReadBindings[read] = object
		}
		definitions = append(definitions, platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetProtocol, Name: protocol.Name}, Source: "tenant", Version: "1", ContractVersion: protocol.Version, Protocol: &protocol})
	}
	return definitions
}
