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

// delegatedBound refuses what a member whose builder role comes only from
// projects may not do in the build app: anything but a draft edit of an asset
// one of their projects names. A member who holds the builder role in their
// own right (directly, by team or delegation) is not bounded. Agents hold no
// project delegation.
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
	if !viaProject || slices.Contains(independent, build.Builder) || owner.Manifest().Actions.PermitsAny(independent, s.GetSchema().GetName()) { // preserve only actions held independently
		return nil
	}
	d, _ := t.app(controlowner.PlatformApp).(platform.ProjectAccess)
	ref, ok := delegatedAsset(s)
	if ok {
		// Projects name assets the way candidates do, by the draft's name;
		// the submission targets the draft's record ID (UX-11). Either form
		// in the project covers the edit.
		if name := t.draftName(s); name != "" && name != ref.Name && d != nil && d.EditsProjectAsset(m.ID, platform.ProjectAsset{App: ref.App, Kind: ref.Kind, Name: name}) {
			ref.Name = name
		}
	}
	if m.Agent || d == nil || !ok || !d.EditsProjectAsset(m.ID, ref) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Project delegation covers only the draft edits of the assets your projects name; {action} on {target} is outside it", s.GetSchema().GetName(), target(s))
	}
	return nil
}

// draftName is the name of the builder draft a submission edits: the payload's
// on a create, the record's otherwise; empty when neither says.
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

// delegatedAsset maps a draft-edit submission to the asset it edits. Only the
// create/edit/save/archive schemas of the build app's asset types qualify; a
// publish or release schema keeps the app-wide role requirement.
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

// holdsIndependentBuildRole excludes the builder derived from a project when
// authorizing app-wide release work. Old recorded seats without grants keep
// their explicit Roles value.
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

// member is a member of the tenant by ID, with its current roles.
func (t *Tenant) member(id string) (platform.Member, bool) {
	if t.identity == nil {
		return platform.Member{}, false
	}
	member, found := t.identity.MemberByID(id)
	return member.WithAuthorization(t.services.Authorization), found
}
