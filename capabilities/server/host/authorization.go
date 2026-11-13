package host

import (
	"encoding/json"
	runtimekernel "platformserver/kernel"
	controlowner "platformserver/services/security/control"

	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"platformserver/services/security"
)

func (t *Tenant) delegatedBound(m platform.Member, s *pb.Submission) *kernel.Error {
	owner := t.App(t.owner["action:"+s.GetSchema().GetName()])
	if owner == nil || owner.Manifest().ID != build.ID {
		return nil
	}
	access, _ := t.App(controlowner.PlatformApp).(platform.ProjectAccess)
	return security.ProjectDelegation{
		App:     build.ID,
		Builder: build.Builder,
		Exempt: func(s *pb.Submission) bool {
			return build.IsResourceSave(s) || s.GetSchema().GetName() == build.SchemaResourceMove || s.GetTarget().GetType() == build.WorkingStateType || s.GetTarget().GetType() == build.FolderType
		},
		Create:    build.IsResourceCreate,
		Permits:   func(roles []string, action string) bool { return owner.Manifest().Actions.PermitsAny(roles, action) },
		Target:    runtimekernel.Target,
		DraftName: t.draftName,
		Edits:     t.projectEdits,
	}.Bound(m, s, access)
}

var projectRefKinds = map[string][]string{
	"process": {"flow"}, "code": {"compute"}, "table": {"compute"}, "testplan": {"test-plan", "evaluation-set"},
	"match": {"matching"}, "linktype": {"link-type"}, "propertytype": {"property-type"}, "valuetype": {"value-type"},
	"objecttypegroup": {"object-type-group"},
}

func (t *Tenant) projectEdits(member string, access platform.ProjectAccess, kind string, names []string) bool {
	t.Projection.Lock()
	covering := []string{}
	if resources := t.Projection.Type(build.ResourceType); resources != nil {
		kinds := append([]string{kind}, projectRefKinds[kind]...)
		for _, row := range resources.Rows().All() {
			resource := row.Value.Interface().(build.Resource)
			if slices.Contains(kinds, string(resource.Ref.Kind)) && slices.ContainsFunc(names, func(name string) bool {
				return name == resource.Draft || name == resource.Ref.Name || build.ID+"."+name == resource.Ref.Name
			}) {
				covering = append(covering, resource.Project)
			}
		}
	}
	t.Projection.Unlock()
	for _, project := range covering {
		if role := access.ProjectRole(member, project); role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole {
			return true
		}
	}
	return false
}

func (t *Tenant) draftName(s *pb.Submission) string {
	if strings.HasSuffix(s.GetSchema().GetName(), ".create") {
		var payload struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(s.GetPayload(), &payload)
		return payload.Name
	}
	t.Projection.Lock()
	defer t.Projection.Unlock()
	et := t.Projection.Type(s.GetTarget().GetType())
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

func (t *Tenant) member(id string) (platform.Member, bool) {
	if t.identity == nil {
		return platform.Member{}, false
	}
	member, found := t.identity.MemberByID(id)
	return member.WithAuthorization(t.services.Authorization), found
}
