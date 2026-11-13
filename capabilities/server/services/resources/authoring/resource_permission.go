package build

import (
	"slices"

	"platformserver/platform"
)

type ResourceRows struct {
	Get       func(typ, id string) (any, bool)
	ForDraft  func(typ, id string) (Resource, bool)
	ForObject func(typ string) (Resource, bool)
	Role      func(project string) string
}

func (r ResourceRows) Permission(m platform.Member, typ, id, kind string) (bool, bool) {
	if id == "" {
		return true, false
	}
	project := ""
	definition := BranchDraftType(typ)
	switch typ {
	case BranchType:
		if row, ok := r.Get(typ, id); ok {
			branch := row.(Branch)
			if len(branch.Changes) == 0 {
				return branch.Created.By == m.ID, false
			}
			for _, change := range branch.Changes {
				if allowed, _ := r.Permission(m, change.Type, change.ID, kind); !allowed {
					return false, false
				}
			}
		}
		return true, false
	case AppType:
		project = id
		if resource, ok := r.ForDraft(typ, id); ok {
			project = resource.Project
		}
	case FolderType:
		row, ok := r.Get(typ, id)
		if !ok {
			return true, false
		}
		project = row.(Folder).Project
	case ResourceType:
		row, ok := r.Get(typ, id)
		if !ok {
			return true, false
		}
		project = row.(Resource).Project
	default:
		if resource, ok := r.ForDraft(typ, id); ok {
			project = resource.Project
		} else if !definition {
			if resource, ok := r.ForObject(typ); ok {
				project = resource.Project
			}
		}
	}
	if project == "" {
		return definition, false
	}
	role := r.Role(project)
	allowed := role != "" && role != platform.ProjectDiscovererRole
	if typ == ResourceType || typ == FolderType {
		allowed = role != ""
	}
	if !slices.Contains([]string{"read", "derive", "export"}, kind) {
		allowed = role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole
	}
	return allowed, definition && allowed
}
