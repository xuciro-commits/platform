package host

import (
	"platformkernel/kernel"
	"platformserver/services/devops/packages"
	"time"

	"platformserver/platform"
	dataowner "platformserver/services/data"
)

func (h hostView) PackageLifecycleImpact(c platform.Caller, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	return h.t.packageLifecycleImpact(c.Member, id, after)
}

func (t *Tenant) packageLifecycleImpact(m platform.Member, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	resources := resourceRows(t.Projection)
	ctx := t.applicationContext()
	return packages.Impact{Resources: resources,
		Records: func(typ string) (int, bool) {
			if objects := t.Projection.Type(typ); objects != nil {
				return objects.Rows().Len(), true
			}
			return 0, false
		},
		Connection: func(id string) (platform.PackageImpactConnection, bool, error) {
			record := t.Projection.Type(dataowner.ConnectionType).Rows().Get(id)
			if record == nil {
				return platform.PackageImpactConnection{}, false, nil
			}
			connection := record.Value.Interface().(dataowner.Connection)
			var definition dataowner.ConnectionDefinition
			if connection.Published != "" {
				var accepted platform.ReleaseAsset
				if err := dataowner.DecodeDescriptor([]byte(connection.Published), &accepted); err != nil {
					return platform.PackageImpactConnection{}, false, err
				}
				if err := dataowner.DecodeDescriptor(accepted.Body, &definition); err != nil {
					return platform.PackageImpactConnection{}, false, err
				}
			}
			return platform.PackageImpactConnection{Configured: connection.Address != "" && (!definition.Credential || connection.Secret != ""), Credential: definition.Credential}, true, nil
		},
		Removals: func(removed, replaced map[platform.AssetRef]bool) []platform.PackageImpactReference {
			return packages.RemovalImpacts(resources, t.definitions, t.OwnerActive, removed, replaced)
		},
		Extensions: t.releaseExtensionReview,
		Runs: func(ref platform.AssetRef, offset int) (platform.ApplicationRunPage, *kernel.Error) {
			return ctx.Runs(m, ref, offset, 100, time.Now())
		}}.Of(id, after)
}
