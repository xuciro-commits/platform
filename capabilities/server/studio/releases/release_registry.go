package releases

import (
	"encoding/json"

	"platformserver/platform"

	"slices"
	"strings"
)

// A retained candidate predates dependency metadata only when Provides is
// absent. Its original code contract still has to match this exact owner and
// source version; the release is never rewritten with today's metadata.
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
	// Only metadata newly added by the owner may be absent in old bytes.
	// A binding already present in the retained body must match exactly.
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
		// Protocols and interfaces now have their own descriptor. Their new
		// code resources, like the observation authoring records added later,
		// add no execution semantics to the old application.
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
