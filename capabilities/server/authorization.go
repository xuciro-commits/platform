package platformserver

import (
	"encoding/json"
	controlowner "platformserver/kernel/control"

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
	if ok {
		if name := t.draftName(s); name != "" && name != ref.Name && d != nil && d.EditsProjectAsset(m.ID, platform.ProjectAsset{App: ref.App, Kind: ref.Kind, Name: name}) {
			ref.Name = name
		}
	}
	if m.Agent || d == nil || !ok || !d.EditsProjectAsset(m.ID, ref) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Project delegation covers only the draft edits of the assets your projects name; {action} on {target} is outside it", s.GetSchema().GetName(), target(s))
	}
	return nil
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
