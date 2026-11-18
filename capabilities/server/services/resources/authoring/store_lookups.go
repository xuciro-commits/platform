package authoring

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func StoreResourceRow(store *records.Store, tenant string, ref platform.AssetRef) (protocol.Resource, bool) {
	row, ok := store.LockedRow(ResourceType, protocol.ResourceID(tenant, ref))
	if !ok {
		return protocol.Resource{}, false
	}
	return row.(protocol.Resource), true
}

func StoreProjectTitle(store *records.Store, project string) string {
	if row, ok := store.LockedRow(protocol.AppType, project); ok {
		return row.(protocol.Application).Title
	}
	return ""
}

func StoreApplicationEntryWithdrawn(store *records.Store, ref platform.AssetRef) bool {
	if ref.App != ID || ref.Kind != platform.AssetApp {
		return false
	}
	store.Lock()
	defer store.Unlock()
	entity := store.Type(protocol.AppType)
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

func StoreRetainedRecordLink(store *records.Store, tenant, app, page, id string, readable func(string) bool) (protocol.RetainedRecordLink, *platform.Error) {
	missing := platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This record link is unavailable")
	root, _ := StoreResourceRow(store, tenant, platform.AssetRef{App: ID, Kind: platform.AssetApp, Name: app})
	if !managedResourceRetired(root) {
		return protocol.RetainedRecordLink{}, missing
	}
	row, ok := records.LockedRow[protocol.Application](store, protocol.AppType, root.Draft)
	if !ok {
		return protocol.RetainedRecordLink{}, missing
	}
	application, ok := publication.Read[protocol.Application](row.Published)
	if !ok || !slices.Contains(application.Pages, page) {
		return protocol.RetainedRecordLink{}, missing
	}
	entry, _ := StoreResourceRow(store, tenant, platform.AssetRef{App: ID, Kind: platform.AssetPage, Name: page})
	draft, ok := records.LockedRow[protocol.Page](store, protocol.PageType, entry.Draft)
	if !ok {
		return protocol.RetainedRecordLink{}, missing
	}
	published, ok := publication.Read[protocol.Page](draft.Published)
	if !ok || !readable(published.Object+"/"+id) {
		return protocol.RetainedRecordLink{}, missing
	}
	return protocol.RetainedRecordLink{Object: published.Object}, nil
}

var projectRefKinds = map[string][]string{
	"process": {"flow"}, "code": {"compute"}, "table": {"compute"}, "testplan": {"test-plan", "evaluation-set"},
	"match": {"matching"}, "linktype": {"link-type"}, "propertytype": {"property-type"}, "valuetype": {"value-type"},
	"objecttypegroup": {"object-type-group"},
}

func StoreProjectEdits(store *records.Store, member string, access protocol.ProjectAccess, kind string, names []string) bool {
	store.Lock()
	covering := []string{}
	if resources := store.Type(ResourceType); resources != nil {
		kinds := append([]string{kind}, projectRefKinds[kind]...)
		for _, row := range resources.Rows().All() {
			resource := row.Value.Interface().(protocol.Resource)
			if slices.Contains(kinds, string(resource.Ref.Kind)) && slices.ContainsFunc(names, func(name string) bool {
				return name == resource.Draft || name == resource.Ref.Name || ID+"."+name == resource.Ref.Name
			}) {
				covering = append(covering, resource.Project)
			}
		}
	}
	store.Unlock()
	for _, project := range covering {
		if role := access.ProjectRole(member, project); role == protocol.ProjectOwnerRole || role == protocol.ProjectEditorRole {
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
