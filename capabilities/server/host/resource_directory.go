package host

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type ResourceEntry struct {
	build.Resource
	Path    []string `json:"path"`
	CanOpen bool     `json:"canOpen"`
	CanEdit bool     `json:"canEdit"`
}

type ResourceDirectory struct {
	Resources []ResourceEntry `json:"resources"`
	Folders   []build.Folder  `json:"folders"`
}

func (t *Tenant) resourceActionActive(name string) bool {
	target := ""
	if owner := t.owner["action:"+name]; owner != nil {
		action, _ := owner.Manifest().Actions.Action(name)
		target = action.Target
	}
	if directory := t.records.Type(build.ResourceType); directory != nil {
		for _, row := range directory.Rows().All() {
			resource := row.Value.Interface().(build.Resource)
			if resource.ManagedBy != "" && resource.State == "archived" && (resource.Ref.Name == name || resource.Ref.Kind == platform.AssetObject && resource.Ref.Name == target) {
				return false
			}
		}
	}
	return true
}

func projectDelegated(m platform.Member) bool {
	return slices.ContainsFunc(m.Grants, func(g platform.Grant) bool { return g.App == build.ID && strings.HasPrefix(g.By, "project:") }) && !holdsIndependentBuildRole(m, build.Builder)
}

func (t *Tenant) resourceRole(m platform.Member, project string) string {
	if m.Holds("platform", "admin") || holdsIndependentBuildRole(m, build.Builder) {
		return platform.ProjectOwnerRole
	}
	if access, ok := t.app("platform").(platform.ProjectAccess); ok {
		return access.ProjectRole(m.ID, project)
	}
	return ""
}

func (t *Tenant) resourcePermission(store *records.Store, m platform.Member, typ, id, kind string, held bool) (bool, bool) {
	if !projectDelegated(m) {
		return true, false
	}
	if id == "" {
		return true, false
	}
	if !held {
		store.Lock()
		defer store.Unlock()
	}
	project := ""
	definition := build.BranchDraftType(typ)
	switch typ {
	case build.BranchType:
		if row := store.Type(typ).Rows().Get(id); row != nil {
			branch := row.Value.Interface().(build.Branch)
			if len(branch.Changes) == 0 {
				return branch.Created.By == m.ID, false
			}
			for _, change := range branch.Changes {
				if allowed, _ := t.resourcePermission(store, m, change.Type, change.ID, kind, true); !allowed {
					return false, false
				}
			}
		}
		return true, false
	case build.AppType:
		project = id
		if directory := store.Type(build.ResourceType); directory != nil {
			for _, row := range directory.Rows().All() {
				resource := row.Value.Interface().(build.Resource)
				if resource.Type == typ && resource.Draft == id {
					project = resource.Project
					break
				}
			}
		}
	case build.FolderType:
		if row := store.Type(typ).Rows().Get(id); row != nil {
			project = row.Value.Interface().(build.Folder).Project
		}
	case build.ResourceType:
		if row := store.Type(typ).Rows().Get(id); row != nil {
			project = row.Value.Interface().(build.Resource).Project
		}
	default:
		if resources := store.Type(build.ResourceType); resources != nil {
			for _, row := range resources.Rows().All() {
				resource := row.Value.Interface().(build.Resource)
				if resource.Type == typ && resource.Draft == id || resource.Ref.Kind == platform.AssetObject && resource.Ref.Name == typ {
					project = resource.Project
					break
				}
			}
		}
	}
	if project == "" {
		return !definition, false
	}
	role := t.resourceRole(m, project)
	allowed := role != "" && role != platform.ProjectDiscovererRole
	if typ == build.ResourceType || typ == build.FolderType {
		allowed = role != ""
	}
	if !slices.Contains([]string{"read", "derive", "export"}, kind) {
		allowed = role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole
	}
	return allowed, definition && allowed
}

func (t *Tenant) resourceDirectory(m platform.Member) ResourceDirectory {
	out := ResourceDirectory{Resources: []ResourceEntry{}, Folders: []build.Folder{}}
	t.records.Lock()
	defer t.records.Unlock()
	if resources := t.records.Type(build.ResourceType); resources != nil {
		for _, row := range resources.Rows().All() {
			resource := row.Value.Interface().(build.Resource)
			role := t.resourceRole(m, resource.Project)
			if role == "" {
				continue
			}
			out.Resources = append(out.Resources, ResourceEntry{Resource: resource, CanOpen: role != platform.ProjectDiscovererRole, CanEdit: resource.ManagedBy == "" && (role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole)})
		}
	}
	if folders := t.records.Type(build.FolderType); folders != nil {
		for _, row := range folders.Rows().All() {
			folder := row.Value.Interface().(build.Folder)
			if !folder.Archived && t.resourceRole(m, folder.Project) != "" {
				out.Folders = append(out.Folders, folder)
			}
		}
	}
	projects := map[string]string{}
	for _, resource := range out.Resources {
		if resource.Type == build.AppType {
			projects[resource.Draft] = resource.Title
		}
	}
	folders := map[string]build.Folder{}
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
	slices.SortFunc(out.Folders, func(a, b build.Folder) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (t *Tenant) initializeResources(now time.Time) error {
	owner := t.app(build.ID)
	if owner == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if refusal := t.recordReadRefusal(); refusal != nil {
		return refusal
	}
	directory := map[string]build.Resource{}
	legacyObjects := []string{}
	t.records.Lock()
	for _, recordType := range t.records.Types() {
		info := recordType.Info()
		if !build.BranchDraftType(info.Type) {
			continue
		}
		for id, row := range t.records.Type(info.Type).Rows().All() {
			if info.Type == build.ObjectType && len(row.Value.Interface().(build.Object).Actions) > 0 {
				legacyObjects = append(legacyObjects, id)
			}
			raw, _ := json.Marshal(row.Value.Interface())
			var meta struct {
				platform.Record
				Name       string            `json:"name"`
				Title      string            `json:"title"`
				Published  string            `json:"published"`
				Kind       string            `json:"kind"`
				Evaluation []json.RawMessage `json:"evaluation"`
			}
			if err := json.Unmarshal(raw, &meta); err != nil {
				t.records.Unlock()
				return err
			}
			ref, err := build.BranchResource(info.Type, meta.Name)
			if err != nil {
				t.records.Unlock()
				return err
			}
			if info.Type == build.TestPlanType && len(meta.Evaluation) > 0 {
				ref.Kind = platform.AssetEvaluation
			}
			rid := platform.ResourceID(t.ID, ref)
			project := build.WorkspaceProject
			if info.Type == build.AppType {
				project = id
			}
			state := "active"
			if meta.Archived {
				state = "archived"
			}
			directory[rid] = build.Resource{Record: platform.Record{ID: rid}, Ref: ref, Type: info.Type, Draft: id, Title: meta.Title, Variant: meta.Kind, Project: project, State: state, Version: meta.Revision}
		}
	}
	if existing := t.records.Type(build.ResourceType); existing != nil {
		for id := range existing.Rows().All() {
			delete(directory, id)
		}
	}
	t.records.Unlock()
	for _, definition := range t.definitions {
		rid := platform.ResourceID(t.ID, definition.Ref)
		if _, exists := directory[rid]; exists {
			continue
		}
		t.records.Lock()
		existing := t.records.Type(build.ResourceType).Rows().Get(rid) != nil
		t.records.Unlock()
		if existing {
			continue
		}
		directory[rid] = build.Resource{Record: platform.Record{ID: rid}, Ref: definition.Ref, Title: definition.Ref.Name, Project: build.WorkspaceProject, State: "active", Version: 1, ManagedBy: "platform"}
	}
	pending := []build.Resource{}
	for _, rid := range slices.SortedFunc(maps.Keys(directory), strings.Compare) {
		pending = append(pending, directory[rid])
	}
	if len(pending) > 0 {
		digest, err := platform.CanonicalDigest(pending)
		if err != nil {
			return err
		}
		submission := &pb.Submission{TenantId: t.ID, Authority: build.ID, IdempotencyKey: "resource-directory-v1:" + digest, Schema: &pb.SchemaRef{Name: build.SchemaResourceRegister, Version: 1}, Target: &pb.EntityRef{Type: build.ResourceType, Id: "directory-v1"}, Payload: platform.Raw(map[string]any{"resources": pending})}
		if refusal := t.submitAutomated(owner, t.automation(build.ID, false), submission, now); refusal != nil {
			return refusal
		}
	}
	for _, id := range legacyObjects {
		submission := &pb.Submission{TenantId: t.ID, Authority: build.ID, IdempotencyKey: "action-types-v1:" + id, Schema: &pb.SchemaRef{Name: build.SchemaExtractActions, Version: 1}, Target: &pb.EntityRef{Type: build.ObjectType, Id: id}, Payload: platform.Raw(map[string]any{})}
		if refusal := t.submitAutomated(owner, t.automation(build.ID, false), submission, now); refusal != nil {
			return refusal
		}
	}
	return nil
}

type BranchReviewers struct {
	People   []platform.ProjectPerson `json:"people"`
	Required int                      `json:"required"`
}

type WorkingStateView struct {
	Record *build.WorkingState `json:"record,omitempty"`
	ID     string              `json:"id"`
}

type WorkingStateList struct {
	Resources []build.WorkingState `json:"resources"`
}

func (h *Host) routesResources(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/markings/{id}/resources", Summary: "Resources carrying a marking, without their data", Answer: []ResourceEntry{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		caller := t.caller(m, t.app("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(caller, "GET /v1/markings/{id}/resources", "read", []string{"admin", platform.Auditor}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		id := r.PathValue("id")
		marked := map[platform.AssetRef]bool{}
		for _, node := range t.markingsIn(t.records, false).graph.Nodes {
			if slices.Contains(node.EffectiveCompartments, id) || slices.Contains(platform.Markings[1:], id) && platform.MarkingRank(node.Effective) >= platform.MarkingRank(id) {
				marked[platform.AssetRef{App: build.ID, Kind: platform.AssetKind(node.Kind), Name: node.Name}] = true
			}
		}
		resources := []ResourceEntry{}
		t.records.Lock()
		for _, row := range t.records.Type(build.ResourceType).Rows().All() {
			resource := row.Value.Interface().(build.Resource)
			if marked[resource.Ref] && !resource.Archived {
				role := t.resourceRole(m, resource.Project)
				resources = append(resources, ResourceEntry{Resource: resource, Path: []string{}, CanOpen: role != "" && role != platform.ProjectDiscovererRole, CanEdit: false})
			}
		}
		t.records.Unlock()
		slices.SortFunc(resources, func(a, b ResourceEntry) int { return strings.Compare(a.ID, b.ID) })
		WriteJSON(w, http.StatusOK, resources)
	})

	rt.recordRead(transport.Route{Pattern: "GET /v1/branches/{id}/reviewers", Summary: "Eligible non-author editors and the project approval requirement", Answer: BranchReviewers{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		branch, refusal := t.readBranchLocked(m, r.PathValue("id"), h.Now())
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		projects, required, err := t.app(build.ID).(*build.Build).BranchReviewRequirement(t.branchCaller(m), branch)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := BranchReviewers{People: []platform.ProjectPerson{}, Required: required}
		for _, id := range t.identity.Holding(build.ID, build.Builder) {
			member, found := t.Member(id)
			if !found || !member.Active() || member.Agent || id == branch.Created.By || id == m.ID || slices.ContainsFunc(branch.Changes, func(change build.BranchChange) bool { return change.By == id }) {
				continue
			}
			eligible := true
			for _, project := range projects {
				role := t.resourceRole(member, project)
				if role != platform.ProjectOwnerRole && role != platform.ProjectEditorRole {
					eligible = false
					break
				}
			}
			if eligible {
				name := id
				if profile, ok := t.app("platform").(platform.IdentityProfile); ok {
					if display := profile.Account(id).DisplayName; display != "" {
						name = display
					}
				}
				out.People = append(out.People, platform.ProjectPerson{ID: id, Name: name})
			}
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/working-state/{branch}", Summary: "All current builder's private resource edits on this branch", Answer: WorkingStateList{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		states := []build.WorkingState{}
		query := platform.Query{Domain: platform.Raw([][]any{{"branch", "=", r.PathValue("branch")}}), Limit: 500, Sort: []string{"id"}}
		for {
			page, refusal := t.recordsRead(m, build.WorkingStateType, query, h.Now())
			if refusal != nil {
				Reply(w, nil, refusal)
				return
			}
			var selected []build.WorkingState
			if err := json.Unmarshal(platform.Raw(page.Records), &selected); err != nil {
				WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			for _, state := range selected {
				if !state.Archived {
					states = append(states, state)
				}
			}
			if len(page.Records) < query.Limit {
				break
			}
			query.Offset += len(page.Records)
		}
		WriteJSON(w, http.StatusOK, WorkingStateList{Resources: states})
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/working-state/{branch}/{type}/{draft}", Summary: "Only the current builder's private working state", Answer: WorkingStateView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		id := build.WorkingStateID(m.ID, r.PathValue("branch"), r.PathValue("type"), r.PathValue("draft"))
		raw, refusal := t.readRecordFrom(t.records, t.branchCaller(m), build.WorkingStateType, id, h.Now())
		view := WorkingStateView{ID: id}
		if refusal == nil {
			var state build.WorkingState
			if err := json.Unmarshal(raw, &state); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !state.Archived {
				view.Record = &state
			}
		} else if refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, view)
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/resources", Summary: "Visible resource identities and their project and folder locations", Answer: ResourceDirectory{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.resourceDirectory(m))
	})
	rt.recordRead(transport.Route{Pattern: "GET /v1/resources/{rid}", Summary: "Resolve one stable resource identity", Answer: ResourceEntry{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		for _, entry := range t.resourceDirectory(m).Resources {
			if entry.ID == r.PathValue("rid") {
				WriteJSON(w, http.StatusOK, entry)
				return
			}
		}
		Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The resource is unavailable"))
	})
}
