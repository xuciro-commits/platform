package packages

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	dataowner "platformserver/services/data"
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

type ManagedPreparation struct {
	Managed
	Archive    bool
	Available  []platform.ReleaseAsset
	At         time.Time
	Upgrade    bool
	Downgrade  bool
	Owner      build.ConstructionDrafts
	Code       func() ([]platform.ReleaseAsset, error)
	Impact     func(string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
	Bootstrap  func(platform.ReleaseCandidate, platform.AssetReleaseContext) error
	Registry   func() (*platform.AssetRegistry, error)
	Projection *records.Store

	CheckExtensions func() error
	CheckStorage    func(upgrade, downgrade bool) error
}

func NewManaged(caller platform.Caller, descriptor platform.PackageDescriptor, rows func() *records.Store, install func(platform.ReleaseInstallation), fail func(*kernel.Error)) Managed {
	return Managed{Caller: caller, Descriptor: descriptor, Resources: Resources(rows()), Rows: ManagedRows{
		Archived: func(resource build.Resource, archived bool) any {
			return rows().ArchivedCopy(resource.Type, resource.Draft, archived)
		},
		Decode:  func(typ string, image json.RawMessage) (any, error) { return rows().DecodeRow(typ, image) },
		Image:   func(record string) json.RawMessage { return rows().RowImage(record) },
		Install: install,
		Fail:    fail,
	}}
}

func Resources(store *records.Store) []build.Resource {
	var resources []build.Resource
	if directory := store.Type(build.ResourceType); directory != nil {
		for _, row := range directory.Rows().All() {
			resources = append(resources, row.Value.Interface().(build.Resource))
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

func (p ManagedPreparation) Prepare() (func(*pb.ChangeRecord), *kernel.Error) {
	if p.Descriptor.Blueprint == nil && !p.Archive {
		return nil, nil
	}
	if p.Archive {
		return p.Managed.Archive, nil
	}
	code, err := p.Code()
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{reason}", err.Error())
	}
	candidate, err := BlueprintCandidate(*p.Descriptor.Blueprint, p.Owner, p.Available)
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
	context := platform.AssetReleaseContext{Member: p.Caller.Member, At: p.At, ManagedBy: p.Descriptor.ID, ManagedAssets: refs, Project: projectID, Upgrade: p.Upgrade}
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

func TenantSourceRefusal(fixed dataowner.Connection, credential func() string) *kernel.Error {
	if fixed.Archived || fixed.State != "ready" || fixed.Kind != "http" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs a ready HTTP connection")
	}
	address, err := url.Parse(fixed.Address)
	if err != nil || address.Host == "" || address.User != nil || address.RawQuery != "" || address.Fragment != "" || address.Scheme != "https" && !(address.Scheme == "http" && fixed.AllowPrivate) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an HTTP service root")
	}
	if fixed.Secret == "" || credential() == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The tenant source needs an available connection credential")
	}
	return nil
}
