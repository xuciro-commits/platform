package host

import (
	"context"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/merge"
)

func (t *Tenant) BranchCompile(m platform.Member, id, typ, draft string, now time.Time) (merge.BranchDetail, *platform.Error) {
	return merge.CompileHooks{
		Read: t.ReadBranch,
		Main: func(m platform.Member, typ, draft string, now time.Time) (merge.MainDraft, *platform.Error) {
			return branchRead(t, func(w merge.Workspace) (merge.MainDraft, *platform.Error) { return w.Main(m, typ, draft, now) })
		},
		Project: func(m platform.Member, typ, draft string, now time.Time) (merge.MainDraft, string, *platform.Error) {
			type basis struct {
				draft   merge.MainDraft
				project string
			}
			base, refusal := recordRead(t, context.Background(), func(t *Tenant) (basis, *platform.Error) {
				current, refusal := t.branchWorkspace().Main(m, typ, draft, now)
				return basis{draft: current, project: t.resourceProject(typ, draft)}, refusal
			})
			return base.draft, base.project, refusal
		},
		Build: t.artifacts().Build,
		Append: func(m platform.Member, plan merge.SavePlan, typ, draft string, now time.Time) (merge.BranchDetail, *platform.Error) {
			return t.branchFlow().Save(m, plan, typ, draft, now)
		},
	}.Compile(m, id, typ, draft, now)
}
