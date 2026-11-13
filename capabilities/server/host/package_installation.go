package host

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
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
	resources := []build.Resource{}
	if directory := d.Records.Type(build.ResourceType); directory != nil {
		for _, row := range directory.Rows().All() {
			resources = append(resources, row.Value.Interface().(build.Resource))
		}
	}
	managed := packages.Managed{Caller: platform.NewCaller(d, c.Member, build.ID, true, false), Descriptor: descriptor, Resources: resources, Rows: packages.ManagedRows{
		Archived: func(resource build.Resource, archived bool) any {
			et := d.Records.Type(resource.Type)
			value := records.Copy(et.Info().Go, et.Rows().Get(resource.Draft).Value.Interface())
			value.FieldByName("Archived").SetBool(archived)
			return value.Interface()
		},
		Decode: func(typ string, image json.RawMessage) (any, error) {
			value := reflect.New(d.Records.Type(typ).Info().Go)
			err := json.Unmarshal(image, value.Interface())
			return value.Elem().Interface(), err
		},
		Image: func(record string) json.RawMessage {
			typ, id, _ := strings.Cut(record, "/")
			image, _ := json.Marshal(d.Records.Type(typ).Rows().Get(id).Value.Interface())
			return image
		},
		Install: func(installation platform.ReleaseInstallation) {
			d.Installations = append(d.Installations, installation)
		},
		Fail: func(refusal *kernel.Error) { d.FailDecision(refusal) },
	}}
	if archive {
		return managed.Archive, nil
	}
	owner := h.t.App(build.ID).(build.ConstructionDrafts)
	code, err := h.CodeReleaseAssets()
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	candidate, err := packages.BlueprintCandidate(*descriptor.Blueprint, owner, available)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	owned, err := owner.CompleteBlueprintAssets(descriptor.Blueprint.Assets)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	refs, refusal := managed.Owned(code, owned)
	if refusal != nil {
		return nil, refusal
	}
	impact, err := h.t.packageLifecycleImpact(c.Member, descriptor.ID, owned)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Package impact could not be inspected: {reason}", err.Error())
	}
	if len(impact.References) > 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Remove references to {resource} from {consumer} before switching the product version", impact.References[0].Resource.String(), impact.References[0].Consumer.String())
	}
	projectID, _ := managed.Project()
	context := platform.AssetReleaseContext{Member: c.Member, At: d.At, ManagedBy: descriptor.ID, ManagedAssets: refs, Project: projectID, Upgrade: upgrade}
	_, _, err = h.t.bootstrapOwnerDraftsLockedWithArchived(candidate, context, false, nil, true, d)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package cannot be installed: {reason}", err.Error())
	}
	registry, err := d.tenant.assetRegistryLocked()
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	end, err := registry.BeginBootstrap(candidate.Assets)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	defer end()
	owned = slices.DeleteFunc(slices.Clone(candidate.Assets), func(asset platform.ReleaseAsset) bool { return !refs[asset.Ref] })
	plans, err := registry.PrepareReleaseAssets(owned, context)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	if err := d.tenant.checkExtensionSet(); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package version breaks an extension: {reason}", err.Error())
	}
	if err := h.t.validateAssetStorage(d.tenant, upgrade, downgrade); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package version cannot read the current data: {reason}", err.Error())
	}
	media := []platform.ReleaseInstallation{}
	for _, plan := range plans {
		ref := plan.Ref
		media = append(media, platform.ReleaseInstallation{ReleasePublication: platform.ReleasePublication{Schema: plan.Schema, Image: plan.Image}, Ref: &ref, Record: plan.Record})
	}
	mediaVersions, err := records.PrepareMediaVersionImages(h.t.Projection, media)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	return func(r *pb.ChangeRecord) { managed.Install(r, refs, plans, mediaVersions) }, nil
}

func (t *Tenant) prepareBatchDefinitions(rows *records.Store, installations []platform.ReleaseInstallation) (*Tenant, error) {
	definitionRows := rows.ForkRecords()
	restoreReferencedDefinitions(definitionRows, installations)
	view, err := t.assetInstallationView(definitionRows)
	if err != nil {
		return nil, err
	}
	registry, err := view.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	view.installingSet = true
	for _, installation := range installations {
		plan, err := releaseowner.InstallationPlan(registry, installation)
		if err != nil {
			return nil, err
		}
		typ, id, _ := strings.Cut(plan.Record, "/")
		et := rows.Type(typ)
		if et == nil || et.Rows().Get(id) == nil {
			return nil, fmt.Errorf("installed resource %s has no accepted image", plan.Record)
		}
		if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{Private: true}); err != nil {
			return nil, err
		}
		if err := view.registerInstalledAsset(plan); err != nil {
			return nil, err
		}
	}
	view.installingSet = false
	if err := view.Projection.Anchor(t.Projection); err != nil {
		return nil, err
	}
	return view, nil
}

func restoreReferencedDefinitions(rows *records.Store, installations []platform.ReleaseInstallation) {
	referenced := map[platform.AssetRef]bool{}
	for _, installation := range installations {
		if installation.Ref != nil {
			referenced[*installation.Ref] = true
		}
	}
	if len(referenced) == 0 {
		return
	}
	resources := rows.Type(build.ResourceType)
	if resources == nil {
		return
	}
	for _, row := range resources.Rows().All() {
		resource, ok := row.Value.Interface().(build.Resource)
		if !ok || !referenced[resource.Ref] {
			continue
		}
		et := rows.Type(resource.Type)
		if et == nil {
			continue
		}
		current := et.Rows().Get(resource.Draft)
		if current == nil || !records.Header(current.Value).Archived {
			continue
		}
		value := reflect.New(current.Value.Type()).Elem()
		value.Set(current.Value)
		records.Header(value).Archived = false
		copy := *current
		copy.Value = value
		et.SetRow(resource.Draft, &copy)
	}
}
