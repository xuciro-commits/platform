package host

import (
	"encoding/json"
	controlowner "platformserver/services/security/control"

	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) delegatedBound(m platform.Member, s *pb.Submission) *kernel.Error {
	owner := t.owner["action:"+s.GetSchema().GetName()]
	if owner == nil || owner.Manifest().ID != build.ID {
		return nil
	}
	if build.IsResourceSave(s) || s.GetSchema().GetName() == build.SchemaResourceMove || s.GetTarget().GetType() == build.WorkingStateType || s.GetTarget().GetType() == build.FolderType {
		return nil
	}
	viaProject := false
	var independent []string
	for _, g := range m.Grants {
		if g.App == build.ID {
			if strings.HasPrefix(g.By, "project:") {
				viaProject = true
			} else {
				independent = append(independent, g.Role)
			}
		}
	}
	if !viaProject || slices.Contains(independent, build.Builder) || owner.Manifest().Actions.PermitsAny(independent, s.GetSchema().GetName()) {
		return nil
	}
	d, _ := t.app(controlowner.PlatformApp).(platform.ProjectAccess)
	if build.IsResourceCreate(s) {
		var payload struct {
			Project string `json:"project"`
		}
		if d == nil || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Project == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the project that owns this resource")
		}
		if role := d.ProjectRole(m.ID, payload.Project); role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole {
			return nil
		}
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Project roles cover only the resources of projects where you are an owner or editor; {action} on {target} is outside them", s.GetSchema().GetName(), target(s))
	}
	ref, ok := delegatedAsset(s)
	names := []string{ref.Name}
	if name := t.draftName(s); name != "" {
		names = append(names, name)
	}
	if m.Agent || d == nil || !ok || !t.projectEdits(m.ID, d, ref.Kind, names) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Project roles cover only the resources of projects where you are an owner or editor; {action} on {target} is outside them", s.GetSchema().GetName(), target(s))
	}
	return nil
}

var projectRefKinds = map[string][]string{
	"process": {"flow"}, "code": {"compute"}, "table": {"compute"}, "testplan": {"test-plan", "evaluation-set"},
	"alertrule": {"alert"}, "match": {"matching"}, "linktype": {"link-type"}, "propertytype": {"property-type"}, "valuetype": {"value-type"},
	"objecttypegroup": {"object-type-group"},
}

func (t *Tenant) projectEdits(member string, access platform.ProjectAccess, kind string, names []string) bool {
	t.records.Lock()
	covering := []string{}
	if resources := t.records.Type(build.ResourceType); resources != nil {
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
	t.records.Unlock()
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
	t.records.Lock()
	defer t.records.Unlock()
	et := t.records.Type(s.GetTarget().GetType())
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

func delegatedAsset(s *pb.Submission) (platform.ProjectAsset, bool) {
	typ, schema := s.GetTarget().GetType(), s.GetSchema().GetName()
	if !strings.HasPrefix(typ, build.ID+".") || !strings.HasPrefix(schema, typ+".") {
		return platform.ProjectAsset{}, false
	}
	verb := strings.TrimPrefix(schema, typ+".")
	switch verb {
	case "create", "edit", "save", "archive":
	default:
		return platform.ProjectAsset{}, false
	}
	id := s.GetTarget().GetId()
	if id == "" {
		return platform.ProjectAsset{}, false
	}
	var payload struct {
		App string `json:"app"`
	}
	_ = json.Unmarshal(s.GetPayload(), &payload)
	return platform.ProjectAsset{App: payload.App, Kind: strings.TrimPrefix(typ, build.ID+"."), Name: id}, true
}

func holdsIndependentBuildRole(m platform.Member, roles ...string) bool {
	held := false
	for _, g := range m.Grants {
		if g.App != build.ID {
			continue
		}
		held = true
		if !strings.HasPrefix(g.By, "project:") && slices.Contains(roles, g.Role) {
			return true
		}
	}
	return !held && slices.Contains(roles, m.Roles[build.ID])
}

func (t *Tenant) member(id string) (platform.Member, bool) {
	if t.identity == nil {
		return platform.Member{}, false
	}
	member, found := t.identity.MemberByID(id)
	return member.WithAuthorization(t.services.Authorization), found
}
