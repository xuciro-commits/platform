package host

import (
	"context"
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	ontology "platformserver/services/ontology"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/merge"
)

type BranchChecks struct {
	At         time.Time                       `json:"at"`
	Conflicts  []branches.BranchResourceDiff   `json:"conflicts"`
	Resources  []branches.BranchResourceCheck  `json:"resources"`
	Approved   bool                            `json:"approved"`
	Approval   string                          `json:"approval,omitempty"`
	Ready      bool                            `json:"ready"`
	Check      branches.BranchCheck            `json:"check"`
	Simulation *trialowner.CandidateSimulation `json:"simulation,omitempty"`
}

type BranchMergeResult struct {
	Branch         branches.BranchDetail           `json:"branch"`
	Candidate      string                          `json:"candidate,omitempty"`
	CandidateError string                          `json:"candidateError,omitempty"`
	Applied        []branches.BranchResourceCheck  `json:"applied"`
	Simulated      *trialowner.CandidateSimulation `json:"simulation,omitempty"`
}

type BranchCheckRequest struct {
	Resolutions []branches.BranchResolution            `json:"resolutions,omitempty"`
	Simulation  *trialowner.CandidateSimulationRequest `json:"simulation,omitempty"`
}

type BranchProposeRequest struct {
	Reviewers   []string                               `json:"reviewers,omitempty"`
	Note        string                                 `json:"note,omitempty"`
	Resolutions []branches.BranchResolution            `json:"resolutions,omitempty"`
	Simulation  *trialowner.CandidateSimulationRequest `json:"simulation,omitempty"`
}

type BranchMergeRequest struct {
	Resolutions []branches.BranchResolution            `json:"resolutions,omitempty"`
	Simulation  *trialowner.CandidateSimulationRequest `json:"simulation,omitempty"`
}

func (t *Tenant) branchCaller(m platform.Member) platform.Caller {
	return platform.NewCaller(runtime{t: t, scoped: true}, m, build.ID, false, false)
}

func (t *Tenant) branchWorkspace() branches.Workspace {
	return branches.Workspace{Tenant: t.ID, Role: t.ResourceRole, Base: func() string { return t.Projection.BranchBase(t.releases.Active(), build.BranchDraftType) },
		Read: func(m platform.Member, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
			return t.readRecordFrom(t.Projection, t.branchCaller(m), typ, id, now)
		},
		List: func(m platform.Member, typ string, q platform.Query, now time.Time) ([]any, *kernel.Error) {
			page, refusal := t.recordsFrom(t.Projection, m, typ, q, now)
			return page.Records, refusal
		},
		Fields:    func(typ string) ([]platform.FieldInfo, bool) { return records.LockedFields(t.Projection, typ) },
		Resources: func() []build.Resource { return records.LockedRows[build.Resource](t.Projection, build.ResourceType) },
		Project: func(id string) (build.Application, bool) {
			return records.LockedRow[build.Application](t.Projection, build.AppType, id)
		},
		Stored: func(module string) bool { return t.artifactStored(context.Background(), module) },
	}
}

func (t *Tenant) readBranch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (build.Branch, *kernel.Error) { return t.ReadBranch(m, id, now) })
}

func (t *Tenant) ReadBranch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	return t.branchWorkspace().Branch(m, id, now)
}

func (t *Tenant) resourceProject(typ, draft string) string {
	return t.branchWorkspace().ProjectOf(typ, draft)
}

func (t *Tenant) mainDraftOf(m platform.Member, typ, id string, now time.Time) (branches.MainDraft, *kernel.Error) {
	return t.branchWorkspace().Main(m, typ, id, now)
}

func (t *Tenant) ListBranches(m platform.Member, now time.Time) ([]branches.BranchDetail, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]branches.BranchDetail, *kernel.Error) {
		return t.branchWorkspace().Branches(m, now)
	})
}

func (t *Tenant) submitBranch(m platform.Member, key, schema, id string, payload any, now time.Time) *kernel.Error {
	_, refusal := t.Submit(m, branches.Submission(t.ID, m, key, schema, id, payload), now)
	return refusal
}

func (t *Tenant) CreateBranch(m platform.Member, request branches.BranchCreateRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	if !ontology.Named(request.Name) {
		return branches.BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A branch name is lower-case letters and digits")
	}
	if refusal := t.submitBranch(m, "branch:"+request.Name+":create", build.BranchType+".create", request.Name, map[string]any{"name": request.Name, "title": request.Title, "description": request.Description}, now); refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.BranchDetail(m, request.Name, now)
}

func (t *Tenant) BranchDetail(m platform.Member, id string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchDetail, *kernel.Error) {
		return t.branchWorkspace().DetailOf(m, id, now)
	})
}

func (t *Tenant) BranchChanges(m platform.Member, id string, specs bool, now time.Time) ([]branches.BranchChangeEntry, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]branches.BranchChangeEntry, *kernel.Error) {
		return t.branchWorkspace().Changes(m, id, specs, now)
	})
}

func (t *Tenant) SaveBranchResource(m platform.Member, id, typ, draft string, request branches.BranchSaveRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	plan, refusal := recordRead(t, context.Background(), func(t *Tenant) (branches.SavePlan, *kernel.Error) {
		return t.branchWorkspace().PrepareSave(m, id, typ, draft, request, now)
	})
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.appendBranchSave(m, plan, typ, draft, now)
}

func (t *Tenant) appendBranchSave(m platform.Member, plan branches.SavePlan, typ, draft string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	s := plan.Save(t.ID, m, typ, draft)
	if s == nil {
		return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchDetail, *kernel.Error) {
			return t.branchWorkspace().Detail(m, plan.Branch, now)
		})
	}
	if _, refusal := t.Submit(m, s, now); refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.BranchDetail(m, plan.Branch.ID, now)
}

func (t *Tenant) BranchResource(m platform.Member, id, typ, draft string, now time.Time) (branches.BranchResourceView, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchResourceView, *kernel.Error) {
		return t.branchWorkspace().Resource(m, id, typ, draft, now)
	})
}

func (t *Tenant) planBranch(m platform.Member, branch build.Branch, resolutions []branches.BranchResolution, now time.Time) ([]branches.ResourcePlan, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]branches.ResourcePlan, *kernel.Error) {
		return t.branchWorkspace().Plan(m, branch, resolutions, now)
	})
}

func (t *Tenant) BranchDiff(m platform.Member, id string, now time.Time) (branches.BranchDiff, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchDiff, *kernel.Error) {
		return t.branchWorkspace().BranchDiff(m, id, now)
	})
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
