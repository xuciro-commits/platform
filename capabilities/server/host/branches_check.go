package host

import (
	"context"
	"fmt"
	runtimekernel "platformserver/kernel"
	"platformserver/services/actions"
	"platformserver/services/resources/merge"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) decideBranchChild(draft *stagedDecision, m platform.Member, item branches.Action, key string, now time.Time) branches.StageChild {
	return branches.DecideChild(draft, t.ID, m, item, key, func(p any) bool { _, ok := p.(stagedEffectPanic); return ok },
		func(draft *stagedDecision, schema string, s *pb.Submission, automation bool) branches.StageChild {
			return t.decideBranchStep(draft, m, schema, s, automation, now)
		})
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

func (t *Tenant) branchDecider(m platform.Member, now time.Time) func(*stagedDecision, branches.Action, string) branches.StageChild {
	return func(draft *stagedDecision, item branches.Action, key string) branches.StageChild {
		return t.decideBranchChild(draft, m, item, key, now)
	}
}

func (t *Tenant) stageBranchLocked(m platform.Member, branchID string, actions []branches.Action, now time.Time) branches.Stage[*stagedDecision] {
	return branches.StageActions(t.newStagedDecision, t.branchDecider(m, now), branchID, actions)
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
	view.ConnectorRoster = t.ConnectorRoster.Fork()
	return view, nil
}

func (t *Tenant) branchWorldLocked(m platform.Member, id string, now time.Time) (*Tenant, *kernel.Error) {
	return branches.StagedWorld(t.branchWorkspace(), m, id, now, func(plan branches.WorldPlan) branches.Stage[*stagedDecision] {
		return t.stageBranchLocked(m, plan.Branch.ID, plan.Actions, now)
	}, func(stage branches.Stage[*stagedDecision]) (*Tenant, error) {
		return t.stageViewLocked(stage, now)
	})
}

func (t *Tenant) branchPreview(m platform.Member) func([]build.JointDraftRef) (branches.GraphPreview, platform.ReleaseCandidate, error) {
	return func(drafts []build.JointDraftRef) (branches.GraphPreview, platform.ReleaseCandidate, error) {
		preview, candidate, err := t.previewReleaseLocked(drafts, m)
		return branches.GraphPreview{Diagnostic: preview.Diagnostic, Candidate: candidate.ID, Added: preview.Added, Removed: preview.Removed, Changed: preview.Changed}, candidate, err
	}
}

func (t *Tenant) stageGraphLocked(m platform.Member, stage branches.Stage[*stagedDecision], actions []branches.Action, now time.Time) (branches.BranchGraphCheck, *stagedCandidate) {
	var reader *Tenant
	graph, drafts, candidate := branches.CheckGraph(func() (build.ConstructionDrafts, bool, error) {
		view, err := t.stageViewLocked(stage, now)
		if err != nil {
			return nil, false, err
		}
		reader = view
		owner := view.builders[build.ID]
		return owner, owner != nil, nil
	}, func(drafts []build.JointDraftRef) (branches.GraphPreview, platform.ReleaseCandidate, error) {
		return reader.branchPreview(m)(drafts)
	}, actions)
	if drafts == nil {
		return graph, nil
	}
	return graph, &stagedCandidate{StagedCandidate: trialowner.StagedCandidate{Candidate: candidate, Drafts: drafts}, reader: reader}
}

func (t *Tenant) evaluateBranch(m platform.Member, branch build.Branch, resolutions []branches.BranchResolution, sim *trialowner.CandidateSimulationRequest, now time.Time) (branches.Evaluation[trialowner.CandidateSimulation], *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.Evaluation[trialowner.CandidateSimulation], *kernel.Error) {
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
			Summarize: trialowner.CandidateSimulation.Summary,
		}, m, branch, resolutions, sim != nil, now)
	})
}

func (t *Tenant) BranchChecks(m platform.Member, id string, request BranchCheckRequest, now time.Time) (BranchChecks, *kernel.Error) {
	return branches.ReadChecks(t.readBranch, t.evaluateBranch, m, id, request, now)
}

func (t *Tenant) RecheckBranch(m platform.Member, id string, request BranchCheckRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return branches.Recheck(t.branchFlow(), t.evaluateBranch, m, id, request, now)
}

func (t *Tenant) branchFlow() branches.Flow {
	return branches.Flow{
		Tenant: t.ID,
		Read:   t.readBranch,
		Submit: func(m platform.Member, submission *pb.Submission, now time.Time) *kernel.Error {
			_, refusal := t.Submit(m, submission, now)
			return refusal
		},
		Detail: t.BranchDetail,
		Plan: func(m platform.Member, branch build.Branch, resolutions []branches.BranchResolution, now time.Time) ([]branches.ResourcePlan, *kernel.Error) {
			return branchRead(t, func(w branches.Workspace) ([]branches.ResourcePlan, *kernel.Error) {
				return w.Plan(m, branch, resolutions, now)
			})
		},
		Evaluate: func(m platform.Member, branch build.Branch, now time.Time) (branches.BranchCheck, *kernel.Error) {
			evaluation, refusal := t.evaluateBranch(m, branch, nil, nil, now)
			return evaluation.Check, refusal
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
	return branches.Propose(t.branchFlow(), t.evaluateBranch, m, id, request, now)
}

func (t *Tenant) MergeBranch(m platform.Member, id string, request BranchMergeRequest, now time.Time) (BranchMergeResult, *kernel.Error) {
	if refusal := t.Admits(m); refusal != nil {
		return BranchMergeResult{}, refusal
	}
	return branches.MergeBranch[trialowner.CandidateSimulation](m, id, request.Resolutions, request.Simulation != nil, now, t.applyBranch, t.saveBranchCandidate, t.BranchDetail)
}

func (t *Tenant) applyBranch(m platform.Member, id string, resolutions []branches.BranchResolution, simulate bool, now time.Time) (branches.MergePlan, *kernel.Error) {
	type prepared struct {
		plan branches.MergePlan
	}
	value, refusal := decideOnImage(t, context.Background(), "branch:merge", func(t *Tenant) (prepared, *kernel.Error) {
		plan, refusal := t.prepareBranchMerge(m, id, resolutions, simulate, now)
		return prepared{plan: plan}, refusal
	}, func(t *Tenant, candidate preparedAccepted, value prepared, refusal *kernel.Error) (prepared, *kernel.Error) {
		_, refusal = t.finishPreparedSubmission(m, candidate, nil, refusal)
		return value, refusal
	})
	return value.plan, refusal
}

func (t *Tenant) prepareBranchMerge(m platform.Member, id string, resolutions []branches.BranchResolution, simulate bool, now time.Time) (branches.MergePlan, *kernel.Error) {
	var none branches.MergePlan
	if t.quarantined() {
		return none, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if refusal := t.recordReadRefusal(); refusal != nil {
		return none, refusal
	}
	plan, refusal := t.branchWorkspace().PlanMerge(m, id, resolutions, simulate, now)
	if refusal != nil {
		return none, refusal
	}
	s := plan.Submission
	owner := t.App(t.owner["action:"+s.Schema.Name])
	if owner == nil {
		return none, unknown()
	}
	s = t.currentSubmissionSchema(owner, s)
	if refusal := t.delegatedBound(m, s); refusal != nil {
		return none, refusal
	}
	before := branches.MergeBefore(id, plan.Actions, t.branchDecider(m, now))
	if _, refusal := t.accept(owner, m, s, now, acceptOptions{before: before}); refusal != nil {
		return none, refusal
	}
	return plan, nil
}

func (t *Tenant) saveBranchCandidate(m platform.Member, branchID, candidateID string, actions []branches.Action, now time.Time) (string, error) {
	drafts, err := readWithError(t, context.Background(), func(t *Tenant) ([]build.JointDraftRef, error) {
		owner := t.builders[build.ID]
		if owner == nil {
			return nil, fmt.Errorf("the tenant has no builder")
		}
		drafts := branches.DraftSet(owner, actions)
		preview, _, err := branches.PreviewDrafts(drafts, t.branchPreview(m))
		if err == nil && preview.Candidate != candidateID {
			err = fmt.Errorf("the release candidate differs from the checked candidate")
		}
		return drafts, err
	})
	if err != nil {
		return "", err
	}
	saved, err := t.SaveReleaseCandidates(m, drafts, candidateID, "branch:"+branchID+":merge", now)
	if err == nil {
		_, err = t.sealer().Seal(saved, now)
	}
	return saved, err
}
