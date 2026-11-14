package host

import (
	"context"
	"encoding/json"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/merge"
)

type BranchChecks = branches.Checks[trialowner.CandidateSimulation]
type BranchMergeResult = branches.MergeResult[trialowner.CandidateSimulation]
type BranchCheckRequest = branches.CheckRequest[trialowner.CandidateSimulationRequest]
type BranchProposeRequest = branches.ProposeRequest[trialowner.CandidateSimulationRequest]
type BranchMergeRequest = branches.MergeRequest[trialowner.CandidateSimulationRequest]

func (t *Tenant) branchCaller(m platform.Member) platform.Caller {
	return platform.NewCaller(runtime{t: t, scoped: true}, m, build.ID, false, false)
}

func (t *Tenant) branchWorkspace() branches.Workspace {
	return branches.Workspace{Tenant: t.ID, Role: t.ResourceRole, Base: func() string { return t.Projection.BranchBase(t.releases.Active(), build.BranchDraftType) },
		Read: func(m platform.Member, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
			return t.objectOwner(t.Projection).ReadRecordFrom(t.Projection, t.branchCaller(m), typ, id, now)
		},
		List: func(m platform.Member, typ string, q platform.Query, now time.Time) ([]any, *kernel.Error) {
			page, refusal := t.objectOwner(t.Projection).RecordsFrom(t.Projection, m, typ, q, now)
			return page.Records, refusal
		},
		Fields:    func(typ string) ([]platform.FieldInfo, bool) { return records.LockedFields(t.Projection, typ) },
		Resources: func() []build.Resource { return records.LockedRows[build.Resource](t.Projection, build.ResourceType) },
		Project: func(id string) (build.Application, bool) {
			return records.LockedRow[build.Application](t.Projection, build.AppType, id)
		},
		Stored: func(module string) bool { return t.artifacts().Stored(context.Background(), module) },
	}
}

func branchRead[T any](t *Tenant, read func(branches.Workspace) (T, *kernel.Error)) (T, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (T, *kernel.Error) { return read(t.branchWorkspace()) })
}

func (t *Tenant) ReadBranch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	return branchRead(t, func(w branches.Workspace) (build.Branch, *kernel.Error) { return w.Branch(m, id, now) })
}

func (t *Tenant) resourceProject(typ, draft string) string {
	return t.branchWorkspace().ProjectOf(typ, draft)
}

func (t *Tenant) ListBranches(m platform.Member, now time.Time) ([]branches.BranchDetail, *kernel.Error) {
	return branchRead(t, func(w branches.Workspace) ([]branches.BranchDetail, *kernel.Error) { return w.Branches(m, now) })
}

func (t *Tenant) CreateBranch(m platform.Member, request branches.BranchCreateRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return branches.Create(t.branchFlow(), m, request, now)
}

func (t *Tenant) BranchDetail(m platform.Member, id string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return branchRead(t, func(w branches.Workspace) (branches.BranchDetail, *kernel.Error) { return w.DetailOf(m, id, now) })
}

func (t *Tenant) BranchChanges(m platform.Member, id string, specs bool, now time.Time) ([]branches.BranchChangeEntry, *kernel.Error) {
	return branchRead(t, func(w branches.Workspace) ([]branches.BranchChangeEntry, *kernel.Error) {
		return w.Changes(m, id, specs, now)
	})
}

func (t *Tenant) SaveBranchResource(m platform.Member, id, typ, draft string, request branches.BranchSaveRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	plan, refusal := branchRead(t, func(w branches.Workspace) (branches.SavePlan, *kernel.Error) {
		return w.PrepareSave(m, id, typ, draft, request, now)
	})
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.branchFlow().Save(m, plan, typ, draft, now)
}

func (t *Tenant) BranchResource(m platform.Member, id, typ, draft string, now time.Time) (branches.BranchResourceView, *kernel.Error) {
	return branchRead(t, func(w branches.Workspace) (branches.BranchResourceView, *kernel.Error) {
		return w.Resource(m, id, typ, draft, now)
	})
}

func (t *Tenant) BranchDiff(m platform.Member, id string, now time.Time) (branches.BranchDiff, *kernel.Error) {
	return branchRead(t, func(w branches.Workspace) (branches.BranchDiff, *kernel.Error) { return w.BranchDiff(m, id, now) })
}

func (t *Tenant) ReviewBranch(m platform.Member, id string, request branches.BranchReviewRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return t.branchFlow().Review(m, id, request, now)
}

func (t *Tenant) WithdrawBranch(m platform.Member, id string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return t.branchFlow().Finish(m, id, "withdraw", now)
}

func (t *Tenant) CloseBranch(m platform.Member, id string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return t.branchFlow().Finish(m, id, "close", now)
}
