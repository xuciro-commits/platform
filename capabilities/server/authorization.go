package platformserver

import (
	"encoding/json"
	controlowner "platformserver/kernel/control"
	"reflect"

	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

func (t *Tenant) delegatedBound(m platform.Member, s *pb.Submission) *kernel.Error {
	owner := t.owner["action:"+s.GetSchema().GetName()]
	if owner == nil || owner.Manifest().ID != build.ID {
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
}

func projectCovers(app build.Application, kind string, names []string) bool {
	refKinds := append([]string{kind}, projectRefKinds[kind]...)
	for _, name := range names {
		switch {
		case kind == "app" && (name == app.Name || name == app.ID):
			return true
		case kind == "page" && slices.Contains(app.Pages, name):
			return true
		}
		for _, ref := range app.Resources {
			if slices.Contains(refKinds, string(ref.Kind)) && (ref.Name == name || ref.Name == build.ID+"."+name) {
				return true
			}
		}
	}
	return false
}

func (t *Tenant) projectEdits(member string, d platform.ProjectAccess, kind string, names []string) bool {
	t.records.Lock()
	et := t.records.Type(build.AppType)
	var covering []string
	if et != nil {
		for id, row := range et.Rows().All() {
			value := row.Value
			if value.Kind() == reflect.Pointer {
				value = value.Elem()
			}
			app, ok := value.Interface().(build.Application)
			if ok && !app.Archived && projectCovers(app, kind, names) {
				covering = append(covering, id)
			}
		}
	}
	t.records.Unlock()
	for _, id := range covering {
		if role := d.ProjectRole(member, id); role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole {
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
