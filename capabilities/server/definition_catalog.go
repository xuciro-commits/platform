package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/internal/host"
	"platformserver/internal/records"
	"platformserver/platform"
)

func (t *Tenant) ownedMetadataKind(ref platform.AssetRef) bool {
	owner, ok := t.app(ref.App).(host.AssetDefinitionReader)
	return ok && owner.DefinitionMetadataKind(ref.Kind)
}

// ownedAssetDefinition indexes only an owner's accepted descriptor identity.
// The original image remains the authority; no descriptor body is copied into
// discovery, and its metadata record is retained solely for read filtering.
func (t *Tenant) ownedAssetDefinition(plan platform.AssetInstallation) (platform.Definition, bool, error) {
	if !t.ownedMetadataKind(plan.Ref) {
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
	owner := t.app(plan.Ref.App).(host.AssetDefinitionReader)
	definition, err := owner.DefinitionDescriptor(asset)
	if err != nil {
		return definition, false, err
	}
	definition.OwnerRecord = plan.Record
	return definition, true, nil
}

// ownedAssetDefinitions is a temporary read projection over stored owner
// publication images. Direct publication and recovery use the same index;
// reading it never installs definitions or changes the live registry.
func (t *Tenant) ownedAssetDefinitions(base []platform.Definition) ([]platform.Definition, error) {
	base = slices.Clone(base)
	base = slices.DeleteFunc(base, func(definition platform.Definition) bool {
		return definition.Ref.Kind == platform.AssetAgent && definition.Source != "code"
	})
	datasets := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, owner := range t.apps {
		provider, ok := owner.(platform.AssetLifecycleOwner)
		if !ok {
			continue
		}
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
			reader, ok := owner.(host.AssetDefinitionReader)
			if !ok {
				return nil, fmt.Errorf("agent owner has no definition descriptor")
			}
			definition, err := reader.DefinitionDescriptor(asset)
			if err != nil {
				return nil, err
			}
			ownerRecord := ""
			func() {
				t.records.mu.Lock()
				defer t.records.mu.Unlock()
				if recordView := t.records.types[asset.Ref.App+".agent"]; recordView != nil {
					for id, row := range recordView.rows.All() {
						if businessField(recordView.info, row.Value, "name") == asset.Ref.Name && !records.Header(row.Value).Archived {
							ownerRecord = recordView.info.Type + "/" + id
							break
						}
					}
				}
			}()
			definition.OwnerRecord = ownerRecord
			base = append(base, definition)
		}
	}
	type image struct {
		app, schema, record string
		raw                 json.RawMessage
	}
	var images []image
	var datasetDefinitions []platform.Definition
	if err := func() error {
		t.records.mu.Lock()
		defer t.records.mu.Unlock()
		for typ, et := range t.records.types {
			published, hasPublished := et.info.Go.FieldByName("Published")
			version, hasVersion := et.info.Go.FieldByName("DefinitionVersion")
			if !hasPublished || !hasVersion || published.Type.Kind() != reflect.String || version.Type.Kind() != reflect.Int {
				continue // other metadata retains its original installed index
			}
			if _, ok := t.app(et.info.App).(platform.AssetLifecycleOwner); !ok {
				continue
			}
			for id, row := range et.rows.All() {
				if records.Header(row.Value).Archived {
					continue
				}
				ordinal := row.Value.FieldByIndex(version.Index).Int()
				raw := row.Value.FieldByIndex(published.Index).String()
				if ordinal == 0 && raw == "" {
					// A dataset is usable from its original create/load path, without
					// a publish action. Project only the descriptor its owner already
					// offers; runtime rows and other unsealed drafts stay out.
					if typ == et.info.App+".dataset" {
						ref := platform.AssetRef{App: et.info.App, Kind: platform.AssetDataset, Name: businessField(et.info, row.Value, "name")}
						if asset, exists := datasets[ref]; exists {
							datasetDefinitions = append(datasetDefinitions, platform.Definition{Ref: asset.Ref, Source: "builder", Version: asset.SourceVersion, ContractVersion: asset.ContractVersion, Requires: slices.Clone(asset.Requires), Provides: slices.Clone(asset.Provides), OwnerRecord: typ + "/" + id})
						}
					}
					continue // an unpublished legacy/development configuration
				}
				if ordinal < 1 || raw == "" {
					return fmt.Errorf("owned metadata %s/%s has no valid publication", typ, id)
				}
				body, err := row.Image()
				if err != nil {
					return err
				}
				images = append(images, image{et.info.App, typ + ".publish", typ + "/" + id, slices.Clone(body)})
			}
		}
		return nil
	}(); err != nil {
		return nil, err
	}
	out := slices.DeleteFunc(slices.Clone(base), func(def platform.Definition) bool { return def.Source != "code" && t.ownedMetadataKind(def.Ref) })
	out = append(out, datasetDefinitions...)
	if len(images) == 0 {
		slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
		return out, nil
	}
	registry, err := t.assetRegistryLocked()
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
		def, indexed, err := t.ownedAssetDefinition(plan)
		if err != nil {
			return nil, err
		}
		if !indexed || seen[def.Ref] {
			return nil, fmt.Errorf("owned metadata %s has an unsupported or duplicate asset", image.record)
		}
		seen[def.Ref] = true
		out = append(out, def)
	}
	slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return out, nil
}
