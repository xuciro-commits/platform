package records

import (
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"reflect"
)

func PrepareMediaVersionImages(store *Store, installations []platform.ReleaseInstallation) ([]platform.MediaSetVersion, error) {
	var out []platform.MediaSetVersion
	for _, installation := range installations {
		if installation.Ref == nil || installation.Ref.Kind != platform.AssetMediaSet {
			continue
		}
		var set platform.MediaSet
		if err := json.Unmarshal(installation.Image, &set); err != nil {
			return nil, err
		}
		for _, snapshot := range platform.RetainedMediaVersions(set) {
			snapshot.ID = platform.MediaSetVersionID(set.ID, snapshot.Version)
			snapshot.MediaSet = set.ID
			if entity := store.Type(platform.MediaSetVersionType); entity != nil {
				if existing := entity.Rows().Get(snapshot.ID); existing != nil {
					prior := existing.Value.Interface().(platform.MediaSetVersion)
					if !reflect.DeepEqual(prior.Items, snapshot.Items) {
						return nil, fmt.Errorf("Media set version already denotes different items")
					}
					snapshot = prior
				}
			}
			out = append(out, snapshot)
		}
	}
	return out, nil
}

func ApplyMediaVersionImages(store *Store, snapshots []platform.MediaSetVersion) error {
	if len(snapshots) == 0 {
		return nil
	}
	versions := store.Type(platform.MediaSetVersionType)
	for _, snapshot := range snapshots {
		if existing := versions.Rows().Get(snapshot.ID); existing != nil {
			prior := existing.Value.Interface().(platform.MediaSetVersion)
			if !reflect.DeepEqual(prior, snapshot) {
				return fmt.Errorf("Accepted media version image differs from retained history")
			}
			continue
		}
		row := &Row{Value: reflect.ValueOf(&snapshot).Elem()}
		row.RetainOriginal(platform.Raw(snapshot))
		versions.SetRow(snapshot.ID, row)
		store.TrackWrite(platform.MediaSetVersionType + "/" + snapshot.ID)
	}
	return nil
}
