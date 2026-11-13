package host

import (
	"context"
	"fmt"
	runtimekernel "platformserver/kernel"
	"platformserver/services/actions"
	"platformserver/services/resources/merge"
	"slices"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) branchBaseLocked() string {
	return t.Projection.BranchBase(t.releases.Active(), build.BranchDraftType)
}

func (t *Tenant) decideBranchChild(draft *stagedDecision, m platform.Member, item branches.Action, key string, now time.Time) (out branches.StageChild) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stagedEffectPanic); !ok {
				panic(p)
			}
			out = branches.StageChild{Refusal: platform.RefuseOutsideAcceptedBoundary()}
		}
	}()
	if item.Schema != "" {
		out = t.decideBranchStep(draft, m, item.Schema, item.Submission(t.ID, m, key), false, now)
		if out.Refusal != nil {
			return out
		}
	}
	if item.Artifact != nil {
		attach := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, Target: &pb.EntityRef{Type: item.Plan.Type, Id: item.Plan.ID},
			Schema: &pb.SchemaRef{Name: build.SchemaCodeAttach, Version: 1}, Payload: item.Artifact}
		attach.IdempotencyKey = branches.Key(key+":build", m.ID, build.SchemaCodeAttach, item.Artifact, nil)
		out = t.decideBranchStep(draft, m, build.SchemaCodeAttach, attach, true, now)
	}
	return out
}

func (t *Tenant) decideBranchStep(draft *stagedDecision, m platform.Member, schema string, submission *pb.Submission, automation bool, now time.Time) branches.StageChild {
	a := t.App(t.owner["action:"+schema])
	if a == nil {
		return branches.StageChild{Refusal: platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The resource type has no {action} action", schema)}
	}
	s := t.currentSubmissionSchema(a, submission)
	if refusal := t.delegatedBound(m, s); refusal != nil {
		return branches.StageChild{Refusal: actions.Explained(refusal, a, s.GetSchema().GetName(), runtimekernel.Target(s))}
	}
	receipt, refusal := platform.Decide(platform.NewCaller(draft, m, build.ID, false, automation), a, s, now)
	if refusal != nil {
		issues := t.readableFieldIssues(m, draft.InputIssues[refusal], now)
		return branches.StageChild{Issues: issues, Refusal: actions.Explained(refusal, a, s.GetSchema().GetName(), runtimekernel.Target(s))}
	}
	return branches.StageChild{Receipt: receipt}
}

func (t *Tenant) stageBranchLocked(m platform.Member, branchID string, actions []branches.Action, now time.Time) branches.Stage[*stagedDecision] {
	return branches.StageActions(t.newStagedDecision, func(draft *stagedDecision, item branches.Action, key string) branches.StageChild {
		return t.decideBranchChild(draft, m, item, key, now)
	}, branchID, actions)
}

func (t *Tenant) stageViewLocked(stage branches.Stage[*stagedDecision], now time.Time) (*Tenant, error) {
	draft := stage.Draft
	if stage.Receipt != nil {
		if refusal := draft.answerRequests(now); refusal != nil {
			return nil, fmt.Errorf("The merged resources cannot be accepted together: %s", refusal.Message)
		}
		if refusal := draft.stageObservers(); refusal != nil {
			return nil, fmt.Errorf("The merged resources cannot be accepted together: %s", refusal.Message)
		}
		if _, err := draft.batchResult(build.ID, stage.Receipt.GetSubmission(), stage.Receipt, now); err != nil {
			return nil, fmt.Errorf("The merged resources do not form one valid accepted result: %s", err)
		}
	}
	view, err := t.assetInstallationView(draft.Records)
	if err != nil {
		return nil, fmt.Errorf("The staged definition graph could not be assembled: %s", err)
	}
	view.connectors = t.connectors.Fork()
	return view, nil
}

type branchWorld struct {
	stage   branches.Stage[*stagedDecision]
	actions []branches.Action
	view    *Tenant
	branch  build.Branch
}

func (t *Tenant) branchWorldLocked(m platform.Member, id string, now time.Time) (branchWorld, *kernel.Error) {
	var world branchWorld
	branch, refusal := t.ReadBranch(m, id, now)
	if refusal != nil {
		return world, refusal
	}
	if branch.State != build.BranchOpen && branch.State != build.BranchProposed {
		return world, branches.Refuse("The branch is {state}; only an open or proposed branch has a staged world", branch.State)
	}
	plans, refusal := t.branchWorkspace().Plan(m, branch, nil, now)
	if refusal != nil {
		return world, refusal
	}
	if conflicts := branches.ConflictsOf(plans); len(conflicts) > 0 {
		return world, branches.Refuse("The branch conflicts with main in {count} resources; rebase it first", len(conflicts))
	}
	actions, refusal := t.branchWorkspace().Actions(plans)
	if refusal != nil {
		return world, refusal
	}
	stage := t.stageBranchLocked(m, branch.ID, actions, now)
	if i := slices.IndexFunc(stage.Resources, func(c branches.BranchResourceCheck) bool { return !c.Accepted }); i >= 0 {
		return world, branches.Refuse("{resource} would be refused in the merged world: {message}", stage.Resources[i].Type+"/"+stage.Resources[i].ID, stage.Resources[i].Message)
	}
	view, err := t.stageViewLocked(stage, now)
	if err != nil {
		return world, branches.Refuse("{reason}", err.Error())
	}
	return branchWorld{stage: stage, actions: actions, view: view, branch: branch}, nil
}

func (t *Tenant) stageGraphLocked(m platform.Member, stage branches.Stage[*stagedDecision], actions []branches.Action, now time.Time) (branches.BranchGraphCheck, *stagedCandidate) {
	graph := branches.BranchGraphCheck{Messages: []string{}}
	fail := func(format string, args ...any) (branches.BranchGraphCheck, *stagedCandidate) {
		graph.Messages = append(graph.Messages, fmt.Sprintf(format, args...))
		return graph, nil
	}
	view, err := t.stageViewLocked(stage, now)
	if err != nil {
		return fail("%s", err.Error())
	}
	owner, ok := view.App(build.ID).(build.ConstructionDrafts)
	if !ok {
		return fail("The staged definition graph has no builder")
	}
	drafts := branches.DraftSet(owner, actions)
	if len(drafts) == 0 {
		graph.Passed = true
		return graph, nil
	}
	preview, candidate, err := view.previewReleaseLocked(drafts, m)
	if err == nil && preview.Diagnostic != "" {
		err = fmt.Errorf("%s", preview.Diagnostic)
	}
	if err != nil {
		return fail("The merged drafts do not form a valid release: %s", err)
	}
	graph.Passed, graph.Candidate = true, candidate.ID
	graph.Added, graph.Removed, graph.Changed = preview.Added, preview.Removed, preview.Changed
	return graph, &stagedCandidate{candidate: candidate, reader: view, drafts: drafts}
}

func (t *Tenant) evaluateBranch(m platform.Member, branch build.Branch, resolutions []branches.BranchResolution, sim *trialowner.CandidateSimulationRequest, now time.Time) (branches.Evaluation[trialowner.CandidateSimulation], *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.Evaluation[trialowner.CandidateSimulation], *kernel.Error) {
		return t.evaluateBranchRead(m, branch, resolutions, sim, now)
	})
}

func (t *Tenant) evaluateBranchRead(m platform.Member, branch build.Branch, resolutions []branches.BranchResolution, sim *trialowner.CandidateSimulationRequest, now time.Time) (branches.Evaluation[trialowner.CandidateSimulation], *kernel.Error) {
	return branches.Evaluate(t.branchWorkspace(), branches.Evaluator[stagedCandidate, trialowner.CandidateSimulation]{
		Stage: func(actions []branches.Action) ([]branches.BranchResourceCheck, bool, branches.BranchGraphCheck, *stagedCandidate) {
			stage := t.stageBranchLocked(m, branch.ID, actions, now)
			if !stage.Accepted() {
				return stage.Resources, false, branches.BranchGraphCheck{}, nil
			}
			graph, staged := t.stageGraphLocked(m, stage, actions, now)
			return stage.Resources, true, graph, staged
		},
		Simulate: func(staged *stagedCandidate) (trialowner.CandidateSimulation, *kernel.Error) {
			return t.simulateCandidate(m, *sim, staged)
		},
		Summarize: func(out trialowner.CandidateSimulation) *branches.BranchSimulationCheck {
			steps := make([]branches.StepOutcome, len(out.Steps))
			for i, step := range out.Steps {
				steps[i] = branches.StepOutcome{Unmatched: step.Matched != nil && !*step.Matched || step.ComputeMatched != nil && !*step.ComputeMatched || step.FunctionMatched != nil && !*step.FunctionMatched, Refusal: step.Refusal}
			}
			return branches.SimulationSummary(out.CandidateID, out.Passed, steps)
		},
	}, m, branch, resolutions, sim != nil, now)
}

func (t *Tenant) recordBranchCheck(m platform.Member, branch build.Branch, check branches.BranchCheck, now time.Time) *kernel.Error {
	schema := build.BranchType + ".check"
	if branch.State == build.BranchProposed {
		schema = build.BranchType + ".recheck"
	}
	return t.submitBranch(m, "branch:"+branch.ID+":check:"+strconv.Itoa(int(branch.Record.Revision)), schema, branch.ID, map[string]any{"check": check}, now)
}

func (t *Tenant) BranchChecks(m platform.Member, id string, request BranchCheckRequest, now time.Time) (BranchChecks, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchChecks{}, refusal
	}
	evaluation, refusal := t.evaluateBranch(m, branch, request.Resolutions, request.Simulation, now)
	if refusal != nil {
		return BranchChecks{}, refusal
	}
	checks := BranchChecks{At: now.UTC(), Conflicts: evaluation.Conflicts, Resources: evaluation.Check.Resources, Check: evaluation.Check, Simulation: evaluation.Simulation, Ready: evaluation.Check.Passed}
	if err := branch.Approved(); err != nil {
		checks.Approval = err.Error()
	} else {
		checks.Approved = true
	}
	return checks, nil
}

func (t *Tenant) RecheckBranch(m platform.Member, id string, request BranchCheckRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen && branch.State != build.BranchProposed || len(branch.Changes) == 0 {
		return branches.BranchDetail{}, branches.Refuse("Only an open or proposed branch with changes can be re-checked")
	}
	evaluation, refusal := t.evaluateBranch(m, branch, request.Resolutions, request.Simulation, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	if refusal := t.recordBranchCheck(m, branch, evaluation.Check, now); refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) branchFlow() branches.Flow {
	return branches.Flow{
		Read:   t.readBranch,
		Submit: t.submitBranch,
		Detail: t.BranchDetail,
		Plan:   t.planBranch,
		Recheck: func(m platform.Member, branch build.Branch, now time.Time) {
			if evaluation, refusal := t.evaluateBranch(m, branch, nil, nil, now); refusal == nil {
				_ = t.recordBranchCheck(m, branch, evaluation.Check, now)
			}
		},
		Reviewer: func(id string) bool {
			member, ok := t.Member(id)
			return ok && member.Holds(build.ID, build.Builder)
		},
	}
}

func (t *Tenant) RebaseBranch(m platform.Member, id string, request branches.BranchResolveRequest, now time.Time) (branches.BranchDetail, []branches.BranchResourceDiff, *kernel.Error) {
	return t.branchFlow().Rebase(m, id, request, now)
}

func (t *Tenant) ProposeBranch(m platform.Member, id string, request BranchProposeRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return t.branchFlow().Propose(m, id, request.Reviewers, request.Note, func(branch build.Branch) (branches.BranchCheck, []branches.BranchResourceDiff, *kernel.Error) {
		evaluation, refusal := t.evaluateBranch(m, branch, request.Resolutions, request.Simulation, now)
		return evaluation.Check, evaluation.Conflicts, refusal
	}, now)
}

func (t *Tenant) MergeBranch(m platform.Member, id string, request BranchMergeRequest, now time.Time) (BranchMergeResult, *kernel.Error) {
	var result BranchMergeResult
	if refusal := t.Admits(m); refusal != nil {
		return result, refusal
	}
	candidate, applied, refusal := t.applyBranch(m, id, request, now)
	if refusal != nil {
		return result, refusal
	}
	result.Applied = applied
	if candidate.id != "" {
		if saved, err := t.saveBranchCandidate(m, id, candidate, now); err != nil {
			result.CandidateError = err.Error()
		} else {
			result.Candidate = saved
		}
	}
	var refusalDetail *kernel.Error
	result.Branch, refusalDetail = t.BranchDetail(m, id, now)
	return result, refusalDetail
}

type mergedCandidate struct {
	id      string
	actions []branches.Action
}

func (t *Tenant) applyBranch(m platform.Member, id string, request BranchMergeRequest, now time.Time) (mergedCandidate, []branches.BranchResourceCheck, *kernel.Error) {
	type prepared struct {
		merged mergedCandidate
		checks []branches.BranchResourceCheck
	}
	value, refusal := decideOnImage(t, context.Background(), "branch:merge", func(t *Tenant) (prepared, *kernel.Error) {
		merged, checks, refusal := t.prepareBranchMerge(m, id, request, now)
		return prepared{merged: merged, checks: checks}, refusal
	}, func(t *Tenant, candidate preparedAccepted, value prepared, refusal *kernel.Error) (prepared, *kernel.Error) {
		_, refusal = t.finishPreparedSubmission(m, candidate, nil, refusal)
		return value, refusal
	})
	return value.merged, value.checks, refusal
}

func (t *Tenant) prepareBranchMerge(m platform.Member, id string, request BranchMergeRequest, now time.Time) (mergedCandidate, []branches.BranchResourceCheck, *kernel.Error) {
	var none mergedCandidate
	if t.quarantined() {
		return none, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if refusal := t.recordReadRefusal(); refusal != nil {
		return none, nil, refusal
	}
	branch, refusal := t.ReadBranch(m, id, now)
	if refusal != nil {
		return none, nil, refusal
	}
	check, plans, refusal := t.branchWorkspace().ReadyToMerge(m, branch, request.Resolutions, request.Simulation != nil, now)
	if refusal != nil {
		return none, nil, refusal
	}
	s, actions, applied, refusal := t.branchWorkspace().MergeSubmission(m, id, branch, check, plans)
	if refusal != nil {
		return none, nil, refusal
	}
	owner := t.App(t.owner["action:"+s.Schema.Name])
	if owner == nil {
		return none, nil, unknown()
	}
	s = t.currentSubmissionSchema(owner, s)
	if refusal := t.delegatedBound(m, s); refusal != nil {
		return none, nil, refusal
	}
	before := func(draft *stagedDecision) *kernel.Error {
		for _, item := range actions {
			if item.Action == "none" {
				continue
			}
			child := t.decideBranchChild(draft, m, item, "branch:"+id+":merge:"+item.Plan.Type+"/"+item.Plan.ID, now)
			if child.Refusal != nil {
				return platform.Refuse(child.Refusal.Code, "{resource} could not be applied: {message}", item.Plan.Type+"/"+item.Plan.ID, child.Refusal.Message)
			}
		}
		return nil
	}
	if _, refusal := t.accept(owner, m, s, now, acceptOptions{before: before}); refusal != nil {
		return none, nil, refusal
	}
	return mergedCandidate{id: check.Graph.Candidate, actions: actions}, applied, nil
}

func (t *Tenant) saveBranchCandidate(m platform.Member, branchID string, merged mergedCandidate, now time.Time) (string, error) {
	type preparation struct {
		drafts    []build.JointDraftRef
		preview   releaseowner.ReleasePreview
		candidate platform.ReleaseCandidate
	}
	prepared, err := readWithError(t, context.Background(), func(t *Tenant) (preparation, error) {
		owner, ok := t.App(build.ID).(build.ConstructionDrafts)
		if !ok {
			return preparation{}, fmt.Errorf("the tenant has no builder")
		}
		drafts := branches.DraftSet(owner, merged.actions)
		preview, candidate, err := t.previewReleaseLocked(drafts, m)
		return preparation{drafts: drafts, preview: preview, candidate: candidate}, err
	})
	drafts, preview, candidate := prepared.drafts, prepared.preview, prepared.candidate
	if err == nil && preview.Diagnostic != "" {
		err = fmt.Errorf("%s", preview.Diagnostic)
	}
	if err != nil {
		return "", err
	}
	if candidate.ID != merged.id {
		return "", fmt.Errorf("the release candidate differs from the checked candidate")
	}
	saved, err := t.SaveReleaseCandidates(m, drafts, candidate.ID, "branch:"+branchID+":merge", now)
	if err == nil {
		_, err = t.SealCandidate(saved, now)
	}
	return saved, err
}
