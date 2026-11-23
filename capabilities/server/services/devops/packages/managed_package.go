package packages

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/publication"
)

type ManagedRows struct {
	Archived func(resource protocol.Resource, archived bool) any
	Decode   func(typ string, image json.RawMessage) (any, error)
	Image    func(record string) json.RawMessage
	Exists   func(typ, id string) bool
	Install  func(platform.ReleaseInstallation)
	Fail     func(*platform.Error)
}

type Managed struct {
	Caller     platform.Caller
	Descriptor platform.PackageDescriptor
	Resources  []protocol.Resource
	Rows       ManagedRows
}

type ManagedPreparation struct {
	Managed
	Archive    bool
	Available  []platform.ReleaseAsset
	At         time.Time
	Upgrade    bool
	Downgrade  bool
	Owner      build.ConstructionDrafts
	Resolver   publication.Resolver
	Code       func() ([]platform.ReleaseAsset, error)
	Impact     func(string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
	Bootstrap  func(protocol.ReleaseCandidate, protocol.AssetReleaseContext) error
	Registry   func() (*publication.AssetRegistry, error)
	Projection *records.Store

	CheckExtensions func() error
	CheckStorage    func(upgrade, downgrade bool) error
}

func NewManaged(caller platform.Caller, descriptor platform.PackageDescriptor, rows func() *records.Store, install func(platform.ReleaseInstallation), fail func(*platform.Error)) Managed {
	return Managed{Caller: caller, Descriptor: descriptor, Resources: Resources(rows()), Rows: ManagedRows{
		Archived: func(resource protocol.Resource, archived bool) any {
			return rows().ArchivedCopy(resource.Type, resource.Draft, archived)
		},
		Decode: func(typ string, image json.RawMessage) (any, error) { return rows().DecodeRow(typ, image) },
		Image:  func(record string) json.RawMessage { return rows().RowImage(record) },
		Exists: func(typ, id string) bool {
			entity := rows().Type(typ)
			return entity != nil && entity.Rows().Get(id) != nil
		},
		Install: install,
		Fail:    fail,
	}}
}

func Resources(store *records.Store) []protocol.Resource {
	var resources []protocol.Resource
	if directory := store.Type(build.ResourceType); directory != nil {
		for _, row := range directory.Rows().All() {
			resources = append(resources, row.Value.Interface().(protocol.Resource))
		}
	}
	return resources
}

func RestoreReferencedResources(store *records.Store, installations []platform.ReleaseInstallation) {
	referenced := map[platform.AssetRef]bool{}
	for _, installation := range installations {
		if installation.Ref != nil {
			referenced[*installation.Ref] = true
		}
	}
	for _, resource := range Resources(store) {
		if !referenced[resource.Ref] {
			continue
		}
		et := store.Type(resource.Type)
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

func (p ManagedPreparation) Prepare() (func(*pb.ChangeRecord), *platform.Error) {
	if p.Descriptor.Blueprint == nil && !p.Archive {
		return nil, nil
	}
	if p.Archive {
		return p.Managed.Archive, nil
	}
	if !p.Upgrade && !p.Downgrade {
		for _, raw := range p.Descriptor.Blueprint.Seed {
			var seed struct {
				Type string `json:"type"`
				ID   string `json:"id"`
			}
			if json.Unmarshal(raw, &seed) != nil || seed.Type == "" || seed.ID == "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid demo seed record")
			}
			if p.Rows.Exists(seed.Type, seed.ID) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Demo seed record {type}/{id} already exists", seed.Type, seed.ID)
			}
		}
	}
	if space := Space(p.Descriptor); space != "" && p.Managed.Taken() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A project named {name} already exists; install under another name", space)
	}
	code, err := p.Code()
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	candidate, err := BlueprintCandidate(p.Resolver, *p.Descriptor.Blueprint, p.Owner, p.Available)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	owned, err := p.Owner.CompleteBlueprintAssets(p.Descriptor.Blueprint.Assets)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	refs, refusal := p.Managed.Owned(code, owned)
	if refusal != nil {
		return nil, refusal
	}
	impact, err := p.Impact(p.Descriptor.ID, owned)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Package impact could not be inspected: {reason}", err.Error())
	}
	if len(impact.References) > 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Remove references to {resource} from {consumer} before switching the product version", impact.References[0].Resource.String(), impact.References[0].Consumer.String())
	}
	projectID, _ := p.Managed.Project()
	context := protocol.AssetReleaseContext{Member: p.Caller.Member, At: p.At, ManagedBy: p.Descriptor.ID, ManagedAssets: refs, Project: projectID, Upgrade: p.Upgrade}
	if err := p.Bootstrap(candidate, context); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package cannot be installed: {reason}", err.Error())
	}
	registry, err := p.Registry()
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
	if err := p.CheckExtensions(); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package version breaks an extension: {reason}", err.Error())
	}
	if err := p.CheckStorage(p.Upgrade, p.Downgrade); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The package version cannot read the current data: {reason}", err.Error())
	}
	media := make([]platform.ReleaseInstallation, 0, len(plans))
	for _, plan := range plans {
		ref := plan.Ref
		media = append(media, platform.ReleaseInstallation{ReleasePublication: platform.ReleasePublication{Schema: plan.Schema, Image: plan.Image}, Ref: &ref, Record: plan.Record})
	}
	mediaVersions, err := records.PrepareMediaVersionImages(p.Projection, media)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	return func(r *pb.ChangeRecord) { p.Managed.Install(r, refs, plans, mediaVersions) }, nil
}

func (m Managed) Project() (id, name string) {
	if space := Space(m.Descriptor); space != "" {
		return space, space
	}
	for _, resource := range m.Resources {
		if resource.ManagedBy == m.Descriptor.ID && resource.Type == protocol.AppType && resource.Draft == resource.Project {
			return resource.Project, resource.Ref.Name
		}
	}
	hash := sha256.Sum256([]byte(m.Descriptor.ID))
	return "package:" + m.Descriptor.ID, fmt.Sprintf("pkg%x", hash[:12])
}

func (m Managed) Taken() bool {
	_, name := m.Project()
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: name}
	held := false
	for _, resource := range m.Resources {
		if resource.Ref == ref {
			if resource.ManagedBy != m.Descriptor.ID {
				return true
			}
			held = true
		}
	}
	_, exists := platform.Get[protocol.Application](m.Caller, name)
	return !held && exists
}

func (m Managed) archive(r *pb.ChangeRecord, resource protocol.Resource, archived bool) {
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
	if project, exists := platform.Get[protocol.Application](m.Caller, projectID); exists {
		project.Archived = true
		m.Caller.Put(r, project)
	}
}

func (m Managed) Owned(code, owned []platform.ReleaseAsset) (map[platform.AssetRef]bool, *platform.Error) {
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

func (m Managed) Install(r *pb.ChangeRecord, refs map[platform.AssetRef]bool, plans []protocol.AssetInstallation, media []platform.MediaSetVersion) {
	tenant := m.Caller.Member.Tenant
	projectID, projectName := m.Project()
	project, _ := platform.Get[protocol.Application](m.Caller, projectID)
	project.ID, project.Name, project.Title, project.State, project.Archived = projectID, projectName, m.Descriptor.Title, "published", false
	m.Caller.Put(r, project)
	projectRef := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: projectName}
	m.Caller.Put(r, protocol.Resource{Record: platform.Record{ID: protocol.ResourceID(tenant, projectRef)}, Ref: projectRef, Type: protocol.AppType, Draft: projectID, Project: projectID, Title: m.Descriptor.Title, State: "active", Version: 1, ManagedBy: m.Descriptor.ID})
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
		resource, _ := platform.Get[protocol.Resource](m.Caller, protocol.ResourceID(tenant, ref))
		resource.ID, resource.Ref, resource.Type, resource.Draft = protocol.ResourceID(tenant, ref), ref, typ, id
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
