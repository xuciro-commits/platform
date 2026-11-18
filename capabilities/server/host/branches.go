package host

import (
	"context"
	"encoding/json"
	"platformserver/kernel/protocol"
	"time"

	"platformserver/kernel/records"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/merge"
)

type BranchChecks = merge.Checks[trialowner.CandidateSimulation]
type BranchMergeResult = merge.MergeResult[trialowner.CandidateSimulation]
type BranchCheckRequest = merge.CheckRequest[trialowner.CandidateSimulationRequest]
type BranchProposeRequest = merge.ProposeRequest[trialowner.CandidateSimulationRequest]
type BranchMergeRequest = merge.MergeRequest[trialowner.CandidateSimulationRequest]

func (t *Tenant) branchCaller(m platform.Member) platform.Caller {
	return platform.NewCaller(runtime{t: t, scoped: true}, m, build.ID, false, false)
}

func (t *Tenant) branchWorkspace() merge.Workspace {
	return merge.Workspace{Tenant: t.ID, Role: t.ResourceRole, Base: func() string { return t.Projection.BranchBase(t.releases.Active(), build.BranchDraftType) },
		Read: func(m platform.Member, typ, id string, now time.Time) (json.RawMessage, *platform.Error) {
			return t.objectOwner(t.Projection).ReadRecordFrom(t.Projection, t.branchCaller(m), typ, id, now)
		},
		List: func(m platform.Member, typ string, q platform.Query, now time.Time) ([]any, *platform.Error) {
			page, refusal := t.objectOwner(t.Projection).RecordsFrom(t.Projection, m, typ, q, now)
			return page.Records, refusal
		},
		Fields: func(typ string) ([]platform.FieldInfo, bool) { return records.LockedFields(t.Projection, typ) },
		Resources: func() []protocol.Resource {
			return records.LockedRows[protocol.Resource](t.Projection, build.ResourceType)
		},
		Project: func(id string) (protocol.Application, bool) {
			return records.LockedRow[protocol.Application](t.Projection, protocol.AppType, id)
		},
		Stored: func(module string) bool { return t.artifacts().Stored(context.Background(), module) },
	}
}

func branchRead[T any](t *Tenant, read func(merge.Workspace) (T, *platform.Error)) (T, *platform.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (T, *platform.Error) { return read(t.branchWorkspace()) })
}

func (t *Tenant) ReadBranch(m platform.Member, id string, now time.Time) (build.Branch, *platform.Error) {
	return branchRead(t, func(w merge.Workspace) (build.Branch, *platform.Error) { return w.Branch(m, id, now) })
}

func (t *Tenant) resourceProject(typ, draft string) string {
	return t.branchWorkspace().ProjectOf(typ, draft)
}

func (t *Tenant) ListBranches(m platform.Member, now time.Time) ([]merge.BranchDetail, *platform.Error) {
	return branchRead(t, func(w merge.Workspace) ([]merge.BranchDetail, *platform.Error) { return w.Branches(m, now) })
}

func (t *Tenant) CreateBranch(m platform.Member, request merge.BranchCreateRequest, now time.Time) (merge.BranchDetail, *platform.Error) {
	return merge.Create(t.branchFlow(), m, request, now)
}

func (t *Tenant) BranchDetail(m platform.Member, id string, now time.Time) (merge.BranchDetail, *platform.Error) {
	return branchRead(t, func(w merge.Workspace) (merge.BranchDetail, *platform.Error) { return w.DetailOf(m, id, now) })
}

func (t *Tenant) BranchChanges(m platform.Member, id string, specs bool, now time.Time) ([]merge.BranchChangeEntry, *platform.Error) {
	return branchRead(t, func(w merge.Workspace) ([]merge.BranchChangeEntry, *platform.Error) {
		return w.Changes(m, id, specs, now)
	})
}

func (t *Tenant) SaveBranchResource(m platform.Member, id, typ, draft string, request merge.BranchSaveRequest, now time.Time) (merge.BranchDetail, *platform.Error) {
	plan, refusal := branchRead(t, func(w merge.Workspace) (merge.SavePlan, *platform.Error) {
		return w.PrepareSave(m, id, typ, draft, request, now)
	})
	if refusal != nil {
		return merge.BranchDetail{}, refusal
	}
	return t.branchFlow().Save(m, plan, typ, draft, now)
}

func (t *Tenant) BranchResource(m platform.Member, id, typ, draft string, now time.Time) (merge.BranchResourceView, *platform.Error) {
	return branchRead(t, func(w merge.Workspace) (merge.BranchResourceView, *platform.Error) {
		return w.Resource(m, id, typ, draft, now)
	})
}

func (t *Tenant) BranchDiff(m platform.Member, id string, now time.Time) (merge.BranchDiff, *platform.Error) {
	return branchRead(t, func(w merge.Workspace) (merge.BranchDiff, *platform.Error) { return w.BranchDiff(m, id, now) })
}

func (t *Tenant) ReviewBranch(m platform.Member, id string, request merge.BranchReviewRequest, now time.Time) (merge.BranchDetail, *platform.Error) {
	return t.branchFlow().Review(m, id, request, now)
}

func (t *Tenant) WithdrawBranch(m platform.Member, id string, now time.Time) (merge.BranchDetail, *platform.Error) {
	return t.branchFlow().Finish(m, id, "withdraw", now)
}

func (t *Tenant) CloseBranch(m platform.Member, id string, now time.Time) (merge.BranchDetail, *platform.Error) {
	return t.branchFlow().Finish(m, id, "close", now)
}
