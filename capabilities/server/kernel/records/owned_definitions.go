package records

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

type DefinitionOwners struct {
	Owners          []platform.AssetLifecycleOwner
	Active          func(app string) bool
	Publishes       func(app string) bool
	Reader          func(app string) (platform.AssetDefinitionReader, bool)
	IncludeArchived bool
}

func (o DefinitionOwners) Metadata(ref platform.AssetRef) bool {
	owner, ok := o.Reader(ref.App)
	return ok && owner.DefinitionMetadataKind(ref.Kind)
}

func (o DefinitionOwners) Definition(plan platform.AssetInstallation) (platform.Definition, bool, error) {
	if !o.Metadata(plan.Ref) {
		return platform.Definition{}, false, nil
	}
	var record struct {
		Published string `json:"published"`
	}
	if err := json.Unmarshal(plan.Image, &record); err != nil || record.Published == "" {
		return platform.Definition{}, false, fmt.Errorf("asset %s has no accepted definition", plan.Ref)
	}
	var asset platform.ReleaseAsset
	if err := json.Unmarshal([]byte(record.Published), &asset); err != nil || asset.Ref != plan.Ref || asset.SourceVersion == "" || asset.ContractVersion < 1 {
		return platform.Definition{}, false, fmt.Errorf("asset %s has an invalid accepted definition", plan.Ref)
	}
	owner, _ := o.Reader(plan.Ref.App)
	definition, err := owner.DefinitionDescriptor(asset)
	if err != nil {
		return definition, false, err
	}
	definition.OwnerRecord = plan.Record
	return definition, true, nil
}

func (s *Store) agentRecord(ref platform.AssetRef) string {
	s.Lock()
	defer s.Unlock()
	if view := s.Type(ref.App + ".agent"); view != nil {
		for id, row := range view.Rows().All() {
			if BusinessField(view.Info(), row.Value, "name") == ref.Name && !Header(row.Value).Archived {
				return view.Info().Type + "/" + id
			}
		}
	}
	return ""
}

type publishedImage struct {
	app, schema, record string
	raw                 json.RawMessage
}

func (s *Store) publishedImages(o DefinitionOwners, datasets map[platform.AssetRef]platform.ReleaseAsset) ([]publishedImage, []platform.Definition, error) {
	s.Lock()
	defer s.Unlock()
	var images []publishedImage
	var definitions []platform.Definition
	for typ, et := range s.Types() {
		published, hasPublished := et.Info().Go.FieldByName("Published")
		version, hasVersion := et.Info().Go.FieldByName("DefinitionVersion")
		if !hasPublished || !hasVersion || published.Type.Kind() != reflect.String || version.Type.Kind() != reflect.Int || !o.Publishes(et.Info().App) {
			continue
		}
		for id, row := range et.Rows().All() {
			if Header(row.Value).Archived && !o.IncludeArchived {
				continue
			}
			ordinal := row.Value.FieldByIndex(version.Index).Int()
			raw := row.Value.FieldByIndex(published.Index).String()
			if ordinal == 0 && raw == "" {
				if typ == et.Info().App+".dataset" {
					ref := platform.AssetRef{App: et.Info().App, Kind: platform.AssetDataset, Name: BusinessField(et.Info(), row.Value, "name")}
					if asset, exists := datasets[ref]; exists {
						definitions = append(definitions, platform.Definition{Ref: asset.Ref, Source: "builder", Version: asset.SourceVersion, ContractVersion: asset.ContractVersion, Requires: slices.Clone(asset.Requires), Provides: slices.Clone(asset.Provides), OwnerRecord: typ + "/" + id})
					}
				}
				continue
			}
			if ordinal < 1 || raw == "" {
				return nil, nil, fmt.Errorf("owned metadata %s/%s has no valid publication", typ, id)
			}
			body, err := row.Image()
			if err != nil {
				return nil, nil, err
			}
			images = append(images, publishedImage{et.Info().App, typ + ".publish", typ + "/" + id, slices.Clone(body)})
		}
	}
	return images, definitions, nil
}

func (s *Store) OwnedDefinitions(base []platform.Definition, o DefinitionOwners) ([]platform.Definition, error) {
	base = slices.DeleteFunc(slices.Clone(base), func(definition platform.Definition) bool {
		return !o.Active(definition.Ref.App) || definition.Ref.Kind == platform.AssetAgent && definition.Source != "code"
	})
	datasets := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, provider := range o.Owners {
		assets, err := provider.ReleaseAssets()
		if err != nil {
			return nil, err
		}
		for _, asset := range assets {
			if asset.Ref.Kind == platform.AssetDataset {
				datasets[asset.Ref] = asset
			}
			if asset.Ref.Kind != platform.AssetAgent {
				for i := range base {
					if base[i].Ref != asset.Ref || base[i].Source == "code" {
						continue
					}
					base[i].Requires, base[i].Provides, base[i].Version = slices.Clone(asset.Requires), slices.Clone(asset.Provides), asset.SourceVersion
					if base[i].Provides == nil {
						descriptor, err := platform.DescribeAsset(asset)
						if err != nil {
							return nil, err
						}
						base[i].Provides = descriptor.Provides
					}
				}
				continue
			}
			reader, ok := provider.(platform.AssetDefinitionReader)
			if !ok {
				return nil, fmt.Errorf("agent owner has no definition descriptor")
			}
			definition, err := reader.DefinitionDescriptor(asset)
			if err != nil {
				return nil, err
			}
			definition.OwnerRecord = s.agentRecord(asset.Ref)
			base = append(base, definition)
		}
	}
	images, datasetDefinitions, err := s.publishedImages(o, datasets)
	if err != nil {
		return nil, err
	}
	out := slices.DeleteFunc(slices.Clone(base), func(def platform.Definition) bool { return def.Source != "code" && o.Metadata(def.Ref) })
	out = append(out, datasetDefinitions...)
	sorted := func() []platform.Definition {
		slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
		return out
	}
	if len(images) == 0 {
		return sorted(), nil
	}
	registry, err := platform.NewAssetRegistry(o.Owners...)
	if err != nil {
		return nil, err
	}
	seen := map[platform.AssetRef]bool{}
	for _, image := range images {
		plan, err := registry.DecodeAssetInstallation(image.app, image.schema, image.raw)
		if err != nil {
			return nil, fmt.Errorf("owned metadata %s: %w", image.record, err)
		}
		if plan.Record != image.record {
			return nil, fmt.Errorf("owned publication metadata identity differs")
		}
		def, indexed, err := o.Definition(plan)
		if err != nil {
			return nil, err
		}
		if !indexed || seen[def.Ref] {
			return nil, fmt.Errorf("owned metadata %s has an unsupported or duplicate asset", image.record)
		}
		seen[def.Ref] = true
		out = append(out, def)
	}
	return sorted(), nil
}
