package security

import (
	"encoding/json"
	"platformserver/kernel/protocol"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type ProjectDelegation struct {
	App       string
	Builder   string
	Exempt    func(*pb.Submission) bool
	Create    func(*pb.Submission) bool
	Permits   func(roles []string, action string) bool
	Target    func(*pb.Submission) string
	DraftName func(*pb.Submission) string
	Edits     func(member string, access protocol.ProjectAccess, kind string, names []string) bool
}

func (p ProjectDelegation) Bound(m platform.Member, s *pb.Submission, access protocol.ProjectAccess) *platform.Error {
	if p.Exempt(s) {
		return nil
	}
	viaProject := false
	var independent []string
	for _, g := range m.Grants {
		if g.App == p.App {
			if strings.HasPrefix(g.By, "project:") {
				viaProject = true
			} else {
				independent = append(independent, g.Role)
			}
		}
	}
	if !viaProject || slices.Contains(independent, p.Builder) || p.Permits(independent, s.GetSchema().GetName()) {
		return nil
	}
	outside := func() *platform.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Project roles cover only the resources of projects where you are an owner or editor; {action} on {target} is outside them", s.GetSchema().GetName(), p.Target(s))
	}
	if p.Create(s) {
		var payload struct {
			Project string `json:"project"`
		}
		if access == nil || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Project == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the project that owns this resource")
		}
		if role := access.ProjectRole(m.ID, payload.Project); role == protocol.ProjectOwnerRole || role == protocol.ProjectEditorRole {
			return nil
		}
		return outside()
	}
	ref, ok := p.asset(s)
	names := []string{ref.Name}
	if name := p.DraftName(s); name != "" {
		names = append(names, name)
	}
	if m.Agent || access == nil || !ok || !p.Edits(m.ID, access, ref.Kind, names) {
		return outside()
	}
	return nil
}

func (p ProjectDelegation) asset(s *pb.Submission) (protocol.ProjectAsset, bool) {
	typ, schema := s.GetTarget().GetType(), s.GetSchema().GetName()
	if !strings.HasPrefix(typ, p.App+".") || !strings.HasPrefix(schema, typ+".") {
		return protocol.ProjectAsset{}, false
	}
	switch strings.TrimPrefix(schema, typ+".") {
	case "create", "edit", "save", "archive":
	default:
		return protocol.ProjectAsset{}, false
	}
	id := s.GetTarget().GetId()
	if id == "" {
		return protocol.ProjectAsset{}, false
	}
	var payload struct {
		App string `json:"app"`
	}
	_ = json.Unmarshal(s.GetPayload(), &payload)
	return protocol.ProjectAsset{App: payload.App, Kind: strings.TrimPrefix(typ, p.App+"."), Name: id}, true
}
