package host

import (
	"encoding/json"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) enterpriseHistoricalReferencePaths(store *records.Store) map[string][][]string {
	var bodies []records.RetainedBody
	for _, raw := range t.releases.Candidates() {
		var release struct {
			Assets []platform.ReleaseAsset `json:"assets"`
		}
		if json.Unmarshal(raw, &release) != nil {
			continue
		}
		for _, asset := range release.Assets {
			if asset.Ref.Kind == platform.AssetObject {
				bodies = append(bodies, records.RetainedBody{Type: asset.Ref.Name, Raw: asset.Body})
			}
		}
	}
	return records.EnterpriseHistoricalPaths(store, build.ObjectType, build.ID+".", build.TypeOf, bodies)
}
