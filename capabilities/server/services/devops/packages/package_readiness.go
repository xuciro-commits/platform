package packages

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
)

// PackageReadiness describes only the presence of shipped reference rows,
// not their values or the readiness of optional demo data.
type PackageReadiness struct {
	Ready   bool `json:"ready"`
	Pending int  `json:"pending"`
}

func ReferenceReadiness(d PreparationOwner, id, version string, store *records.Store) (PackageReadiness, *platform.Error) {
	if d == nil {
		return PackageReadiness{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Package management is unavailable")
	}
	held := d.InstalledPackages()[id]
	if held == nil || held.Version != version || held.State != "active" {
		return PackageReadiness{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The exact product version is unavailable")
	}
	blueprint, _, err := d.PreparationBlueprint(id, version)
	if err != nil {
		return PackageReadiness{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	descriptor := platform.PackageDescriptor{Blueprint: &blueprint}
	if held.Space != "" && held.Space != Space(descriptor) {
		descriptor, err = Rebind(descriptor, held.Space)
		if err != nil {
			return PackageReadiness{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
		}
	}
	pending := 0
	for _, asset := range descriptor.Blueprint.Assets {
		if asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var definition struct {
			Records []struct {
				ID string `json:"id"`
			} `json:"records"`
		}
		if err := json.Unmarshal(asset.Body, &definition); err != nil {
			return PackageReadiness{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
		}
		if len(definition.Records) == 0 {
			continue
		}
		ids := make([]string, 0, len(definition.Records))
		for _, row := range definition.Records {
			ids = append(ids, row.ID)
		}
		typ := store.Type(asset.Ref.Name)
		if typ == nil {
			pending += len(ids)
			continue
		}
		rows := typ.Rows().GetBatch(ids)
		for _, id := range ids {
			if rows[id] == nil {
				pending++
			}
		}
	}
	return PackageReadiness{Ready: pending == 0, Pending: pending}, nil
}
