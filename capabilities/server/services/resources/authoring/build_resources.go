package build

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const ResourceType = "build.resource"
const FolderType = "build.folder"
const SchemaResourceRegister = ResourceType + ".register"
const SchemaResourceMove = ResourceType + ".move"
const WorkspaceProject = "workspace"

type Resource struct {
	platform.Record
	Ref       platform.AssetRef `json:"ref" field:"readonly" type:"json"`
	Type      string            `json:"type,omitempty" field:"readonly"`
	Draft     string            `json:"draft,omitempty" field:"readonly"`
	Variant   string            `json:"variant,omitempty" field:"readonly"`
	Title     string            `json:"title" field:"readonly,search"`
	Project   string            `json:"project" field:"readonly"`
	Folder    string            `json:"folder,omitempty" field:"readonly"`
	Version   uint32            `json:"version" field:"readonly"`
	State     string            `json:"state" field:"readonly"`
	ManagedBy string            `json:"managedBy,omitempty" field:"readonly"`
}

type Folder struct {
	platform.Record
	Name    string `json:"name" field:"required,search"`
	Project string `json:"project" field:"required"`
	Parent  string `json:"parent,omitempty"`
}

func (b *Build) resourceEntities() []platform.Entity {
	return []platform.Entity{
		{Type: ResourceType, Title: "Resource", Plural: "Resources", Model: Resource{}, Display: "title", Scope: platform.Scope{Default: platform.ScopeTenant}},
		{Type: FolderType, Title: "Folder", Plural: "Folders", Model: Folder{}, Display: "name", Scope: platform.Scope{Default: platform.ScopeTenant}, Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "resources"}, Validate: func(c platform.Caller, record any) *kernel.Error {
			folder := record.(*Folder)
			project, found := platform.Get[Application](c, folder.Project)
			if !found || project.Archived || !canEditProject(c, folder.Project) {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Choose a project you can edit")
			}
			for parent := folder.Parent; parent != ""; {
				if parent == folder.ID {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A folder cannot contain itself")
				}
				ancestor, found := platform.Get[Folder](c, parent)
				if !found || ancestor.Archived || ancestor.Project != folder.Project {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a folder in the same project")
				}
				parent = ancestor.Parent
			}
			return nil
		}},
	}
}

func canEditProject(c platform.Caller, project string) bool {
	if c.Automation || c.Replaying || c.Holds("platform", "admin") {
		return true
	}
	for _, grant := range c.Grants {
		if grant.App == ID && grant.Role == Builder && (!strings.HasPrefix(grant.By, "project:") || grant.By == "project:"+project) {
			return true
		}
	}
	return len(c.Grants) == 0 && c.Holds(ID, Builder)
}

func resourceActions(native []platform.Action) []platform.Action {
	resources := []platform.Action{
		{Schema: SchemaResourceRegister, Target: ResourceType, Title: "Register resource", Description: "Record the stable identity and location of an existing resource.", Automation: true, Payload: []platform.Field{{Name: "resources", Type: "json", Required: true}}},
		{Schema: SchemaResourceMove, Target: ResourceType, Title: "Move resource", Description: "Move a resource to a project or folder without changing its identity.", Capability: "resources", Roles: []string{Builder}, Payload: []platform.Field{{Name: "project", Type: "string", Required: true}, {Name: "folder", Type: "string"}}},
	}
	for _, action := range native {
		if BranchDraftType(action.Target) && action.Schema == action.Target+".create" {
			resources = append(resources, platform.Action{Schema: action.Target + ".save", Target: action.Target, Title: "Save resource", Description: "Review and save this definition in its owning project.", Capability: action.Capability, Roles: action.Roles, Payload: []platform.Field{{Name: "spec", Type: "json", Required: true}, {Name: "project", Type: "string"}, {Name: "specSchema", Type: "json"}}})
		}
	}
	return resources
}

func IsResourceSave(s *pb.Submission) bool {
	return BranchDraftType(s.GetTarget().GetType()) && s.GetSchema().GetName() == s.GetTarget().GetType()+".save"
}

func (b *Build) submitResource(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if IsResourceSave(s) {
		return b.saveResource(c, s, now)
	}
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		id := s.GetTarget().GetId()
		resource, found := platform.Get[Resource](c, id)
		if s.GetSchema().GetName() == SchemaResourceRegister {
			var input struct {
				Resources []Resource `json:"resources"`
			}
			if json.Unmarshal(s.GetPayload(), &input) != nil || len(input.Resources) == 0 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the resources to register")
			}
			pending := []Resource{}
			for _, item := range input.Resources {
				if item.Ref.Check() != nil || platform.ResourceID(c.Tenant, item.Ref) != item.ID {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A resource registration needs its RID and reference")
				}
				if _, exists := platform.Get[Resource](c, item.ID); !exists {
					pending = append(pending, item)
				}
			}
			return func(r *pb.ChangeRecord) {
				b.ensureWorkspace(c, r)
				for _, item := range pending {
					c.Put(r, item)
				}
			}, nil
		} else {
			if !found {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The resource is unavailable")
			}
			var input struct {
				Project string `json:"project"`
				Folder  string `json:"folder"`
			}
			if json.Unmarshal(s.GetPayload(), &input) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a destination project and folder")
			}
			project, exists := platform.Get[Application](c, input.Project)
			if !exists || project.Archived || !canEditProject(c, resource.Project) || !canEditProject(c, input.Project) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Moving a resource requires editing both projects")
			}
			if input.Folder != "" {
				folder, exists := platform.Get[Folder](c, input.Folder)
				if !exists || folder.Archived || folder.Project != input.Project {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a folder in the destination project")
				}
			}
			if resource.ManagedBy != "" && input.Project != resource.Project {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Managed resources remain in their installed project")
			}
			resource.Project, resource.Folder = input.Project, input.Folder
		}
		return func(r *pb.ChangeRecord) { b.ensureWorkspace(c, r); c.Put(r, resource) }, nil
	})
}

func (b *Build) ensureWorkspace(c platform.Caller, r *pb.ChangeRecord) {
	if _, exists := platform.Get[Application](c, WorkspaceProject); !exists {
		c.Put(r, Application{Record: platform.Record{ID: WorkspaceProject}, Name: WorkspaceProject, Title: "Workspace", Pages: []string{}, State: "draft"})
		ref := platform.AssetRef{App: ID, Kind: platform.AssetApp, Name: WorkspaceProject}
		c.Put(r, Resource{Record: platform.Record{ID: platform.ResourceID(c.Tenant, ref)}, Ref: ref, Type: AppType, Draft: WorkspaceProject, Project: WorkspaceProject, Title: "Workspace", State: "active", Version: 1})
	}
}

func (b *Build) trackResource(c platform.Caller, r *pb.ChangeRecord, typ, id string, at time.Time) {
	if !BranchDraftType(typ) {
		return
	}
	raw, refusal := c.ReadRecord(typ, id, at)
	if refusal != nil {
		return
	}
	var draft struct {
		platform.Record
		Name       string            `json:"name"`
		Title      string            `json:"title"`
		State      string            `json:"state"`
		Version    uint32            `json:"version"`
		Published  string            `json:"published"`
		Kind       string            `json:"kind"`
		Evaluation []json.RawMessage `json:"evaluation"`
	}
	if json.Unmarshal(raw, &draft) != nil {
		return
	}
	ref, err := BranchResource(typ, draft.Name)
	if err != nil {
		return
	}
	if typ == TestPlanType && len(draft.Evaluation) > 0 {
		ref.Kind = platform.AssetEvaluation
	}
	rid := platform.ResourceID(c.Tenant, ref)
	resource, exists := platform.Get[Resource](c, rid)
	if !exists {
		resource = Resource{Record: platform.Record{ID: rid}, Ref: ref, Type: typ, Draft: id, Project: WorkspaceProject}
	}
	if typ == AppType {
		resource.Project = id
	}
	resource.Title = draft.Title
	if resource.Title == "" {
		resource.Title = ref.Name
	}
	resource.Version++
	resource.Variant = draft.Kind
	resource.State = "active"
	if draft.Archived {
		resource.State = "archived"
	}
	b.ensureWorkspace(c, r)
	c.Put(r, resource)
}

func (b *Build) ResourceInventory(c platform.Caller) ([]Resource, error) {
	inventory, err := readDefinitionInventory[Resource](c)
	slices.SortFunc(inventory, func(a, b Resource) int { return strings.Compare(a.ID, b.ID) })
	return inventory, err
}

func (b *Build) saveResource(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var input struct {
			Spec       json.RawMessage         `json:"spec"`
			Project    string                  `json:"project"`
			SpecSchema *platform.SchemaVersion `json:"specSchema,omitempty"`
		}
		typ, id := s.GetTarget().GetType(), s.GetTarget().GetId()
		if json.Unmarshal(s.GetPayload(), &input) != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a resource type, identity and definition")
		}
		var prior struct {
			platform.Record
			Name string `json:"name"`
		}
		raw, refusal := c.ReadRecord(typ, id, now)
		if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			return nil, refusal
		}
		exists := refusal == nil
		if exists {
			if err := json.Unmarshal(raw, &prior); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		var meta struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(input.Spec, &meta) != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The resource definition is one JSON object")
		}
		if exists && meta.Name != "" && meta.Name != prior.Name {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A resource API name is immutable")
		}
		if meta.Name == "" {
			meta.Name = prior.Name
		}
		ref, err := BranchResource(typ, meta.Name)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		rid := platform.ResourceID(c.Tenant, ref)
		resource, known := platform.Get[Resource](c, rid)
		if known {
			input.Project = resource.Project
			if resource.ManagedBy != "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Managed resources are read-only; create an extension")
			}
		}
		if input.Project == "" {
			input.Project = WorkspaceProject
		}
		if !canEditProject(c, input.Project) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Choose a project you can edit")
		}
		if !known {
			resource = Resource{Record: platform.Record{ID: rid}, Ref: ref, Type: typ, Draft: id, Project: input.Project, Title: meta.Name, State: "new"}
		}
		entity := platform.Entity{}
		for _, candidate := range b.Manifest().Entities {
			if candidate.Type == typ {
				entity = candidate
				break
			}
		}
		if typ == PageType {
			entity = b.pageDraftEntity()
		}
		if typ == AgentType {
			entity = b.agentDraftEntity()
		}
		verb := "create"
		if exists {
			verb = "edit"
		}
		if refusal := b.host.CheckResourceSchema(typ+"."+verb, input.SpecSchema); refusal != nil {
			return nil, refusal
		}
		return func(r *pb.ChangeRecord) {
			b.ensureWorkspace(c, r)
			if !known {
				c.Put(r, resource)
			}
			var refusal *kernel.Error
			if typ == ObjectType {
				refusal = b.writeObject(c, r, verb, id, input.Spec, now)
			} else if typ == ActionTypeType {
				refusal = b.writeActionType(c, r, verb, id, input.Spec, now)
			} else {
				refusal = b.ledger.ApplyTxnRecord(c, r, entity, verb, id, input.Spec, now)
			}
			if refusal != nil {
				c.FailDecision(refusal)
				return
			}
			b.trackResource(c, r, typ, id, now)
		}, nil
	})
}

func (b *Build) TrackResource(c platform.Caller, record *pb.ChangeRecord, at time.Time) {
	target := record.GetSubmission().GetTarget()
	schema := record.GetSubmission().GetSchema().GetName()
	if schema != target.GetType()+".create" && schema != target.GetType()+".edit" && schema != target.GetType()+".archive" && !slices.Contains(b.AcceptedPublicationSchemas(), schema) {
		return
	}
	b.trackResource(c, record, target.GetType(), target.GetId(), at)
}
