package build

import (
	"encoding/json"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func StoreResourceRow(store *records.Store, tenant string, ref platform.AssetRef) (Resource, bool) {
	row, ok := store.LockedRow(ResourceType, platform.ResourceID(tenant, ref))
	if !ok {
		return Resource{}, false
	}
	return row.(Resource), true
}

func StoreProjectTitle(store *records.Store, project string) string {
	if row, ok := store.LockedRow(AppType, project); ok {
		return row.(Application).Title
	}
	return ""
}

func StoreApplicationEntryWithdrawn(store *records.Store, ref platform.AssetRef) bool {
	if ref.App != ID || ref.Kind != platform.AssetApp {
		return false
	}
	store.Lock()
	defer store.Unlock()
	entity := store.Type(AppType)
	if entity == nil {
		return false
	}
	for _, row := range entity.Rows().All() {
		if !row.Value.FieldByName("Record").Interface().(platform.Record).Archived && records.BusinessField(entity.Info(), row.Value, "name") == ref.Name {
			return records.BusinessField(entity.Info(), row.Value, "state") == "withdrawn"
		}
	}
	return false
}

var projectRefKinds = map[string][]string{
	"process": {"flow"}, "code": {"compute"}, "table": {"compute"}, "testplan": {"test-plan", "evaluation-set"},
	"match": {"matching"}, "linktype": {"link-type"}, "propertytype": {"property-type"}, "valuetype": {"value-type"},
	"objecttypegroup": {"object-type-group"},
}

func StoreProjectEdits(store *records.Store, member string, access platform.ProjectAccess, kind string, names []string) bool {
	store.Lock()
	covering := []string{}
	if resources := store.Type(ResourceType); resources != nil {
		kinds := append([]string{kind}, projectRefKinds[kind]...)
		for _, row := range resources.Rows().All() {
			resource := row.Value.Interface().(Resource)
			if slices.Contains(kinds, string(resource.Ref.Kind)) && slices.ContainsFunc(names, func(name string) bool {
				return name == resource.Draft || name == resource.Ref.Name || ID+"."+name == resource.Ref.Name
			}) {
				covering = append(covering, resource.Project)
			}
		}
	}
	store.Unlock()
	for _, project := range covering {
		if role := access.ProjectRole(member, project); role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole {
			return true
		}
	}
	return false
}

func StoreDraftName(store *records.Store, s *pb.Submission) string {
	if strings.HasSuffix(s.GetSchema().GetName(), ".create") {
		var payload struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(s.GetPayload(), &payload)
		return payload.Name
	}
	store.Lock()
	defer store.Unlock()
	et := store.Type(s.GetTarget().GetType())
	if et == nil {
		return ""
	}
	row := et.Rows().Get(s.GetTarget().GetId())
	field, ok := et.Info().Field("name")
	if row == nil || !ok || field.Index == nil {
		return ""
	}
	return row.Value.FieldByIndex(field.Index).String()
}
