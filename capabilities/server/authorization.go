package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Authorization is the platform's own, per-tenant separation of duties
// (ADR-0047 §11, ordered by the owner on 2026-10-05). Three things live here:
//
//   - the auditor role: a member of the platform app who may read the tenant's
//     audit, health, work, members, settings and catalog and may decide
//     nothing, so an audit right is not faked with a wider administrator role;
//   - the publisher role (declared by the build app): a member who may save and
//     activate release candidates and apply code publications without holding
//     the builder role that edits definitions;
//   - projects: a tenant-level organisational association that also carries
//     asset edit delegation. A project names members and the assets assigned to
//     it; an editor of a live project holds the builder role through it (a
//     derived grant, `By: project:<id>`, ADR-0078 §3.3), so the Studio, the
//     reads and the action catalogue open for them like for any builder. The
//     bound is enforced on the submission path from each submission's target:
//     a builder role held only through projects may draft the named assets and
//     nothing else, so a direct API call is checked exactly like the
//     professional editor and a filtered list never masquerades as isolation.
//     Publishing and activation stay with the app-wide builder or publisher.
//
// None of these change what a definition edit or an activation means: they only
// decide who may ask for one.

// Auditor reads the tenant's administration surface and decides nothing.
const Auditor = "auditor"

// Asset edit delegation (build projects).
const (
	ProjectType          = "platform.project"
	SchemaProjectSave    = ProjectType + ".save"
	SchemaProjectArchive = ProjectType + ".archive"
	// ProjectEditorRole is the part a member holds inside a project.
	ProjectEditorRole = "editor"
)

// ProjectEditor is one member's part in a project.
type ProjectMember struct {
	Member string `json:"member" field:"required" title:"Member" help:"A member's ID in this tenant"`
	Role   string `json:"role,omitempty" field:"required,choice:editor" title:"Part" help:"editor: may edit the assets this project names"`
}

// ProjectAsset is one asset a project covers.
type ProjectAsset struct {
	App  string `json:"app,omitempty" title:"App" help:"The app the asset belongs to; empty: any"`
	Kind string `json:"kind" field:"required,choice:object,page,query,function,process,code,propertytype,linktype" title:"Kind"`
	Name string `json:"name" field:"required" title:"Name" help:"The asset's name inside its owner app"`
}

// Project is a tenant's project: a named association of members with asset
// edit delegation. It is organisational, tenant-level, and never a second
// security boundary around records.
type BuildProject struct {
	platform.Record
	Name    string          `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"hotel-opening"`
	Title   string          `json:"title" field:"required,search" title:"What people call it" example:"Hotel opening"`
	Members []ProjectMember `json:"members,omitempty" title:"Members"`
	Assets  []ProjectAsset  `json:"assets,omitempty" title:"Assets"`
}

func projectEntity() platform.Entity {
	return platform.Entity{Type: ProjectType, Title: "Project", Plural: "Projects", Model: BuildProject{}, Display: "title",
		Description: "A named association of members that carries edit delegation for the assets it names.",
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true}}
}

// ProjectActions are the decisions an administrator makes about projects.
func ProjectActions() []platform.Action {
	admin := []string{Admin}
	return []platform.Action{
		{Schema: SchemaProjectSave, Target: ProjectType, Capability: "projects", Title: "Save project",
			Description: "Create or change a project and the members and assets it covers.",
			Payload: []platform.Field{
				{Name: "name", Type: "string", Required: true},
				{Name: "title", Type: "string", Required: true},
				{Name: "members", Type: "json", Description: "member IDs with their part in the project"},
				{Name: "assets", Type: "json", Description: "app, kind and name of every asset the project covers"},
			}, Roles: admin},
		{Schema: SchemaProjectArchive, Target: ProjectType, Capability: "projects", Title: "Archive project",
			Description: "Archive a project: its members stop editing its assets; history stays.", Payload: []platform.Field{}, Roles: admin},
	}
}

// decideProject applies the directory's project decisions. It follows
// decideMember: a change returns a closure the ledger runs against the record.
func (d *Console) decideProject(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	id := s.GetTarget().GetId()
	if s.GetSchema().GetName() == SchemaProjectArchive {
		p := d.projects[id]
		if p == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) { p.Archived = true }, nil
	}
	var p struct {
		Name, Title string
		Members     []ProjectMember `json:"members"`
		Assets      []ProjectAsset  `json:"assets"`
	}
	if id == "" || json.Unmarshal(s.GetPayload(), &p) != nil || p.Name == "" || p.Title == "" {
		return nil, invalid
	}
	if p.Name != strings.ToLower(p.Name) {
		return nil, invalid
	}
	for _, e := range p.Members {
		if e.Member == "" || e.Role == "" || e.Role != ProjectEditorRole {
			return nil, invalid
		}
	}
	for _, a := range p.Assets {
		if a.Kind == "" || a.Name == "" {
			return nil, invalid
		}
	}
	if d.t == nil {
		return nil, invalid
	}
	for _, e := range p.Members { // only members of this tenant may be named
		if _, ok := d.t.Member(e.Member); !ok {
			return nil, invalid
		}
	}
	return func(*pb.ChangeRecord) {
		p := &BuildProject{Name: p.Name, Title: p.Title, Members: p.Members, Assets: p.Assets}
		p.ID, p.Changed = id, platform.Stamp{At: time.Now().UTC()}
		if prior := d.projects[id]; prior != nil {
			p.Created, p.Revision = prior.Created, prior.Revision+1
		}
		d.projects[id] = p
	}, nil
}

// ProjectViews is the tenant's projects as the console reads them.
func (d *Console) ProjectViews() []BuildProject {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []BuildProject{}
	for _, p := range d.projects {
		out = append(out, *p)
	}
	return out
}

// edits reports whether the member is an editor of a project that covers the
// asset. An archived project delegates nothing.
func (d *Console) edits(member string, ref ProjectAsset) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range d.projects {
		if p.Archived || !p.covers(ref) {
			continue
		}
		for _, e := range p.Members {
			if e.Member == member && e.Role == ProjectEditorRole {
				return true
			}
		}
	}
	return false
}

func (p BuildProject) covers(ref ProjectAsset) bool {
	for _, a := range p.Assets {
		if a.Kind == ref.Kind && a.Name == ref.Name && (a.App == "" || ref.App == "" || a.App == ref.App) {
			return true
		}
	}
	return false
}

// projectGrants are the builder grants a member holds through projects
// (WorkQueue #141, ADR-0078 §3.3): editor of a live project → builder in the
// build app, `By: project:<id>`. The grant is real for reading, loading the
// Studio and the action catalogue; delegatedBound keeps its writes to the
// assets the projects name. Call with d.mu held.
func (d *Console) projectGrantsLocked(member string) []platform.Grant {
	var out []platform.Grant
	if m := d.members[member]; m != nil && m.Agent {
		return out
	}
	for _, id := range slices.Sorted(maps.Keys(d.projects)) {
		p := d.projects[id]
		if p.Archived {
			continue
		}
		for _, e := range p.Members {
			if e.Member == member && e.Role == ProjectEditorRole {
				out = append(out, platform.Grant{App: build.ID, Role: build.Builder, By: "project:" + id, Reason: p.Title})
				break
			}
		}
	}
	return out
}

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
	d := consoleOf(t)
	ref, ok := delegatedAsset(s)
	if m.Agent || d == nil || !ok || !d.edits(m.ID, ref) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Project delegation covers only the draft edits of the assets your projects name; {action} on {target} is outside it", s.GetSchema().GetName(), target(s))
	}
	return nil
}

// delegatedAsset maps a draft-edit submission to the asset it edits. Only the
// create/edit/save/archive schemas of the build app's asset types qualify; a
// publish or release schema keeps the app-wide role requirement.
func delegatedAsset(s *pb.Submission) (ProjectAsset, bool) {
	typ, schema := s.GetTarget().GetType(), s.GetSchema().GetName()
	if !strings.HasPrefix(typ, build.ID+".") || !strings.HasPrefix(schema, typ+".") {
		return ProjectAsset{}, false
	}
	verb := strings.TrimPrefix(schema, typ+".")
	switch verb {
	case "create", "edit", "save", "archive":
	default:
		return ProjectAsset{}, false
	}
	id := s.GetTarget().GetId()
	if id == "" {
		return ProjectAsset{}, false
	}
	var payload struct {
		App string `json:"app"`
	}
	_ = json.Unmarshal(s.GetPayload(), &payload)
	return ProjectAsset{App: payload.App, Kind: strings.TrimPrefix(typ, build.ID+"."), Name: id}, true
}

// Auditor read surface: the administrator reads an auditor may use.
var auditorReads = []string{"audit", "deliveries", "work", "connectors", "settings",
	"endpoints", "effects", "health", "personal-reads", "permissions", "access", "members", "projects", "packages", "notifications"}

func auditorMayRead(name string) bool {
	for _, r := range auditorReads {
		if r == name {
			return true
		}
	}
	return false
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
