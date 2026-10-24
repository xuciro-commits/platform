package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"

	"platformserver/internal/construction"
	"platformserver/platform"
)

type ReleaseExtensionReview struct {
	Type          string                `json:"type"`
	Base          platform.AssetBinding `json:"base"`
	TargetVersion string                `json:"targetVersion"`
	Fields        []string              `json:"fields"`
	Issue         string                `json:"issue,omitempty"`
}

// All installed extensions participate in upgrade admission, including those
// outside the selected application closure. Nothing republishes or replaces an
// extension merely because its base package changed.
func (t *Tenant) checkExtensionSet() error {
	for _, et := range t.records.sortedTypes() {
		if et.info.Extends == "" {
			continue
		}
		base, exists := t.entity(et.info.Extends)
		if !exists {
			return fmt.Errorf("extension %s base %s is unavailable", et.info.Type, et.info.Extends)
		}
		if err := platform.CheckExtension(et.info, base); err != nil {
			return err
		}
	}
	return nil
}

// releaseAssetShapes are the object shapes a candidate installs, by type.
func (t *Tenant) releaseAssetShapes(assets []platform.ReleaseAsset) (map[string]platform.EntityInfo, error) {
	shapes := map[string]platform.EntityInfo{}
	for _, asset := range assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object construction.Object
		if err := json.Unmarshal(asset.Body, &object); err != nil {
			return nil, err
		}
		entity := construction.CompileObjectShape(object)
		info, err := platform.Describe(construction.ID, entity, func(reflect.Type) string { return "" })
		if err != nil {
			return nil, err
		}
		shapes[entity.Type] = info
	}
	return shapes, nil
}

func (t *Tenant) releaseExtensionReview(candidate platform.ReleaseCandidate) ([]ReleaseExtensionReview, error) {
	shapes, err := t.releaseAssetShapes(candidate.Assets)
	if err != nil {
		return nil, err
	}
	versions := map[string]string{}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetObject {
			versions[asset.Ref.Name] = asset.SourceVersion
		}
	}
	var result []ReleaseExtensionReview
	for _, et := range t.records.sortedTypes() {
		extension := et.info
		if next, exists := shapes[extension.Type]; exists {
			extension = next
		}
		if extension.Extends == "" {
			continue
		}
		base, exists := shapes[extension.Extends]
		if !exists {
			base, exists = t.entity(extension.Extends)
		}
		row := ReleaseExtensionReview{Type: extension.Type, Fields: []string{}, TargetVersion: versions[extension.Extends]}
		if extension.ExtensionSource != nil {
			row.Base = extension.ExtensionSource.Base
		}
		if row.Base.Ref.Name == "" {
			row.Base.Ref = platform.AssetRef{App: base.App, Kind: platform.AssetObject, Name: extension.Extends}
		}
		if row.TargetVersion == "" {
			if binding, ok := (hostView{t: t}).EntitySource(extension.Extends); ok {
				row.TargetVersion = binding.SourceVersion
			}
		}
		for _, field := range extension.Fields {
			if field.Name != "base" {
				row.Fields = append(row.Fields, field.Name)
			}
		}
		if !exists {
			row.Issue = "extension base is unavailable"
		} else if err := platform.CheckExtension(extension, base); err != nil {
			row.Issue = err.Error()
		}
		result = append(result, row)
	}
	return result, nil
}
