package branches

import (
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

type CheckRequest[S any] struct {
	Resolutions []BranchResolution `json:"resolutions,omitempty"`
	Simulation  *S                 `json:"simulation,omitempty"`
}

type ProposeRequest[S any] struct {
	Reviewers   []string           `json:"reviewers,omitempty"`
	Note        string             `json:"note,omitempty"`
	Resolutions []BranchResolution `json:"resolutions,omitempty"`
	Simulation  *S                 `json:"simulation,omitempty"`
}

type MergeRequest[S any] struct {
	Resolutions []BranchResolution `json:"resolutions,omitempty"`
	Simulation  *S                 `json:"simulation,omitempty"`
}

type Checks[R any] struct {
	At         time.Time             `json:"at"`
	Conflicts  []BranchResourceDiff  `json:"conflicts"`
	Resources  []BranchResourceCheck `json:"resources"`
	Approved   bool                  `json:"approved"`
	Approval   string                `json:"approval,omitempty"`
	Ready      bool                  `json:"ready"`
	Check      BranchCheck           `json:"check"`
	Simulation *R                    `json:"simulation,omitempty"`
}

type MergeResult[R any] struct {
	Branch         BranchDetail          `json:"branch"`
	Candidate      string                `json:"candidate,omitempty"`
	CandidateError string                `json:"candidateError,omitempty"`
	Applied        []BranchResourceCheck `json:"applied"`
	Simulation     *R                    `json:"simulation,omitempty"`
}

func Create(f Flow, m platform.Member, request BranchCreateRequest, now time.Time) (BranchDetail, *kernel.Error) {
	if !ontology.Named(request.Name) {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A branch name is lower-case letters and digits")
	}
	if refusal := f.Submit(m, Submission(f.Tenant, m, "branch:"+request.Name+":create", build.BranchType+".create", request.Name,
		map[string]any{"name": request.Name, "title": request.Title, "description": request.Description}), now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return f.Detail(m, request.Name, now)
}

func StagedWorld[D, T any](w Workspace, m platform.Member, id string, now time.Time,
	stage func(WorldPlan) Stage[D], view func(Stage[D]) (T, error)) (T, *kernel.Error) {
	var none T
	plan, refusal := w.WorldPlan(m, id, now)
	if refusal != nil {
		return none, refusal
	}
	merged := stage(plan)
	if i := slices.IndexFunc(merged.Resources, func(c BranchResourceCheck) bool { return !c.Accepted }); i >= 0 {
		return none, Refuse("{resource} would be refused in the merged world: {message}", merged.Resources[i].Type+"/"+merged.Resources[i].ID, merged.Resources[i].Message)
	}
	value, err := view(merged)
	if err != nil {
		return none, Refuse("{reason}", err.Error())
	}
	return value, nil
}

func ReadChecks[S, R any](read func(platform.Member, string, time.Time) (build.Branch, *kernel.Error),
	evaluate func(platform.Member, build.Branch, []BranchResolution, *S, time.Time) (Evaluation[R], *kernel.Error),
	m platform.Member, id string, request CheckRequest[S], now time.Time) (Checks[R], *kernel.Error) {
	branch, refusal := read(m, id, now)
	if refusal != nil {
		return Checks[R]{}, refusal
	}
	evaluation, refusal := evaluate(m, branch, request.Resolutions, request.Simulation, now)
	if refusal != nil {
		return Checks[R]{}, refusal
	}
	out := Checks[R]{At: now.UTC(), Conflicts: evaluation.Conflicts, Resources: evaluation.Check.Resources, Check: evaluation.Check,
		Simulation: evaluation.Simulation, Ready: evaluation.Check.Passed}
	if err := branch.Approved(); err != nil {
		out.Approval = err.Error()
	} else {
		out.Approved = true
	}
	return out, nil
}

func Recheck[S, R any](flow Flow, evaluate func(platform.Member, build.Branch, []BranchResolution, *S, time.Time) (Evaluation[R], *kernel.Error),
	m platform.Member, id string, request CheckRequest[S], now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := flow.Read(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen && branch.State != build.BranchProposed || len(branch.Changes) == 0 {
		return BranchDetail{}, Refuse("Only an open or proposed branch with changes can be re-checked")
	}
	evaluation, refusal := evaluate(m, branch, request.Resolutions, request.Simulation, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if refusal := flow.RecordCheck(m, branch, evaluation.Check, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return flow.Detail(m, id, now)
}

func Propose[S, R any](flow Flow, evaluate func(platform.Member, build.Branch, []BranchResolution, *S, time.Time) (Evaluation[R], *kernel.Error),
	m platform.Member, id string, request ProposeRequest[S], now time.Time) (BranchDetail, *kernel.Error) {
	return flow.Propose(m, id, request.Reviewers, request.Note, func(branch build.Branch) (BranchCheck, []BranchResourceDiff, *kernel.Error) {
		evaluation, refusal := evaluate(m, branch, request.Resolutions, request.Simulation, now)
		return evaluation.Check, evaluation.Conflicts, refusal
	}, now)
}

func MergeBranch[R any](m platform.Member, id string, resolutions []BranchResolution, simulate bool, now time.Time,
	apply func(platform.Member, string, []BranchResolution, bool, time.Time) (MergePlan, *kernel.Error),
	save func(platform.Member, string, string, []Action, time.Time) (string, error),
	detail func(platform.Member, string, time.Time) (BranchDetail, *kernel.Error)) (MergeResult[R], *kernel.Error) {
	var out MergeResult[R]
	plan, refusal := apply(m, id, resolutions, simulate, now)
	if refusal != nil {
		return out, refusal
	}
	out.Applied = plan.Applied
	if candidate := plan.Check.Graph.Candidate; candidate != "" {
		if saved, err := save(m, id, candidate, plan.Actions, now); err != nil {
			out.CandidateError = err.Error()
		} else {
			out.Candidate = saved
		}
	}
	out.Branch, refusal = detail(m, id, now)
	return out, refusal
}
