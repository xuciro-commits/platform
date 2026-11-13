package branches

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

type StageChild struct {
	Receipt *pb.ChangeRecord
	Issues  []platform.FieldIssue
	Refusal *kernel.Error
}

type Stage[D any] struct {
	Draft     D
	Resources []BranchResourceCheck
	Receipt   *pb.ChangeRecord
}

func (s Stage[D]) Accepted() bool {
	return !slices.ContainsFunc(s.Resources, func(c BranchResourceCheck) bool { return !c.Accepted })
}

func StageActions[D any](newDraft func() D, decide func(D, Action, string) StageChild, branchID string, actions []Action) Stage[D] {
	stage := Stage[D]{Resources: make([]BranchResourceCheck, 0, len(actions)), Draft: newDraft()}
	key := func(item Action) string {
		return "branch-check:" + branchID + ":" + item.Plan.Type + "/" + item.Plan.ID
	}
	var applied []int
	for i, item := range actions {
		check := BranchResourceCheck{Type: item.Plan.Type, ID: item.Plan.ID, RID: item.Plan.RID, Action: item.Action, Artifact: item.Module(), Accepted: true}
		stage.Resources = append(stage.Resources, check)
		if item.Action == "none" {
			continue
		}
		child := decide(stage.Draft, item, key(item))
		if child.Refusal == nil {
			applied = append(applied, i)
			stage.Receipt = child.Receipt
			continue
		}
		stage.Resources[i].Accepted, stage.Resources[i].Code = false, child.Refusal.Code.String()
		stage.Resources[i].Message, stage.Resources[i].Issues = child.Refusal.Message, child.Issues
		stage.Draft, stage.Receipt = newDraft(), nil
		kept := applied[:0]
		for _, j := range applied {
			replay := decide(stage.Draft, actions[j], key(actions[j]))
			if replay.Refusal != nil {
				stage.Resources[j].Accepted, stage.Resources[j].Code = false, replay.Refusal.Code.String()
				stage.Resources[j].Message, stage.Resources[j].Issues = replay.Refusal.Message, replay.Issues
				continue
			}
			kept = append(kept, j)
			stage.Receipt = replay.Receipt
		}
		applied = kept
	}
	return stage
}

type StepOutcome struct {
	Unmatched bool
	Refusal   string
}

func SimulationSummary(candidateID string, passed *bool, steps []StepOutcome) *BranchSimulationCheck {
	summary := &BranchSimulationCheck{CandidateID: candidateID, Passed: passed, Steps: len(steps), Messages: []string{}}
	for i, step := range steps {
		if !step.Unmatched {
			continue
		}
		message := fmt.Sprintf("Simulation step %d did not match its expected outcome", i+1)
		if step.Refusal != "" {
			message += ": " + step.Refusal
		}
		summary.Messages = append(summary.Messages, message)
	}
	if passed != nil && !*passed && len(summary.Messages) == 0 {
		summary.Messages = append(summary.Messages, "The candidate simulation failed")
	}
	return summary
}

func FailedSimulation(message string) *BranchSimulationCheck {
	failed := false
	return &BranchSimulationCheck{Passed: &failed, Messages: []string{message}}
}

type Evaluation[R any] struct {
	Check      BranchCheck
	Conflicts  []BranchResourceDiff
	Simulation *R
}

type Evaluator[S, R any] struct {
	Stage     func(actions []Action) (resources []BranchResourceCheck, accepted bool, graph BranchGraphCheck, staged *S)
	Simulate  func(staged *S) (R, *kernel.Error)
	Summarize func(R) *BranchSimulationCheck
}

func Evaluate[S, R any](w Workspace, e Evaluator[S, R], m platform.Member, branch build.Branch, resolutions []BranchResolution, simulate bool, now time.Time) (Evaluation[R], *kernel.Error) {
	var out Evaluation[R]
	plans, refusal := w.Plan(m, branch, resolutions, now)
	if refusal != nil {
		return out, refusal
	}
	out.Conflicts = ConflictsOf(plans)
	check := BranchCheck{At: now.UTC(), By: m.ID, Base: w.Base(), Content: Content(plans), Conflicts: len(out.Conflicts),
		Resources: []BranchResourceCheck{}, Graph: BranchGraphCheck{Messages: []string{}}}
	var staged *S
	if len(out.Conflicts) > 0 {
		check.Graph.Messages = append(check.Graph.Messages, fmt.Sprintf("Not evaluated: the branch conflicts with main in %d resources; rebase it first", len(out.Conflicts)))
	} else {
		actions, refusal := w.Actions(plans)
		if refusal != nil {
			return out, refusal
		}
		resources, accepted, graph, candidate := e.Stage(actions)
		check.Resources = resources
		if accepted {
			check.Graph, staged = graph, candidate
		} else {
			check.Graph.Messages = append(check.Graph.Messages, "Not evaluated: a resource would be refused in the merged world")
		}
	}
	if simulate {
		switch {
		case staged == nil:
			check.Simulation = FailedSimulation("Not run: the staged candidate is unavailable")
		default:
			result, refusal := e.Simulate(staged)
			if refusal != nil {
				check.Simulation = FailedSimulation(refusal.Message)
			} else {
				out.Simulation = &result
				check.Simulation = e.Summarize(result)
			}
		}
	}
	check.Passed = len(out.Conflicts) == 0 && len(check.Resources) > 0 && !slices.ContainsFunc(check.Resources, func(c BranchResourceCheck) bool { return !c.Accepted }) &&
		check.Graph.Passed && check.Simulation.OK()
	out.Check = check
	return out, nil
}

type Flow struct {
	Read     func(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error)
	Submit   func(m platform.Member, key, schema, id string, payload any, now time.Time) *kernel.Error
	Detail   func(m platform.Member, id string, now time.Time) (BranchDetail, *kernel.Error)
	Plan     func(m platform.Member, branch build.Branch, resolutions []BranchResolution, now time.Time) ([]ResourcePlan, *kernel.Error)
	Recheck  func(m platform.Member, branch build.Branch, now time.Time)
	Reviewer func(id string) bool
}

func (f Flow) Rebase(m platform.Member, id string, request BranchResolveRequest, now time.Time) (BranchDetail, []BranchResourceDiff, *kernel.Error) {
	branch, refusal := f.Read(m, id, now)
	if refusal != nil {
		return BranchDetail{}, nil, refusal
	}
	if branch.State != build.BranchOpen {
		return BranchDetail{}, nil, Refuse("The branch is {state}; only an open branch rebases", branch.State)
	}
	plans, refusal := f.Plan(m, branch, request.Resolutions, now)
	if refusal != nil {
		return BranchDetail{}, nil, refusal
	}
	if conflicts := ConflictsOf(plans); len(conflicts) > 0 {
		return BranchDetail{}, conflicts, nil
	}
	seq := len(branch.Changes)
	for _, plan := range plans {
		if string(plan.Base) == string(plan.Main.Spec) {
			continue
		}
		seq++
		op := plan.Op
		payload := map[string]any{"type": plan.Type, "id": plan.ID, "op": op, "base": plan.Main.Spec, "rebased": true}
		if plan.Main.Spec == nil {
			payload["base"] = nil
		}
		switch {
		case plan.Merged == nil:
			payload["op"] = "delete"
		case !plan.Main.Exists:
			payload["op"], payload["spec"] = "create", plan.Merged
		default:
			payload["op"], payload["spec"] = "update", plan.Merged
		}
		if refusal := f.Submit(m, "branch:"+id+":save:"+strconv.Itoa(seq), build.BranchType+".save", id, payload, now); refusal != nil {
			return BranchDetail{}, nil, refusal
		}
	}
	if rebased, refusal := f.Read(m, id, now); refusal == nil && len(rebased.Changes) > 0 {
		f.Recheck(m, rebased, now)
	}
	detail, refusal := f.Detail(m, id, now)
	return detail, nil, refusal
}

func (f Flow) Propose(m platform.Member, id string, reviewers []string, note string, evaluate func(build.Branch) (BranchCheck, []BranchResourceDiff, *kernel.Error), now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := f.Read(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen || len(branch.Changes) == 0 {
		return BranchDetail{}, Refuse("Only an open branch with changes can be proposed")
	}
	for _, reviewer := range reviewers {
		if !f.Reviewer(reviewer) {
			return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Reviewer {member} must be a builder", reviewer)
		}
	}
	check, conflicts, refusal := evaluate(branch)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if len(conflicts) > 0 {
		return BranchDetail{}, Refuse("The branch conflicts with main in {count} resources; rebase it first", len(conflicts))
	}
	if !check.Passed {
		return BranchDetail{}, Refuse("The staged check of the merged definition graph failed: {message}", check.Failure())
	}
	if reviewers == nil {
		reviewers = []string{}
	}
	if refusal := f.Submit(m, "branch:"+id+":propose:"+strconv.Itoa(len(branch.Changes)), build.BranchType+".propose", id, map[string]any{"reviewers": reviewers, "note": note, "checks": check}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return f.Detail(m, id, now)
}

func (w Workspace) ReadyToMerge(m platform.Member, branch build.Branch, resolutions []BranchResolution, simulate bool, now time.Time) (*BranchCheck, []ResourcePlan, *kernel.Error) {
	if branch.State != build.BranchProposed {
		return nil, nil, Refuse("The branch is {state}; only a proposed branch merges", branch.State)
	}
	if err := branch.Approved(); err != nil {
		return nil, nil, Refuse("{reason}", err.Error())
	}
	plans, refusal := w.Plan(m, branch, resolutions, now)
	if refusal != nil {
		return nil, nil, refusal
	}
	if conflicts := ConflictsOf(plans); len(conflicts) > 0 {
		return nil, nil, Refuse("The branch conflicts with main in {count} resources; rebase it first", len(conflicts))
	}
	check := DecodeCheck(branch)
	if check == nil {
		return nil, nil, Refuse("The proposal has no staged check; re-check the branch before merging")
	}
	if !check.Passed {
		return nil, nil, Refuse("The last staged check failed: {message}; fix the branch and re-check it before merging", check.Failure())
	}
	if check.Base != w.Base() {
		return nil, nil, Refuse("Main changed since the proposal was last checked; rebase or re-check the branch before merging")
	}
	if check.Content != Content(plans) {
		return nil, nil, Refuse("The merged content differs from the checked content; re-check the branch before merging")
	}
	if simulate && check.Simulation == nil {
		return nil, nil, Refuse("The last staged check did not run a simulation; re-check the branch with a simulation before merging")
	}
	return check, plans, nil
}

func (w Workspace) MergeSubmission(m platform.Member, id string, branch build.Branch, check *BranchCheck, plans []ResourcePlan) (*pb.Submission, []Action, []BranchResourceCheck, *kernel.Error) {
	actions, refusal := w.Actions(plans)
	if refusal != nil {
		return nil, nil, nil, refusal
	}
	applied := make([]BranchResourceCheck, 0, len(actions))
	for _, item := range actions {
		applied = append(applied, BranchResourceCheck{Type: item.Plan.Type, ID: item.Plan.ID, RID: item.Plan.RID, Action: item.Action, Artifact: item.Module(), Accepted: true})
	}
	revision := branch.Record.Revision
	payload := platform.Raw(map[string]any{"candidate": check.Graph.Candidate, "applied": applied, "base": check.Base})
	s := &pb.Submission{TenantId: w.Tenant, PrincipalId: m.ID, Authority: build.ID, ExpectedRevision: &revision,
		Target: &pb.EntityRef{Type: build.BranchType, Id: id}, Schema: &pb.SchemaRef{Name: build.BranchType + ".merge", Version: 1}, Payload: payload}
	s.IdempotencyKey = Key("branch:"+id+":merge", m.ID, s.Schema.Name, payload, &revision)
	return s, actions, applied, nil
}

type CompileHooks struct {
	Read    func(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error)
	Main    func(m platform.Member, typ, draft string, now time.Time) (MainDraft, *kernel.Error)
	Project func(m platform.Member, typ, draft string, now time.Time) (MainDraft, string, *kernel.Error)
	Build   func(ctx context.Context, request platform.CodeBuildRequest) (platform.CodeBuildResult, error)
	Append  func(m platform.Member, plan SavePlan, typ, draft string, now time.Time) (BranchDetail, *kernel.Error)
}

func (h CompileHooks) Compile(m platform.Member, id, typ, draft string, now time.Time) (BranchDetail, *kernel.Error) {
	if typ != build.CodeType {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Only a code function compiles")
	}
	branch, refusal := h.openBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	request, expected, spec, refusal := h.codeSource(m, branch, draft, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	result, err := h.Build(ctx, request)
	cancel()
	if err != nil || result.Module == "" {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The build failed: {diagnostics}", result.Diagnostics)
	}
	if result.SourceHash != expected.SourceHash || result.BuildHash != expected.BuildHash {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The compiler answered for different source or schemas")
	}
	artifact := build.CodeArtifact{Module: result.Module, SourceHash: result.SourceHash, BuildHash: result.BuildHash, Toolchain: result.Toolchain}
	branch, refusal = h.openBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	_, latest, spec, refusal := h.codeSource(m, branch, draft, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if latest.SourceHash != expected.SourceHash || latest.BuildHash != expected.BuildHash {
		return BranchDetail{}, Refuse("The code changed while it was compiling; compile it again")
	}
	var saved map[string]json.RawMessage
	_ = json.Unmarshal(spec, &saved)
	saved[CodeBuildKey], _ = json.Marshal(artifact)
	raw, _ := json.Marshal(saved)
	current, project, refusal := h.Project(m, typ, draft, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if latestChange, found := branch.Latest(typ, draft); found && latestChange.Project != "" {
		project = latestChange.Project
	}
	return h.Append(m, SavePlan{Branch: branch, Current: current, Op: "update", Project: project, Spec: raw}, typ, draft, now)
}

func (h CompileHooks) openBranch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	branch, refusal := h.Read(m, id, now)
	if refusal != nil {
		return branch, refusal
	}
	if branch.State != build.BranchOpen {
		return branch, Refuse("The branch is {state}; only an open branch takes a compile", branch.State)
	}
	return branch, nil
}

func (h CompileHooks) codeSource(m platform.Member, branch build.Branch, draft string, now time.Time) (platform.CodeBuildRequest, build.CodeArtifact, json.RawMessage, *kernel.Error) {
	none := platform.CodeBuildRequest{}
	current, refusal := h.Main(m, build.CodeType, draft, now)
	if refusal != nil {
		return none, build.CodeArtifact{}, nil, refusal
	}
	spec, _ := EffectiveSpec(branch, build.CodeType, draft)
	if _, touched := branch.Latest(build.CodeType, draft); !touched {
		spec = current.Spec
	}
	if spec == nil {
		return none, build.CodeArtifact{}, nil, Refuse("{resource} does not exist", build.CodeType+"/"+draft)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(spec, &fields) != nil {
		return none, build.CodeArtifact{}, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The code spec is unreadable")
	}
	delete(fields, CodeBuildKey)
	authored, _ := json.Marshal(fields)
	request, expected, bad := build.CodeBuildOf(authored)
	if bad != nil {
		return none, build.CodeArtifact{}, nil, bad
	}
	return request, expected, authored, nil
}

func (f Flow) Review(m platform.Member, id string, request BranchReviewRequest, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := f.Read(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	reviews := 0
	if branch.Proposal != nil {
		reviews = len(branch.Proposal.Reviews)
	}
	if refusal := f.Submit(m, "branch:"+id+":review:"+strconv.Itoa(len(branch.Changes))+":"+strconv.Itoa(reviews+1), build.BranchType+".review", id, map[string]any{"decision": request.Decision, "note": request.Note}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return f.Detail(m, id, now)
}

func (f Flow) Finish(m platform.Member, id, verb string, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := f.Read(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if refusal := f.Submit(m, "branch:"+id+":"+verb+":"+strconv.Itoa(int(branch.Record.Revision)), build.BranchType+"."+verb, id, map[string]any{}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return f.Detail(m, id, now)
}
