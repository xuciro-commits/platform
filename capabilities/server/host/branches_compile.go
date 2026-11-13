package host

import (
	"context"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/merge"
)

func (t *Tenant) BranchCompile(m platform.Member, id, typ, draft string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return branches.CompileHooks{
		Read: t.readBranch,
		Main: func(m platform.Member, typ, draft string, now time.Time) (branches.MainDraft, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (branches.MainDraft, *kernel.Error) { return t.mainDraftOf(m, typ, draft, now) })
		},
		Project: func(m platform.Member, typ, draft string, now time.Time) (branches.MainDraft, string, *kernel.Error) {
			type basis struct {
				draft   branches.MainDraft
				project string
			}
			base, refusal := recordRead(t, context.Background(), func(t *Tenant) (basis, *kernel.Error) {
				current, refusal := t.mainDraftOf(m, typ, draft, now)
				return basis{draft: current, project: t.resourceProject(typ, draft)}, refusal
			})
			return base.draft, base.project, refusal
		},
		Build:  t.buildCode,
		Append: t.appendBranchSave,
	}.Compile(m, id, typ, draft, now)
}
