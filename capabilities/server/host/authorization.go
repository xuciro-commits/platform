package host

import (
	runtimekernel "platformserver/kernel"

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
	access := t.identityOwner
	return security.ProjectDelegation{
		App:     build.ID,
		Builder: build.Builder,
		Exempt: func(s *pb.Submission) bool {
			return build.IsResourceSave(s) || s.GetSchema().GetName() == build.SchemaResourceMove || s.GetTarget().GetType() == build.WorkingStateType || s.GetTarget().GetType() == build.FolderType
		},
		Create:    build.IsResourceCreate,
		Permits:   func(roles []string, action string) bool { return owner.Manifest().Actions.PermitsAny(roles, action) },
		Target:    runtimekernel.Target,
		DraftName: func(s *pb.Submission) string { return build.StoreDraftName(t.Projection, s) },
		Edits: func(member string, access platform.ProjectAccess, kind string, names []string) bool {
			return build.StoreProjectEdits(t.Projection, member, access, kind, names)
		},
	}.Bound(m, s, access)
}

func (t *Tenant) member(id string) (platform.Member, bool) {
	if t.identity == nil {
		return platform.Member{}, false
	}
	member, found := t.identity.MemberByID(id)
	return member.WithAuthorization(t.services.Authorization), found
}
