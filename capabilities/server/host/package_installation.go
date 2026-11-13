package host

import (
	"fmt"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
)

func (h hostView) PrepareManagedPackage(c platform.Caller, descriptor platform.PackageDescriptor, available []platform.ReleaseAsset, archive, upgrade, downgrade bool) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.Replaying || descriptor.Blueprint == nil && !archive {
		return nil, nil
	}
	d := h.decision
	managed := packages.NewManaged(platform.NewCaller(d, c.Member, build.ID, true, false), descriptor, func() *records.Store { return d.Records }, func(installation platform.ReleaseInstallation) {
		d.Installations = append(d.Installations, installation)
	}, d.FailDecision)
	return packages.ManagedPreparation{
		Managed: managed, Archive: archive, Available: available, At: d.At, Upgrade: upgrade, Downgrade: downgrade,
		Owner: h.t.builders[build.ID], Code: h.CodeReleaseAssets,
		Impact: func(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
			return h.t.packageLifecycleImpact(c.Member, id, after)
		},
		Bootstrap: func(candidate platform.ReleaseCandidate, context platform.AssetReleaseContext) error {
			_, _, err := h.t.bootstrapOwnerDraftsLockedWithArchived(candidate, context, false, nil, true, d)
			return err
		},
		Registry:        func() (*platform.AssetRegistry, error) { return d.tenant.assetRegistryLocked() },
		Projection:      h.t.Projection,
		CheckExtensions: func() error { return d.tenant.checkExtensionSet() },
		CheckStorage:    func(upgrade, downgrade bool) error { return h.t.validateAssetStorage(d.tenant, upgrade, downgrade) },
	}.Prepare()
}

func (t *Tenant) prepareBatchDefinitions(rows *records.Store, installations []platform.ReleaseInstallation) (*Tenant, error) {
	definitionRows := rows.ForkRecords()
	packages.RestoreReferencedResources(definitionRows, installations)
	view, err := t.assetInstallationView(definitionRows)
	if err != nil {
		return nil, err
	}
	registry, err := view.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	view.installingSet = true
	err = releaseowner.StageInstallations(registry, installations, platform.AssetApplyContext{Private: true},
		func(plan platform.AssetInstallation, _ platform.ReleaseInstallation) error {
			typ, id, _ := strings.Cut(plan.Record, "/")
			if et := rows.Type(typ); et == nil || et.Rows().Get(id) == nil {
				return fmt.Errorf("installed resource %s has no accepted image", plan.Record)
			}
			return nil
		}, view.registerInstalledAsset)
	if err != nil {
		return nil, err
	}
	view.installingSet = false
	if err := view.Projection.Anchor(t.Projection); err != nil {
		return nil, err
	}
	return view, nil
}
