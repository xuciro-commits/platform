package packages

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

type ManagedRows struct {
	Archived func(resource build.Resource, archived bool) any
	Decode   func(typ string, image json.RawMessage) (any, error)
	Image    func(record string) json.RawMessage
	Install  func(platform.ReleaseInstallation)
	Fail     func(*kernel.Error)
}

type Managed struct {
	Caller     platform.Caller
	Descriptor platform.PackageDescriptor
	Resources  []build.Resource
	Rows       ManagedRows
}

func (m Managed) Project() (id, name string) {
	hash := sha256.Sum256([]byte(m.Descriptor.ID))
	return "package:" + m.Descriptor.ID, fmt.Sprintf("pkg%x", hash[:12])
}

func (m Managed) archive(r *pb.ChangeRecord, resource build.Resource, archived bool) {
	resource.State, resource.Archived = "active", archived
	if archived {
		resource.State = "archived"
	}
	m.Caller.Put(r, resource)
	m.Caller.Put(r, m.Rows.Archived(resource, archived))
}

func (m Managed) Archive(r *pb.ChangeRecord) {
	for _, resource := range m.Resources {
		if resource.ManagedBy == m.Descriptor.ID {
			m.archive(r, resource, true)
		}
	}
	projectID, _ := m.Project()
	if project, exists := platform.Get[build.Application](m.Caller, projectID); exists {
		project.Archived = true
		m.Caller.Put(r, project)
	}
}

func (m Managed) Owned(code, owned []platform.ReleaseAsset) (map[platform.AssetRef]bool, *kernel.Error) {
	native := map[platform.AssetRef]bool{}
	for _, asset := range code {
		native[asset.Ref] = true
	}
	refs := map[platform.AssetRef]bool{}
	for _, asset := range owned {
		if !native[asset.Ref] {
			refs[asset.Ref] = true
		}
	}
	for _, resource := range m.Resources {
		if refs[resource.Ref] && resource.ManagedBy != m.Descriptor.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package cannot replace resource {resource} in another project", resource.Ref.String())
		}
	}
	return refs, nil
}

func (m Managed) Install(r *pb.ChangeRecord, refs map[platform.AssetRef]bool, plans []platform.AssetInstallation, media []platform.MediaSetVersion) {
	tenant := m.Caller.Member.Tenant
	projectID, projectName := m.Project()
	project, _ := platform.Get[build.Application](m.Caller, projectID)
	project.ID, project.Name, project.Title, project.State, project.Archived = projectID, projectName, m.Descriptor.Title, "published", false
	m.Caller.Put(r, project)
	projectRef := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: projectName}
	m.Caller.Put(r, build.Resource{Record: platform.Record{ID: platform.ResourceID(tenant, projectRef)}, Ref: projectRef, Type: build.AppType, Draft: projectID, Project: projectID, Title: m.Descriptor.Title, State: "active", Version: 1, ManagedBy: m.Descriptor.ID})
	for _, plan := range plans {
		typ, id, _ := strings.Cut(plan.Record, "/")
		value, err := m.Rows.Decode(typ, plan.Image)
		if err != nil {
			m.Rows.Fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error()))
			return
		}
		m.Caller.Put(r, value)
		image := m.Rows.Image(plan.Record)
		ref := plan.Ref
		m.Rows.Install(platform.ReleaseInstallation{ReleasePublication: platform.ReleasePublication{Schema: plan.Schema, Image: image}, Ref: &ref, Record: plan.Record})
		var title struct{ Title string }
		_ = json.Unmarshal(image, &title)
		if title.Title == "" {
			title.Title = ref.Name
		}
		resource, _ := platform.Get[build.Resource](m.Caller, platform.ResourceID(tenant, ref))
		resource.ID, resource.Ref, resource.Type, resource.Draft = platform.ResourceID(tenant, ref), ref, typ, id
		resource.Project, resource.ManagedBy, resource.State, resource.Title, resource.Archived = projectID, m.Descriptor.ID, "active", title.Title, false
		resource.Version++
		m.Caller.Put(r, resource)
	}
	for _, resource := range m.Resources {
		if resource.ManagedBy == m.Descriptor.ID && resource.Ref != projectRef && !refs[resource.Ref] {
			m.archive(r, resource, true)
		}
	}
	for _, resource := range m.Resources {
		if refs[resource.Ref] && resource.Archived {
			m.archive(r, resource, false)
		}
	}
	for _, version := range media {
		m.Caller.Put(r, version)
	}
}
