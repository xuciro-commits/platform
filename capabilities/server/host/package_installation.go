package host

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
)

func (h hostView) PrepareManagedPackage(c platform.Caller, descriptor platform.PackageDescriptor, archive bool) (func(*pb.ChangeRecord), *kernel.Error) {
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
	candidate, err := packages.BlueprintCandidate(*descriptor.Blueprint, owner, code)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	refs := map[platform.AssetRef]bool{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == build.ID {
			refs[asset.Ref] = true
		}
	}
	for _, resource := range resources {
		if refs[resource.Ref] && resource.ManagedBy != descriptor.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package cannot replace resource {resource} in another project", resource.Ref.String())
		}
	}
	context := platform.AssetReleaseContext{Member: c.Member, At: d.at, ManagedBy: descriptor.ID, Upgrade: true}
	_, _, err = h.t.bootstrapOwnerDraftsLocked(candidate, context, false, nil, d)
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
	plans, err := registry.PrepareReleaseAssets(candidate.Assets, context)
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
		installed := map[platform.AssetRef]bool{}
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
			installed[ref] = true
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
			if resource.ManagedBy == descriptor.ID && resource.Ref != projectRef && !installed[resource.Ref] {
				archiveResource(r, resource)
			}
		}
		for _, version := range mediaVersions {
			caller.Put(r, version)
		}
	}, nil
}

func (t *Tenant) prepareBatchDefinitions(rows *records.Store, installations []platform.ReleaseInstallation) (*Tenant, error) {
	view, err := t.assetInstallationView(rows)
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
