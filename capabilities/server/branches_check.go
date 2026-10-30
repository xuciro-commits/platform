package platformserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
	trialowner "platformserver/studio/trials"
)

func decodeBranchCheck(branch build.Branch) *BranchCheck {
	if len(branch.Check) == 0 {
		return nil
	}
	var check BranchCheck
	if json.Unmarshal(branch.Check, &check) != nil {
		return nil
	}
	if check.Resources == nil {
		check.Resources = []BranchResourceCheck{}
	}
	if check.Graph.Messages == nil {
		check.Graph.Messages = []string{}
	}
	return &check
}

func (c BranchCheck) Failure() string {
	for _, resource := range c.Resources {
		if !resource.Accepted {
			return resource.Type + "/" + resource.ID + ": " + resource.Message
		}
	}
	if len(c.Graph.Messages) > 0 && !c.Graph.Passed {
		return c.Graph.Messages[0]
	}
	if c.Simulation != nil && len(c.Simulation.Messages) > 0 && (c.Simulation.Passed == nil || !*c.Simulation.Passed) {
		return c.Simulation.Messages[0]
	}
	if c.Conflicts > 0 {
		return strconv.Itoa(c.Conflicts) + " resources conflict with main"
	}
	return ""
}

func (t *Tenant) branchBaseLocked() string {
	sum := sha256.New()
	sum.Write([]byte(t.releases.Active()))
	for _, et := range t.records.SortedTypes() {
		info := et.Info()
		if !build.BranchDraftType(info.Type) {
			continue
		}
		sum.Write([]byte("\x00" + info.Type))
		t.records.Lock()
		var entries []string
		for id, row := range et.Rows().All() {
			header := row.Value.FieldByName("Record").Interface().(platform.Record)
			entry := id + ":" + strconv.FormatUint(uint64(header.Revision), 10)
			if header.Archived {
				entry += ":archived"
			}
			entries = append(entries, entry)
		}
		t.records.Unlock()
		sort.Strings(entries)
		for _, entry := range entries {
			sum.Write([]byte("\x00" + entry))
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func branchContent(plans []resourcePlan) string {
	ordered := slices.Clone(plans)
	slices.SortFunc(ordered, func(a, b resourcePlan) int { return strings.Compare(a.Type+"/"+a.ID, b.Type+"/"+b.ID) })
	sum := sha256.New()
	for _, plan := range ordered {
		sum.Write([]byte(plan.Type + "\x00" + plan.ID + "\x00" + plan.Op + "\x00"))
		sum.Write(plan.Merged)
		sum.Write([]byte("\x00"))
	}
	return hex.EncodeToString(sum.Sum(nil))
}

type branchChild struct {
	receipt *pb.ChangeRecord
	issues  []platform.FieldIssue
	refusal *kernel.Error
}

func (t *Tenant) decideBranchChild(draft *stagedDecision, m platform.Member, item branchAction, key string, now time.Time) (out branchChild) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stagedEffectPanic); !ok {
				panic(p)
			}
			out = branchChild{refusal: platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action uses an effect outside the accepted-result boundary")}
		}
	}()
	if item.schema != "" {
		out = t.decideBranchStep(draft, m, item.schema, item.submission(t.ID, m, key), false, now)
		if out.refusal != nil {
			return out
		}
	}
	if item.artifact != nil {
		attach := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, Target: &pb.EntityRef{Type: item.plan.Type, Id: item.plan.ID},
			Schema: &pb.SchemaRef{Name: build.SchemaCodeAttach, Version: 1}, Payload: item.artifact}
		attach.IdempotencyKey = branchKey(key+":build", m.ID, build.SchemaCodeAttach, item.artifact, nil)
		out = t.decideBranchStep(draft, m, build.SchemaCodeAttach, attach, true, now)
	}
	return out
}

func (t *Tenant) decideBranchStep(draft *stagedDecision, m platform.Member, schema string, submission *pb.Submission, automation bool, now time.Time) branchChild {
	a := t.owner["action:"+schema]
	if a == nil {
		return branchChild{refusal: platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The resource type has no {action} action", schema)}
	}
	s := t.currentSubmissionSchema(a, submission)
	if refusal := t.delegatedBound(m, s); refusal != nil {
		return branchChild{refusal: explained(refusal, a, s.GetSchema().GetName(), target(s))}
	}
	receipt, refusal := platform.Decide(platform.NewCaller(draft, m, build.ID, false, automation), a, s, now)
	if refusal != nil {
		issues := t.readableFieldIssues(m, draft.inputIssues[refusal], now)
		return branchChild{issues: issues, refusal: explained(refusal, a, s.GetSchema().GetName(), target(s))}
	}
	return branchChild{receipt: receipt}
}

type branchStage struct {
	draft     *stagedDecision
	resources []BranchResourceCheck
	receipt   *pb.ChangeRecord
}

func (s branchStage) accepted() bool {
	return !slices.ContainsFunc(s.resources, func(c BranchResourceCheck) bool { return !c.Accepted })
}

func (t *Tenant) stageBranchLocked(m platform.Member, branchID string, actions []branchAction, now time.Time) branchStage {
	stage := branchStage{resources: make([]BranchResourceCheck, 0, len(actions)), draft: t.newStagedDecision()}
	key := func(item branchAction) string {
		return "branch-check:" + branchID + ":" + item.plan.Type + "/" + item.plan.ID
	}
	var applied []int
	for i, item := range actions {
		check := BranchResourceCheck{Type: item.plan.Type, ID: item.plan.ID, RID: item.plan.RID, Action: item.action, Artifact: item.module(), Accepted: true}
		stage.resources = append(stage.resources, check)
		if item.action == "none" {
			continue
		}
		child := t.decideBranchChild(stage.draft, m, item, key(item), now)
		if child.refusal == nil {
			applied = append(applied, i)
			stage.receipt = child.receipt
			continue
		}
		stage.resources[i].Accepted, stage.resources[i].Code = false, child.refusal.Code.String()
		stage.resources[i].Message, stage.resources[i].Issues = child.refusal.Message, child.issues
		stage.draft, stage.receipt = t.newStagedDecision(), nil
		kept := applied[:0]
		for _, j := range applied {
			replay := t.decideBranchChild(stage.draft, m, actions[j], key(actions[j]), now)
			if replay.refusal != nil {
				stage.resources[j].Accepted, stage.resources[j].Code = false, replay.refusal.Code.String()
				stage.resources[j].Message, stage.resources[j].Issues = replay.refusal.Message, replay.issues
				continue
			}
			kept = append(kept, j)
			stage.receipt = replay.receipt
		}
		applied = kept
	}
	return stage
}

func branchDraftSet(owner build.ConstructionDrafts, actions []branchAction) []build.JointDraftRef {
	var drafts []build.JointDraftRef
	for _, item := range actions {
		if item.action == "none" || item.action == "archive" {
			continue
		}
		ref, err := build.BranchResource(item.plan.Type, item.plan.ID)
		if err != nil {
			continue
		}
		selected := []build.JointDraftRef{{Kind: ref.Kind, ID: item.plan.ID}}
		if closure, err := owner.ReferencedDrafts(ref.Kind, item.plan.ID); err == nil {
			selected = append(selected, closure...)
		}
		for _, draft := range selected {
			if !slices.Contains(drafts, draft) {
				drafts = append(drafts, draft)
			}
		}
	}
	return drafts
}

func (t *Tenant) stageViewLocked(stage branchStage, now time.Time) (*Tenant, error) {
	draft := stage.draft
	if stage.receipt != nil {
		if refusal := draft.answerRequests(now); refusal != nil {
			return nil, fmt.Errorf("The merged resources cannot be accepted together: %s", refusal.Message)
		}
		if refusal := draft.stageObservers(); refusal != nil {
			return nil, fmt.Errorf("The merged resources cannot be accepted together: %s", refusal.Message)
		}
		if _, err := draft.batchResult(build.ID, stage.receipt.GetSubmission(), stage.receipt, now); err != nil {
			return nil, fmt.Errorf("The merged resources do not form one valid accepted result: %s", err)
		}
	}
	view, err := t.assetInstallationView(draft.records)
	if err != nil {
		return nil, fmt.Errorf("The staged definition graph could not be assembled: %s", err)
	}
	t.opsMu.Lock()
	descriptors, marks := t.connectors.kernel.State()
	t.opsMu.Unlock()
	for i, descriptor := range descriptors {
		descriptors[i] = proto.Clone(descriptor).(*pb.ConnectorDescriptor)
	}
	view.connectors = newConnectorRoster(t.services.Kernel)
	view.connectors.kernel.Restore(descriptors, marks)
	return view, nil
}

type branchWorld struct {
	stage   branchStage
	actions []branchAction
	view    *Tenant
}

func (t *Tenant) branchWorldLocked(m platform.Member, id string, now time.Time) (branchWorld, *kernel.Error) {
	var world branchWorld
	branch, refusal := t.readBranchLocked(m, id, now)
	if refusal != nil {
		return world, refusal
	}
	if branch.State != build.BranchOpen && branch.State != build.BranchProposed {
		return world, branchRefuse("The branch is {state}; only an open or proposed branch has a staged world", branch.State)
	}
	plans, refusal := t.planBranchLocked(m, branch, nil, now)
	if refusal != nil {
		return world, refusal
	}
	if conflicts := conflictsOf(plans); len(conflicts) > 0 {
		return world, branchRefuse("The branch conflicts with main in {count} resources; rebase it first", len(conflicts))
	}
	actions, refusal := t.branchActions(plans)
	if refusal != nil {
		return world, refusal
	}
	stage := t.stageBranchLocked(m, branch.ID, actions, now)
	if i := slices.IndexFunc(stage.resources, func(c BranchResourceCheck) bool { return !c.Accepted }); i >= 0 {
		return world, branchRefuse("{resource} would be refused in the merged world: {message}", stage.resources[i].Type+"/"+stage.resources[i].ID, stage.resources[i].Message)
	}
	view, err := t.stageViewLocked(stage, now)
	if err != nil {
		return world, branchRefuse("{reason}", err.Error())
	}
	return branchWorld{stage: stage, actions: actions, view: view}, nil
}

func (t *Tenant) stageGraphLocked(m platform.Member, stage branchStage, actions []branchAction, now time.Time) (BranchGraphCheck, *stagedCandidate) {
	graph := BranchGraphCheck{Messages: []string{}}
	fail := func(format string, args ...any) (BranchGraphCheck, *stagedCandidate) {
		graph.Messages = append(graph.Messages, fmt.Sprintf(format, args...))
		return graph, nil
	}
	view, err := t.stageViewLocked(stage, now)
	if err != nil {
		return fail("%s", err.Error())
	}
	owner, ok := view.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return fail("The staged definition graph has no builder")
	}
	drafts := branchDraftSet(owner, actions)
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

type branchEvaluation struct {
	check      BranchCheck
	conflicts  []BranchResourceDiff
	simulation *trialowner.CandidateSimulation
}

func simulationSummary(out trialowner.CandidateSimulation) *BranchSimulationCheck {
	summary := &BranchSimulationCheck{CandidateID: out.CandidateID, Passed: out.Passed, Steps: len(out.Steps), Messages: []string{}}
	for i, step := range out.Steps {
		unmatched := step.Matched != nil && !*step.Matched || step.ComputeMatched != nil && !*step.ComputeMatched || step.FunctionMatched != nil && !*step.FunctionMatched
		if !unmatched {
			continue
		}
		message := fmt.Sprintf("Simulation step %d did not match its expected outcome", i+1)
		if step.Refusal != "" {
			message += ": " + step.Refusal
		}
		summary.Messages = append(summary.Messages, message)
	}
	if out.Passed != nil && !*out.Passed && len(summary.Messages) == 0 {
		summary.Messages = append(summary.Messages, "The candidate simulation failed")
	}
	return summary
}

func (s *BranchSimulationCheck) ok() bool {
	return s == nil || len(s.Messages) == 0 && (s.Passed == nil || *s.Passed)
}

func failedSimulation(message string) *BranchSimulationCheck {
	failed := false
	return &BranchSimulationCheck{Passed: &failed, Messages: []string{message}}
}

func (t *Tenant) evaluateBranch(m platform.Member, branch build.Branch, resolutions []BranchResolution, sim *trialowner.CandidateSimulationRequest, now time.Time) (branchEvaluation, *kernel.Error) {
	var out branchEvaluation
	t.mu.Lock()
	plans, refusal := t.planBranchLocked(m, branch, resolutions, now)
	if refusal != nil {
		t.mu.Unlock()
		return out, refusal
	}
	out.conflicts = conflictsOf(plans)
	check := BranchCheck{At: now.UTC(), By: m.ID, Base: t.branchBaseLocked(), Content: branchContent(plans), Conflicts: len(out.conflicts),
		Resources: []BranchResourceCheck{}, Graph: BranchGraphCheck{Messages: []string{}}}
	var staged *stagedCandidate
	if len(out.conflicts) > 0 {
		check.Graph.Messages = append(check.Graph.Messages, fmt.Sprintf("Not evaluated: the branch conflicts with main in %d resources; rebase it first", len(out.conflicts)))
	} else {
		actions, refusal := t.branchActions(plans)
		if refusal != nil {
			t.mu.Unlock()
			return out, refusal
		}
		stage := t.stageBranchLocked(m, branch.ID, actions, now)
		check.Resources = stage.resources
		if stage.accepted() {
			check.Graph, staged = t.stageGraphLocked(m, stage, actions, now)
		} else {
			check.Graph.Messages = append(check.Graph.Messages, "Not evaluated: a resource would be refused in the merged world")
		}
	}
	t.mu.Unlock()
	if sim != nil {
		switch {
		case staged == nil:
			check.Simulation = failedSimulation("Not run: the staged candidate is unavailable")
		default:
			result, refusal := t.simulateCandidate(m, *sim, staged)
			if refusal != nil {
				check.Simulation = failedSimulation(refusal.Message)
			} else {
				out.simulation = &result
				check.Simulation = simulationSummary(result)
			}
		}
	}
	check.Passed = len(out.conflicts) == 0 && len(check.Resources) > 0 && !slices.ContainsFunc(check.Resources, func(c BranchResourceCheck) bool { return !c.Accepted }) &&
		check.Graph.Passed && check.Simulation.ok()
	out.check = check
	return out, nil
}

func (t *Tenant) recordBranchCheck(m platform.Member, branch build.Branch, check BranchCheck, now time.Time) *kernel.Error {
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
	checks := BranchChecks{At: now.UTC(), Conflicts: evaluation.conflicts, Resources: evaluation.check.Resources, Check: evaluation.check, Simulation: evaluation.simulation, Ready: evaluation.check.Passed}
	if err := branch.Approved(); err != nil {
		checks.Approval = err.Error()
	} else {
		checks.Approved = true
	}
	return checks, nil
}

func (t *Tenant) RecheckBranch(m platform.Member, id string, request BranchCheckRequest, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen && branch.State != build.BranchProposed || len(branch.Changes) == 0 {
		return BranchDetail{}, branchRefuse("Only an open or proposed branch with changes can be re-checked")
	}
	evaluation, refusal := t.evaluateBranch(m, branch, request.Resolutions, request.Simulation, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if refusal := t.recordBranchCheck(m, branch, evaluation.check, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) RebaseBranch(m platform.Member, id string, request BranchResolveRequest, now time.Time) (BranchDetail, []BranchResourceDiff, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, nil, refusal
	}
	if branch.State != build.BranchOpen {
		return BranchDetail{}, nil, branchRefuse("The branch is {state}; only an open branch rebases", branch.State)
	}
	plans, refusal := t.planBranch(m, branch, request.Resolutions, now)
	if refusal != nil {
		return BranchDetail{}, nil, refusal
	}
	if conflicts := conflictsOf(plans); len(conflicts) > 0 {
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
		if refusal := t.submitBranch(m, "branch:"+id+":save:"+strconv.Itoa(seq), build.BranchType+".save", id, payload, now); refusal != nil {
			return BranchDetail{}, nil, refusal
		}
	}
	if rebased, refusal := t.readBranch(m, id, now); refusal == nil && len(rebased.Changes) > 0 {
		if evaluation, refusal := t.evaluateBranch(m, rebased, nil, nil, now); refusal == nil {
			_ = t.recordBranchCheck(m, rebased, evaluation.check, now)
		}
	}
	detail, refusal := t.BranchDetail(m, id, now)
	return detail, nil, refusal
}

func (t *Tenant) ProposeBranch(m platform.Member, id string, request BranchProposeRequest, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen || len(branch.Changes) == 0 {
		return BranchDetail{}, branchRefuse("Only an open branch with changes can be proposed")
	}
	for _, reviewer := range request.Reviewers {
		member, ok := t.Member(reviewer)
		if !ok || !member.Holds(build.ID, build.Builder) {
			return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Reviewer {member} must be a builder", reviewer)
		}
	}
	evaluation, refusal := t.evaluateBranch(m, branch, request.Resolutions, request.Simulation, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if len(evaluation.conflicts) > 0 {
		return BranchDetail{}, branchRefuse("The branch conflicts with main in {count} resources; rebase it first", len(evaluation.conflicts))
	}
	if !evaluation.check.Passed {
		return BranchDetail{}, branchRefuse("The staged check of the merged definition graph failed: {message}", evaluation.check.Failure())
	}
	reviewers := request.Reviewers
	if reviewers == nil {
		reviewers = []string{}
	}
	if refusal := t.submitBranch(m, "branch:"+id+":propose:"+strconv.Itoa(len(branch.Changes)), build.BranchType+".propose", id, map[string]any{"reviewers": reviewers, "note": request.Note, "checks": evaluation.check}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) MergeBranch(m platform.Member, id string, request BranchMergeRequest, now time.Time) (BranchMergeResult, *kernel.Error) {
	var result BranchMergeResult
	if refusal := t.admits(m); refusal != nil {
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
	actions []branchAction
}

func (t *Tenant) applyBranch(m platform.Member, id string, request BranchMergeRequest, now time.Time) (mergedCandidate, []BranchResourceCheck, *kernel.Error) {
	var none mergedCandidate
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return none, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if refusal := t.recordReadRefusal(); refusal != nil {
		return none, nil, refusal
	}
	branch, refusal := t.readBranchLocked(m, id, now)
	if refusal != nil {
		return none, nil, refusal
	}
	if branch.State != build.BranchProposed {
		return none, nil, branchRefuse("The branch is {state}; only a proposed branch merges", branch.State)
	}
	if err := branch.Approved(); err != nil {
		return none, nil, branchRefuse("{reason}", err.Error())
	}
	plans, refusal := t.planBranchLocked(m, branch, request.Resolutions, now)
	if refusal != nil {
		return none, nil, refusal
	}
	if conflicts := conflictsOf(plans); len(conflicts) > 0 {
		return none, nil, branchRefuse("The branch conflicts with main in {count} resources; rebase it first", len(conflicts))
	}
	check := decodeBranchCheck(branch)
	if check == nil {
		return none, nil, branchRefuse("The proposal has no staged check; re-check the branch before merging")
	}
	if !check.Passed {
		return none, nil, branchRefuse("The last staged check failed: {message}; fix the branch and re-check it before merging", check.Failure())
	}
	if check.Base != t.branchBaseLocked() {
		return none, nil, branchRefuse("Main changed since the proposal was last checked; rebase or re-check the branch before merging")
	}
	if check.Content != branchContent(plans) {
		return none, nil, branchRefuse("The merged content differs from the checked content; re-check the branch before merging")
	}
	if request.Simulation != nil && check.Simulation == nil {
		return none, nil, branchRefuse("The last staged check did not run a simulation; re-check the branch with a simulation before merging")
	}
	actions, refusal := t.branchActions(plans)
	if refusal != nil {
		return none, nil, refusal
	}
	applied := make([]BranchResourceCheck, 0, len(actions))
	for _, item := range actions {
		applied = append(applied, BranchResourceCheck{Type: item.plan.Type, ID: item.plan.ID, RID: item.plan.RID, Action: item.action, Artifact: item.module(), Accepted: true})
	}
	revision := branch.Record.Revision
	payload := platform.Raw(map[string]any{"candidate": check.Graph.Candidate, "applied": applied, "base": check.Base})
	s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, ExpectedRevision: &revision,
		Target: &pb.EntityRef{Type: build.BranchType, Id: id}, Schema: &pb.SchemaRef{Name: build.BranchType + ".merge", Version: 1}, Payload: payload}
	s.IdempotencyKey = branchKey("branch:"+id+":merge", m.ID, s.Schema.Name, payload, &revision)
	owner := t.owner["action:"+s.Schema.Name]
	if owner == nil {
		return none, nil, unknown()
	}
	s = t.currentSubmissionSchema(owner, s)
	if refusal := t.delegatedBound(m, s); refusal != nil {
		return none, nil, refusal
	}
	before := func(draft *stagedDecision) *kernel.Error {
		for _, item := range actions {
			if item.action == "none" {
				continue
			}
			child := t.decideBranchChild(draft, m, item, "branch:"+id+":merge:"+item.plan.Type+"/"+item.plan.ID, now)
			if child.refusal != nil {
				return platform.Refuse(child.refusal.Code, "{resource} could not be applied: {message}", item.plan.Type+"/"+item.plan.ID, child.refusal.Message)
			}
		}
		return nil
	}
	if _, refusal := t.submitAcceptedWith(owner, m, s, now, false, nil, nil, nil, nil, before); refusal != nil {
		return none, nil, refusal
	}
	return mergedCandidate{id: check.Graph.Candidate, actions: actions}, applied, nil
}

func (t *Tenant) saveBranchCandidate(m platform.Member, branchID string, merged mergedCandidate, now time.Time) (string, error) {
	t.mu.Lock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		t.mu.Unlock()
		return "", fmt.Errorf("the tenant has no builder")
	}
	drafts := branchDraftSet(owner, merged.actions)
	preview, candidate, err := t.previewReleaseLocked(drafts, m)
	t.mu.Unlock()
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
