package host

import (
	"context"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/merge"
)

func (t *Tenant) BranchCompile(m platform.Member, id, typ, draft string, now time.Time) (branches.BranchDetail, *platform.Error) {
	return branches.CompileHooks{
		Read: t.ReadBranch,
		Main: func(m platform.Member, typ, draft string, now time.Time) (branches.MainDraft, *platform.Error) {
			return branchRead(t, func(w branches.Workspace) (branches.MainDraft, *platform.Error) { return w.Main(m, typ, draft, now) })
		},
		Project: func(m platform.Member, typ, draft string, now time.Time) (branches.MainDraft, string, *platform.Error) {
			type basis struct {
				draft   branches.MainDraft
				project string
			}
			base, refusal := recordRead(t, context.Background(), func(t *Tenant) (basis, *platform.Error) {
				current, refusal := t.branchWorkspace().Main(m, typ, draft, now)
				return basis{draft: current, project: t.resourceProject(typ, draft)}, refusal
			})
			return base.draft, base.project, refusal
		},
		Build: t.artifacts().Build,
		Append: func(m platform.Member, plan branches.SavePlan, typ, draft string, now time.Time) (branches.BranchDetail, *platform.Error) {
			return t.branchFlow().Save(m, plan, typ, draft, now)
		},
	}.Compile(m, id, typ, draft, now)
}
