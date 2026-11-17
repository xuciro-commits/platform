package build

import (
	"platformserver/platform"
	branchowner "platformserver/services/resources/merge/owner"
)

type branchEnvironment struct {
	b *Build
}

func (e branchEnvironment) AppID() string { return ID }
func (e branchEnvironment) Member(id string) (platform.Member, bool) {
	return e.b.host.Member(id)
}
func (e branchEnvironment) ProjectApprovals(project string) int {
	return e.b.host.ProjectApprovals(project)
}
func (e branchEnvironment) Resources(c platform.Caller) ([]branchowner.ResourceInfo, error) {
	resources, err := e.b.ResourceInventory(c)
	if err != nil {
		return nil, err
	}
	out := make([]branchowner.ResourceInfo, 0, len(resources))
	for _, resource := range resources {
		out = append(out, branchowner.ResourceInfo{Type: resource.Type, Draft: resource.Draft, Project: resource.Project})
	}
	return out, nil
}
func (e branchEnvironment) DraftType(typ string) bool { return BranchDraftType(typ) }
func (e branchEnvironment) CanEdit(c platform.Caller, member platform.Member, project string) bool {
	return canEditProject(e.b.host.Caller(c, member, ID), project)
}

var _ branchowner.Environment = branchEnvironment{}
