package authoring

import (
	"encoding/json"
	"maps"
	"platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

const memberType = "platform.member"

type ResourceEntry struct {
	protocol.Resource
	Path    []string `json:"path"`
	CanOpen bool     `json:"canOpen"`
	CanEdit bool     `json:"canEdit"`
}

type ResourceDirectory struct {
	Resources []ResourceEntry `json:"resources"`
	Folders   []Folder        `json:"folders"`
}

type LegacyResourceProject struct {
	ID      string `json:"id"`
	Project string `json:"project"`
}

type Hit struct {
	Type      string         `json:"type"`
	TypeTitle string         `json:"typeTitle,omitempty"`
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Group     string         `json:"group"`
	Resource  *ResourceEntry `json:"resource,omitempty"`
}

type Search struct {
	Resources []ResourceEntry
	Entities  []platform.RecordTypeInfo
	Names     func(platform.RecordTypeInfo, string) (string, bool)
	Read      func(typ string, query platform.Query) ([]any, *platform.Error)
}

var searchScopes = map[byte]string{'#': protocol.AppType, ':': ObjectType, '@': memberType}

func (s Search) Find(q string) []Hit {
	out := []Hit{}
	q = strings.TrimSpace(q)
	only := ""
	if q != "" {
		if scope, ok := searchScopes[q[0]]; ok {
			only, q = scope, strings.TrimSpace(q[1:])
		}
	}
	if q == "" && only == "" {
		return out
	}
	text := strings.ToLower(q)
	counts := map[string]int{}
	for _, resource := range s.Resources {
		if resource.State == "archived" || only != "" && resource.Type != only || counts[resource.Type] >= 5 {
			continue
		}
		if !strings.Contains(strings.ToLower(resource.Title+" "+resource.Ref.Name+" "+string(resource.Ref.Kind)+" "+resource.ID), text) {
			continue
		}
		out = append(out, Hit{Type: resource.Type, ID: resource.Draft, Title: resource.Title, Group: "resource", Resource: &resource})
		counts[resource.Type]++
	}
	for _, info := range s.Entities {
		if info.Type == protocol.FollowType || info.Type == ResourceType || BranchDraftType(info.Type) || only != "" && info.Type != only {
			continue
		}
		query := platform.Query{Search: q, Limit: 5}
		if rest, named := s.Names(info, q); named && only == "" {
			query.Search = rest
		}
		page, err := s.Read(info.Type, query)
		if err != nil {
			continue
		}
		group := "object"
		if info.Type == memberType {
			group = "member"
		}
		for _, r := range page {
			v := reflect.ValueOf(r)
			hit := Hit{Type: info.Type, TypeTitle: info.Title, ID: v.FieldByName("ID").String(), Group: group}
			if f, ok := info.Field(info.Display); ok {
				hit.Title = v.FieldByIndex(f.Index).String()
			}
			out = append(out, hit)
		}
	}
	return out
}

func NewDirectory(resources []protocol.Resource, saved []Folder, role func(project string) string) ResourceDirectory {
	out := ResourceDirectory{Resources: []ResourceEntry{}, Folders: []Folder{}}
	for _, resource := range resources {
		role := role(resource.Project)
		if role == "" {
			continue
		}
		out.Resources = append(out.Resources, ResourceEntry{Resource: resource, CanOpen: role != protocol.ProjectDiscovererRole, CanEdit: resource.ManagedBy == "" && (role == protocol.ProjectOwnerRole || role == protocol.ProjectEditorRole)})
	}
	for _, folder := range saved {
		if !folder.Archived && role(folder.Project) != "" {
			out.Folders = append(out.Folders, folder)
		}
	}
	projects := map[string]string{}
	for _, resource := range out.Resources {
		if resource.Type == protocol.AppType {
			projects[resource.Draft] = resource.Title
		}
	}
	folders := map[string]Folder{}
	for _, folder := range out.Folders {
		folders[folder.ID] = folder
	}
	for i := range out.Resources {
		resource := &out.Resources[i]
		for folder := folders[resource.Folder]; folder.ID != ""; folder = folders[folder.Parent] {
			resource.Path = append(resource.Path, folder.Name)
		}
		slices.Reverse(resource.Path)
		resource.Path = append([]string{projects[resource.Project]}, resource.Path...)
	}
	slices.SortFunc(out.Resources, func(a, b ResourceEntry) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(out.Folders, func(a, b Folder) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func LegacyProjectAssignments(apps []protocol.Application, resources []protocol.Resource) []LegacyResourceProject {
	referenced := map[platform.AssetRef][]string{}
	projects := map[string]bool{}
	{
		for _, app := range apps {
			if app.Archived {
				continue
			}
			projects[app.ID] = true
			for _, ref := range app.Resources {
				referenced[ref] = appendUnique(referenced[ref], app.ID)
			}
			for _, page := range app.Pages {
				ref := platform.AssetRef{App: ID, Kind: platform.AssetPage, Name: page}
				referenced[ref] = appendUnique(referenced[ref], app.ID)
			}
		}
	}
	assignments := []LegacyResourceProject{}
	{
		for _, resource := range resources {
			if resource.Project != WorkspaceProject || resource.ManagedBy != "" || resource.Ref.Kind == platform.AssetApp {
				continue
			}
			owners := referenced[resource.Ref]
			if len(owners) != 1 || !projects[owners[0]] || owners[0] == WorkspaceProject {
				continue
			}
			assignments = append(assignments, LegacyResourceProject{ID: resource.ID, Project: owners[0]})
		}
	}
	slices.SortFunc(assignments, func(a, b LegacyResourceProject) int { return strings.Compare(a.ID, b.ID) })
	return assignments
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

type DraftRecord struct {
	Type  string
	ID    string
	Value any
}

type ResourceBootstrap struct {
	Tenant      string
	Definitions []platform.Definition
	Drafts      func(match func(typ string) bool) []DraftRecord
	Exists      func(rid string) bool
	Snapshot    func() ([]protocol.Application, []protocol.Resource)
	Submit      func(*pb.Submission) error
}

func (h ResourceBootstrap) Initialize() error {
	directory := map[string]protocol.Resource{}
	for _, draft := range h.Drafts(BranchDraftType) {
		raw, _ := json.Marshal(draft.Value)
		var meta struct {
			platform.Record
			Name      string `json:"name"`
			Title     string `json:"title"`
			Published string `json:"published"`
			Kind      string `json:"kind"`
			Variant   string `json:"variant"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			return err
		}
		ref, err := BranchResource(draft.Type, meta.Name)
		if err != nil {
			return err
		}
		if draft.Type == TestPlanType && meta.Variant == "evaluation-set" {
			ref.Kind = platform.AssetEvaluation
		}
		rid := protocol.ResourceID(h.Tenant, ref)
		project := WorkspaceProject
		if draft.Type == protocol.AppType {
			project = draft.ID
		}
		state := "active"
		if meta.Archived {
			state = "archived"
		}
		variant := meta.Kind
		if draft.Type == TestPlanType {
			variant = meta.Variant
		}
		directory[rid] = protocol.Resource{Record: platform.Record{ID: rid}, Ref: ref, Type: draft.Type, Draft: draft.ID, Title: meta.Title, Variant: variant, Project: project, State: state, Version: meta.Revision}
	}
	for id := range directory {
		if h.Exists(id) {
			delete(directory, id)
		}
	}
	for _, definition := range h.Definitions {
		rid := protocol.ResourceID(h.Tenant, definition.Ref)
		if _, exists := directory[rid]; exists || h.Exists(rid) {
			continue
		}
		directory[rid] = protocol.Resource{Record: platform.Record{ID: rid}, Ref: definition.Ref, Title: definition.Ref.Name, Project: WorkspaceProject, State: "active", Version: 1, ManagedBy: "platform"}
	}
	pending := []protocol.Resource{}
	for _, rid := range slices.SortedFunc(maps.Keys(directory), strings.Compare) {
		pending = append(pending, directory[rid])
	}
	if len(pending) > 0 {
		digest, err := platform.CanonicalDigest(pending)
		if err != nil {
			return err
		}
		submission := &pb.Submission{TenantId: h.Tenant, Authority: ID, IdempotencyKey: "resource-directory-v1:" + digest, Schema: &pb.SchemaRef{Name: SchemaResourceRegister, Version: 1}, Target: &pb.EntityRef{Type: ResourceType, Id: "directory-v1"}, Payload: platform.Raw(map[string]any{"resources": pending})}
		if err := h.Submit(submission); err != nil {
			return err
		}
	}
	apps, resources := h.Snapshot()
	assignments := LegacyProjectAssignments(apps, resources)
	if len(assignments) == 0 {
		return nil
	}
	digest, err := platform.CanonicalDigest(assignments)
	if err != nil {
		return err
	}
	return h.Submit(&pb.Submission{TenantId: h.Tenant, Authority: ID, IdempotencyKey: "resource-projects-v1:" + digest,
		Schema: &pb.SchemaRef{Name: SchemaResourceMigrateProjects, Version: 1}, Target: &pb.EntityRef{Type: ResourceType, Id: "migrate-projects-v1"},
		Payload: platform.Raw(map[string]any{"assignments": assignments})})
}
