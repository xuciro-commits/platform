package host

import (
	"crypto/sha256"
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

func (h hostView) PrepareManagedPackage(c platform.Caller, descriptor platform.PackageDescriptor, available []platform.ReleaseAsset, archive bool) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.Replaying || descriptor.Blueprint == nil && !archive {
		return nil, nil
	}
	d := h.decision
	projectID := "package:" + descriptor.ID
	projectHash := sha256.Sum256([]byte(descriptor.ID))
	projectName := fmt.Sprintf("pkg%x", projectHash[:12])
	caller := platform.NewCaller(d, c.Member, build.ID, true, false)
	resources := []build.Resource{}
	if directory := d.records.Type(build.ResourceType); directory != nil {
		for _, row := range directory.Rows().All() {
			resources = append(resources, row.Value.Interface().(build.Resource))
		}
	}
	archiveResource := func(r *pb.ChangeRecord, resource build.Resource) {
		resource.State, resource.Archived = "archived", true
		caller.Put(r, resource)
		et := d.records.Type(resource.Type)
		value := records.Copy(et.Info().Go, et.Rows().Get(resource.Draft).Value.Interface())
		value.FieldByName("Archived").SetBool(true)
		caller.Put(r, value.Interface())
	}
	restoreResource := func(r *pb.ChangeRecord, resource build.Resource) {
		resource.State, resource.Archived = "active", false
		caller.Put(r, resource)
		et := d.records.Type(resource.Type)
		value := records.Copy(et.Info().Go, et.Rows().Get(resource.Draft).Value.Interface())
		value.FieldByName("Archived").SetBool(false)
		caller.Put(r, value.Interface())
	}
	if archive {
		return func(r *pb.ChangeRecord) {
			for _, resource := range resources {
				if resource.ManagedBy == descriptor.ID {
					archiveResource(r, resource)
				}
			}
			if project, exists := platform.Get[build.Application](caller, projectID); exists {
				project.Archived = true
				caller.Put(r, project)
			}
		}, nil
	}
	owner := h.t.app(build.ID).(build.ConstructionDrafts)
	code, err := h.CodeReleaseAssets()
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	candidate, err := packages.BlueprintCandidate(*descriptor.Blueprint, owner, available)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	native := map[platform.AssetRef]bool{}
	for _, asset := range code {
		native[asset.Ref] = true
	}
	owned, err := owner.CompleteBlueprintAssets(descriptor.Blueprint.Assets)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	refs := map[platform.AssetRef]bool{}
	for _, asset := range owned {
		if !native[asset.Ref] {
			refs[asset.Ref] = true
		}
	}
	for _, resource := range resources {
		if refs[resource.Ref] && resource.ManagedBy != descriptor.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package cannot replace resource {resource} in another project", resource.Ref.String())
		}
	}
	removed := map[platform.AssetRef]bool{}
	for _, resource := range resources {
		if resource.ManagedBy == descriptor.ID && resource.Draft != projectID && !resource.Archived && !refs[resource.Ref] {
			removed[resource.Ref] = true
		}
	}
	views := h.t.app("platform").(interface{ PackageViews() []platform.PackageView }).PackageViews()
	for _, view := range views {
		if view.Descriptor.ID != descriptor.ID || view.Installed == nil || view.Descriptor.Version != view.Installed.Version || view.Descriptor.Blueprint == nil {
			continue
		}
		previous, err := owner.CompleteBlueprintAssets(view.Descriptor.Blueprint.Assets)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
		}
		for _, asset := range previous {
			if !refs[asset.Ref] && !native[asset.Ref] {
				removed[asset.Ref] = true
			}
		}
	}
	if impacts := h.t.packageRemovalImpacts(removed, refs); len(impacts) > 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Remove references to {resource} from {consumer} before upgrading the product", impacts[0].Resource.String(), impacts[0].Consumer.String())
	}
	context := platform.AssetReleaseContext{Member: c.Member, At: d.at, ManagedBy: descriptor.ID, ManagedAssets: refs, Project: projectID, Upgrade: true}
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
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package upgrade breaks an extension: {reason}", err.Error())
	}
	if err := h.t.validateAssetStorage(d.tenant, true); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package upgrade needs a data migration: {reason}", err.Error())
	}
	media := []platform.ReleaseInstallation{}
	for _, plan := range plans {
		ref := plan.Ref
		media = append(media, platform.ReleaseInstallation{ReleasePublication: platform.ReleasePublication{Schema: plan.Schema, Image: plan.Image}, Ref: &ref, Record: plan.Record})
	}
	mediaVersions, err := h.t.prepareMediaVersionImages(media)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	return func(r *pb.ChangeRecord) {
		project, _ := platform.Get[build.Application](caller, projectID)
		project.ID, project.Name, project.Title, project.State, project.Archived = projectID, projectName, descriptor.Title, "published", false
		caller.Put(r, project)
		projectRef := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: projectName}
		caller.Put(r, build.Resource{Record: platform.Record{ID: platform.ResourceID(c.Tenant, projectRef)}, Ref: projectRef, Type: build.AppType, Draft: projectID, Project: projectID, Title: descriptor.Title, State: "active", Version: 1, ManagedBy: descriptor.ID})
		for _, plan := range plans {
			typ, id, _ := strings.Cut(plan.Record, "/")
			et := d.records.Type(typ)
			value := reflect.New(et.Info().Go)
			if err := json.Unmarshal(plan.Image, value.Interface()); err != nil {
				d.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error()))
				return
			}
			caller.Put(r, value.Elem().Interface())
			row := d.records.Type(typ).Rows().Get(id)
			image, _ := json.Marshal(row.Value.Interface())
			ref := plan.Ref
			d.installations = append(d.installations, platform.ReleaseInstallation{ReleasePublication: platform.ReleasePublication{Schema: plan.Schema, Image: image}, Ref: &ref, Record: plan.Record})
			var title struct{ Title string }
			_ = json.Unmarshal(image, &title)
			if title.Title == "" {
				title.Title = ref.Name
			}
			resource, _ := platform.Get[build.Resource](caller, platform.ResourceID(c.Tenant, ref))
			resource.ID, resource.Ref, resource.Type, resource.Draft = platform.ResourceID(c.Tenant, ref), ref, typ, id
			resource.Project, resource.ManagedBy, resource.State, resource.Title, resource.Archived = projectID, descriptor.ID, "active", title.Title, false
			resource.Version++
			caller.Put(r, resource)
		}
		for _, resource := range resources {
			if resource.ManagedBy == descriptor.ID && resource.Ref != projectRef && !refs[resource.Ref] {
				archiveResource(r, resource)
			}
		}
		for _, resource := range resources {
			if refs[resource.Ref] && resource.Archived {
				restoreResource(r, resource)
			}
		}
		for _, version := range mediaVersions {
			caller.Put(r, version)
		}
	}, nil
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
	if err := view.records.Anchor(t.records); err != nil {
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
