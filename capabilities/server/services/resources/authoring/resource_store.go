package build

import (
	"encoding/json"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func StoreResourceRows(store *records.Store, role func(project string) string) ResourceRows {
	return ResourceRows{
		Get: func(typ, id string) (any, bool) {
			if row := store.Type(typ).Rows().Get(id); row != nil {
				return row.Value.Interface(), true
			}
			return nil, false
		},
		ForDraft: func(typ, id string) (Resource, bool) {
			return records.FindRow(store, ResourceType, []records.StateMatch{
				{Path: []string{"value", "type"}, Values: []string{typ}},
				{Path: []string{"value", "draft"}, Values: []string{id}},
			}, func(resource Resource) bool { return resource.Type == typ && resource.Draft == id })
		},
		ForObject: func(typ string) (Resource, bool) {
			return records.FindRow(store, ResourceType, []records.StateMatch{
				{Path: []string{"value", "ref", "kind"}, Values: []string{string(platform.AssetObject)}},
				{Path: []string{"value", "ref", "name"}, Values: []string{typ}},
			}, func(resource Resource) bool {
				return resource.Ref.Kind == platform.AssetObject && resource.Ref.Name == typ
			})
		},
		Role: role,
	}
}

func ResourceActionRetired(store *records.Store, name, target string) bool {
	names := []string{name}
	if target != "" && target != name {
		names = append(names, target)
	}
	_, retired := records.FindRow(store, ResourceType, []records.StateMatch{
		{Path: []string{"value", "state"}, Values: []string{"archived"}},
		{Path: []string{"value", "ref", "name"}, Values: names},
	}, func(resource Resource) bool {
		return resource.ManagedBy != "" && resource.State == "archived" &&
			(resource.Ref.Name == name || resource.Ref.Kind == platform.AssetObject && resource.Ref.Name == target)
	})
	return retired
}

func StoreResources(store *records.Store) []Resource {
	var out []Resource
	if rows := store.Type(ResourceType); rows != nil {
		for _, row := range rows.Rows().All() {
			out = append(out, row.Value.Interface().(Resource))
		}
	}
	return out
}

func StoreDirectory(store *records.Store, role func(project string) string) ResourceDirectory {
	var folders []Folder
	if rows := store.Type(FolderType); rows != nil {
		for _, row := range rows.Rows().All() {
			folders = append(folders, row.Value.Interface().(Folder))
		}
	}
	return NewDirectory(StoreResources(store), folders, role)
}

func StoreBootstrap(tenant string, definitions []platform.Definition, store *records.Store, submit func(*pb.Submission) error) ResourceBootstrap {
	return ResourceBootstrap{Tenant: tenant, Definitions: definitions, Submit: submit,
		Drafts: func(match func(string) bool) []DraftRecord {
			store.Lock()
			defer store.Unlock()
			var out []DraftRecord
			for _, recordType := range store.Types() {
				if typ := recordType.Info().Type; match(typ) {
					for id, row := range recordType.Rows().All() {
						out = append(out, DraftRecord{Type: typ, ID: id, Value: row.Value.Interface()})
					}
				}
			}
			return out
		},
		Exists: func(rid string) bool {
			store.Lock()
			defer store.Unlock()
			existing := store.Type(ResourceType)
			return existing != nil && existing.Rows().Get(rid) != nil
		},
		Snapshot: func() (apps []Application, resources []Resource) {
			store.Lock()
			defer store.Unlock()
			if rows := store.Type(AppType); rows != nil {
				for _, row := range rows.Rows().All() {
					apps = append(apps, row.Value.Interface().(Application))
				}
			}
			return apps, StoreResources(store)
		}}
}

func MarkedResources(marked map[platform.AssetRef]bool, resources []Resource, role func(project string) string) []ResourceEntry {
	out := []ResourceEntry{}
	for _, resource := range resources {
		if marked[resource.Ref] && !resource.Archived {
			held := role(resource.Project)
			out = append(out, ResourceEntry{Resource: resource, Path: []string{}, CanOpen: held != "" && held != platform.ProjectDiscovererRole, CanEdit: false})
		}
	}
	slices.SortFunc(out, func(a, b ResourceEntry) int { return strings.Compare(a.ID, b.ID) })
	return out
}

type BranchReviewers struct {
	People   []platform.ProjectPerson `json:"people"`
	Required int                      `json:"required"`
}

type ReviewerHooks struct {
	Holding []string
	Member  func(id string) (platform.Member, bool)
	Role    func(member platform.Member, project string) string
	Name    func(id string) string
}

func (h ReviewerHooks) Eligible(branch Branch, caller string, projects []string, required int) BranchReviewers {
	out := BranchReviewers{People: []platform.ProjectPerson{}, Required: required}
	for _, id := range h.Holding {
		member, found := h.Member(id)
		if !found || !member.Active() || member.Agent || id == branch.Created.By || id == caller || slices.ContainsFunc(branch.Changes, func(change BranchChange) bool { return change.By == id }) {
			continue
		}
		if !slices.ContainsFunc(projects, func(project string) bool {
			role := h.Role(member, project)
			return role != platform.ProjectOwnerRole && role != platform.ProjectEditorRole
		}) {
			out.People = append(out.People, platform.ProjectPerson{ID: id, Name: h.Name(id)})
		}
	}
	return out
}

type WorkingStateView struct {
	Record *WorkingState `json:"record,omitempty"`
	ID     string        `json:"id"`
}

type WorkingStateList struct {
	Resources []WorkingState `json:"resources"`
}

func ReadWorkingStates(branch string, read func(platform.Query) ([]any, *kernel.Error)) (WorkingStateList, *kernel.Error, error) {
	states := []WorkingState{}
	query := platform.Query{Domain: platform.Raw([][]any{{"branch", "=", branch}}), Limit: 500, Sort: []string{"id"}}
	for {
		page, refusal := read(query)
		if refusal != nil {
			return WorkingStateList{}, refusal, nil
		}
		var selected []WorkingState
		if err := json.Unmarshal(platform.Raw(page), &selected); err != nil {
			return WorkingStateList{}, nil, err
		}
		for _, state := range selected {
			if !state.Archived {
				states = append(states, state)
			}
		}
		if len(page) < query.Limit {
			return WorkingStateList{Resources: states}, nil, nil
		}
		query.Offset += len(page)
	}
}

func ReadWorkingState(id string, raw json.RawMessage, refusal *kernel.Error) (WorkingStateView, *kernel.Error, error) {
	view := WorkingStateView{ID: id}
	if refusal != nil {
		if refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			return WorkingStateView{}, refusal, nil
		}
		return view, nil, nil
	}
	var state WorkingState
	if err := json.Unmarshal(raw, &state); err != nil {
		return WorkingStateView{}, nil, err
	}
	if !state.Archived {
		view.Record = &state
	}
	return view, nil, nil
}

func ProjectHidden(store *records.Store, tenant string, m platform.Member, et *records.Type, role func(m platform.Member, project string) string) bool {
	info := et.Info()
	if !ProjectDelegated(m) || info.App != ID || BranchDraftType(info.Type) || info.Type == ResourceType || info.Type == FolderType || info.Type == WorkingStateType || info.Type == BranchType {
		return false
	}
	resources := store.Type(ResourceType)
	if resources == nil {
		return false
	}
	row := resources.Rows().Get(platform.ResourceID(tenant, platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: info.Type}))
	if row == nil {
		return false
	}
	held := role(m, row.Value.Interface().(Resource).Project)
	return held == "" || held == platform.ProjectDiscovererRole
}

func MaskDraftObject(record any, m platform.Member, reader ConstructionDraftReader, mask func(canonical any) any) (any, *kernel.Error) {
	object, ok := record.(Object)
	if !ok || !m.Holds(ID, Builder) || reader == nil {
		return record, nil
	}
	canonical, err := reader.CanonicalObject(object)
	if err != nil {
		return record, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return mask(canonical), nil
}

func ManagedDefinition(store *records.Store, tenant string, ref platform.AssetRef) bool {
	directory := store.Type(ResourceType)
	if directory == nil {
		return false
	}
	row := directory.Rows().Get(platform.ResourceID(tenant, ref))
	return row != nil && row.Value.Interface().(Resource).ManagedBy != ""
}

func ProjectRoleOf(m platform.Member, access platform.ProjectAccess, project string) string {
	if m.Holds("platform", "admin") || HoldsIndependentRole(m, Builder) {
		return platform.ProjectOwnerRole
	}
	if access == nil {
		return ""
	}
	return access.ProjectRole(m.ID, project)
}
