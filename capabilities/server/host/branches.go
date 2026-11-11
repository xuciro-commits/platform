package host

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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

func branchRefuse(message string, args ...any) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, message, args...)
}

func (t *Tenant) branchCaller(m platform.Member) platform.Caller {
	return platform.NewCaller(runtime{t: t, scoped: true}, m, build.ID, false, false)
}

func (t *Tenant) readBranch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (build.Branch, *kernel.Error) { return t.ReadBranch(m, id, now) })
}

func (t *Tenant) ReadBranch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	var branch build.Branch
	raw, refusal := t.readRecordFrom(t.Projection, t.branchCaller(m), build.BranchType, id, now)
	if refusal != nil {
		return branch, refusal
	}
	if err := json.Unmarshal(raw, &branch); err != nil || branch.Changes == nil {
		if err != nil {
			return branch, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The branch is unavailable")
		}
		branch.Changes = []build.BranchChange{}
	}
	return branch, nil
}

func (t *Tenant) resourceProject(typ, draft string) string {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	resources := t.Projection.Type(build.ResourceType)
	if resources == nil {
		return ""
	}
	for _, row := range resources.Rows().All() {
		resource := row.Value.Interface().(build.Resource)
		if resource.Type == typ && resource.Draft == draft {
			return resource.Project
		}
	}
	return ""
}

func (t *Tenant) canEditProjectResource(m platform.Member, project string) bool {
	t.Projection.Lock()
	apps := t.Projection.Type(build.AppType)
	exists := false
	if apps != nil {
		if row := apps.Rows().Get(project); row != nil {
			app := row.Value.Interface().(build.Application)
			exists = !app.Archived
		}
	}
	t.Projection.Unlock()
	if !exists {
		return false
	}
	role := t.ResourceRole(m, project)
	return role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole
}

func (t *Tenant) draftFields(typ string) (map[string]bool, *kernel.Error) {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	et := t.Projection.Type(typ)
	if et == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{type} is not an installed resource type", typ)
	}
	fields := map[string]bool{}
	for _, f := range et.Info().Fields {
		if len(f.Index) > 0 && f.Index[0] != 0 && !f.ReadOnly && f.AliasOf == "" && f.Name != "id" {
			fields[f.Name] = true
		}
	}
	return fields, nil
}

func (t *Tenant) mainDraftOf(m platform.Member, typ, id string, now time.Time) (branches.MainDraft, *kernel.Error) {
	var out branches.MainDraft
	if !build.BranchDraftType(typ) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{type} is not a resource draft type", typ)
	}
	fields, refusal := t.draftFields(typ)
	if refusal != nil {
		return out, refusal
	}
	raw, refusal := t.readRecordFrom(t.Projection, t.branchCaller(m), typ, id, now)
	if refusal != nil {
		if refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			out.RID = t.branchRID(typ, id, nil)
			return out, nil
		}
		return out, refusal
	}
	var record map[string]json.RawMessage
	if json.Unmarshal(raw, &record) != nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The draft is unavailable")
	}
	var archived bool
	_ = json.Unmarshal(record["archived"], &archived)
	var revision uint32
	_ = json.Unmarshal(record["revision"], &revision)
	spec := map[string]json.RawMessage{}
	for name, value := range record {
		if fields[name] {
			spec[name] = value
		}
	}
	out.Revision = revision
	_ = json.Unmarshal(record["module"], &out.Module)
	_ = json.Unmarshal(record["buildHash"], &out.BuildHash)
	out.Exists = !archived
	out.RID = t.branchRID(typ, id, spec)
	if !archived {
		out.Spec, _ = json.Marshal(spec)
	}
	return out, nil
}

func (t *Tenant) branchRID(typ, id string, spec map[string]json.RawMessage) string {
	name := id
	var named string
	if json.Unmarshal(spec["name"], &named) == nil && named != "" {
		name = named
	}
	ref, err := build.BranchResource(typ, name)
	if err != nil {
		return ""
	}
	return platform.ResourceID(t.ID, ref)
}

func (t *Tenant) branchDetail(m platform.Member, branch build.Branch, now time.Time) (branches.BranchDetail, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchDetail, *kernel.Error) { return t.branchDetailRead(m, branch, now) })
}

func (t *Tenant) branchDetailRead(m platform.Member, branch build.Branch, now time.Time) (branches.BranchDetail, *kernel.Error) {
	detail := branches.BranchDetail{ID: branch.ID, Name: branch.Name, Title: branch.Title, Description: branch.Description, State: branch.State, Revision: branch.Revision,
		Created: branch.Created, Changed: branch.Changed, Changes: len(branch.Changes), Resources: []branches.BranchResourceStatus{}, Proposal: branch.Proposal, Check: branches.DecodeCheck(branch), Merge: branch.Merge}
	if detail.Check != nil && (branch.State == build.BranchOpen || branch.State == build.BranchProposed) {
		detail.CheckStale = detail.Check.Base != t.branchBaseLocked()
	}
	for _, key := range branch.Resources() {
		typ, id, _ := strings.Cut(key, "/")
		latest, _ := branch.Latest(typ, id)
		count := 0
		for _, change := range branch.Changes {
			if change.Type == typ && change.ID == id {
				count++
			}
		}
		status := branches.BranchResourceStatus{Type: typ, ID: id, Op: latest.Op, Changes: count, LastSeq: latest.Seq, LastAt: latest.At, LastBy: latest.By}
		if branch.State == build.BranchOpen || branch.State == build.BranchProposed {
			current, refusal := t.mainDraftOf(m, typ, id, now)
			if refusal != nil {
				return detail, refusal
			}
			status.RID = current.RID
			base, _ := branch.Base(typ, id)
			diff, err := branches.Diff(base, current.Spec)
			status.Drifted = err == nil && len(diff) > 0
		} else {
			status.RID = t.branchRID(typ, id, nil)
		}
		detail.Resources = append(detail.Resources, status)
	}
	return detail, nil
}

func (t *Tenant) ListBranches(m platform.Member, now time.Time) ([]branches.BranchDetail, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]branches.BranchDetail, *kernel.Error) { return t.scopedListBranches(m, now) })
}

func (t *Tenant) scopedListBranches(m platform.Member, now time.Time) ([]branches.BranchDetail, *kernel.Error) {
	page, refusal := t.recordsFrom(t.Projection, m, build.BranchType, platform.Query{Limit: 500, Sort: []string{"id"}}, now)
	var ids []string
	if refusal == nil {
		for _, record := range page.Records {
			raw, _ := json.Marshal(record)
			var header struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw, &header) == nil {
				ids = append(ids, header.ID)
			}
		}
	}
	if refusal != nil {
		return nil, refusal
	}
	out := []branches.BranchDetail{}
	for _, id := range ids {
		branch, refusal := t.readBranch(m, id, now)
		if refusal != nil {
			return nil, refusal
		}
		detail := branches.BranchDetail{ID: branch.ID, Name: branch.Name, Title: branch.Title, Description: branch.Description, State: branch.State, Revision: branch.Revision,
			Created: branch.Created, Changed: branch.Changed, Changes: len(branch.Changes), Resources: []branches.BranchResourceStatus{}, Proposal: branch.Proposal, Check: branches.DecodeCheck(branch), Merge: branch.Merge}
		for _, key := range branch.Resources() {
			typ, rid, _ := strings.Cut(key, "/")
			latest, _ := branch.Latest(typ, rid)
			detail.Resources = append(detail.Resources, branches.BranchResourceStatus{Type: typ, ID: rid, Op: latest.Op, LastSeq: latest.Seq, LastAt: latest.At, LastBy: latest.By})
		}
		out = append(out, detail)
	}
	return out, nil
}

func (t *Tenant) submitBranch(m platform.Member, key, schema, id string, payload any, now time.Time) *kernel.Error {
	raw := platform.Raw(payload)
	_, refusal := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: branches.Key(key, m.ID, schema, raw, nil),
		Target: &pb.EntityRef{Type: build.BranchType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
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
	return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchDetail, *kernel.Error) { return t.branchDetailByIDRead(m, id, now) })
}

func (t *Tenant) branchDetailByIDRead(m platform.Member, id string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.branchDetail(m, branch, now)
}

func (t *Tenant) BranchChanges(m platform.Member, id string, specs bool, now time.Time) ([]branches.BranchChangeEntry, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]branches.BranchChangeEntry, *kernel.Error) {
		return t.scopedBranchChanges(m, id, specs, now)
	})
}

func (t *Tenant) scopedBranchChanges(m platform.Member, id string, specs bool, now time.Time) ([]branches.BranchChangeEntry, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return nil, refusal
	}
	out := make([]branches.BranchChangeEntry, 0, len(branch.Changes))
	for _, change := range branch.Changes {
		entry := branches.BranchChangeEntry{Seq: change.Seq, At: change.At, By: change.By, Type: change.Type, ID: change.ID, Project: change.Project, Op: change.Op, Rebased: change.Rebased, RID: t.branchRID(change.Type, change.ID, nil)}
		if specs {
			entry.Base, entry.Spec = change.Base, change.Spec
		}
		out = append(out, entry)
	}
	return out, nil
}

type branchSavePreparation struct {
	branch      build.Branch
	current     branches.MainDraft
	op, project string
	spec        json.RawMessage
}

func (t *Tenant) SaveBranchResource(m platform.Member, id, typ, draft string, request branches.BranchSaveRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	plan, refusal := recordRead(t, context.Background(), func(t *Tenant) (branchSavePreparation, *kernel.Error) {
		return t.prepareBranchSave(m, id, typ, draft, request, now)
	})
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.appendBranchSave(m, plan.branch, plan.current, typ, draft, plan.op, plan.project, plan.spec, now)
}

func (t *Tenant) prepareBranchSave(m platform.Member, id, typ, draft string, request branches.BranchSaveRequest, now time.Time) (branchSavePreparation, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branchSavePreparation{}, refusal
	}
	if branch.State != build.BranchOpen {
		return branchSavePreparation{}, branchRefuse("The branch is {state}; only an open branch takes saves", branch.State)
	}
	current, refusal := t.mainDraftOf(m, typ, draft, now)
	if refusal != nil {
		return branchSavePreparation{}, refusal
	}
	_, touched := branch.Latest(typ, draft)
	previous, _ := branch.Latest(typ, draft)
	op := request.Op
	if op == "" {
		op = "update"
		if !current.Exists && (!touched || previous.Op == "create") {
			op = "create"
		}
	}
	existsOnBranch := current.Exists
	if touched {
		existsOnBranch = previous.Op != "delete"
	}
	switch op {
	case "create":
		if existsOnBranch {
			return branchSavePreparation{}, branchRefuse("{resource} already exists; save it as an update", typ+"/"+draft)
		}
	case "update", "delete":
		if !existsOnBranch {
			return branchSavePreparation{}, branchRefuse("{resource} does not exist; save it as a create", typ+"/"+draft)
		}
	default:
		return branchSavePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A branch change creates, updates or deletes")
	}
	if op != "delete" {
		fields, refusal := t.draftFields(typ)
		if refusal != nil {
			return branchSavePreparation{}, refusal
		}
		var spec map[string]json.RawMessage
		if json.Unmarshal(request.Spec, &spec) != nil || spec == nil {
			return branchSavePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A saved spec is one JSON object")
		}
		for name := range spec {
			if !fields[name] && !(typ == build.CodeType && name == branches.CodeBuildKey) {
				return branchSavePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is not an authored field of {type}", name, typ)
			}
		}
		if typ == build.CodeType {
			delete(spec, branches.CodeBuildKey)
			if kept := branches.KeptCodeBuild(previous, touched, spec); kept != nil {
				spec[branches.CodeBuildKey] = kept
			}
			request.Spec, _ = json.Marshal(spec)
		}
	}
	project := request.Project
	if op == "create" {
		if typ == build.AppType {
			project = draft
		}
	} else {
		authoritative := t.resourceProject(typ, draft)
		if authoritative == "" {
			authoritative = previous.Project
		}
		if authoritative == "" {
			return branchSavePreparation{}, branchRefuse("The resource has no authoritative project")
		}
		if project != "" && project != authoritative {
			return branchSavePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A resource stays in the project where it is registered")
		}
		project = authoritative
	}
	if project == "" {
		return branchSavePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the project that owns this resource")
	}
	if previousProject := previous.Project; touched && previousProject != "" && previousProject != project {
		return branchSavePreparation{}, branchRefuse("A resource keeps the project recorded by its first branch change")
	}
	if op == "create" && typ == build.AppType {
		if !m.Holds("platform", "admin") && !build.HoldsIndependentRole(m, build.Builder) {
			return branchSavePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Creating a project needs an independent builder")
		}
	} else if !t.canEditProjectResource(m, project) {
		return branchSavePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Editing this resource requires its project")
	}
	return branchSavePreparation{branch: branch, current: current, op: op, project: project, spec: request.Spec}, nil
}

func (t *Tenant) appendBranchSave(m platform.Member, branch build.Branch, current branches.MainDraft, typ, draft, op, project string, spec json.RawMessage, now time.Time) (branches.BranchDetail, *kernel.Error) {
	previous, touched := branch.Latest(typ, draft)
	if touched && previous.Op == op && !previous.Rebased && branches.SameJSON(previous.Spec, spec) {
		return t.branchDetail(m, branch, now)
	}
	payload := map[string]any{"type": typ, "id": draft, "op": op, "project": project}
	if op != "delete" {
		payload["spec"] = spec
	}
	if !touched {
		if current.Exists {
			payload["base"] = current.Spec
		} else {
			payload["base"] = nil
		}
	}
	if refusal := t.submitBranch(m, "branch:"+branch.ID+":save:"+strconv.Itoa(len(branch.Changes)+1), build.BranchType+".save", branch.ID, payload, now); refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.BranchDetail(m, branch.ID, now)
}

func (t *Tenant) BranchResource(m platform.Member, id, typ, draft string, now time.Time) (branches.BranchResourceView, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchResourceView, *kernel.Error) {
		return t.scopedBranchResource(m, id, typ, draft, now)
	})
}

func (t *Tenant) scopedBranchResource(m platform.Member, id, typ, draft string, now time.Time) (branches.BranchResourceView, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchResourceView{}, refusal
	}
	latest, touched := branch.Latest(typ, draft)
	current, refusal := t.mainDraftOf(m, typ, draft, now)
	if refusal != nil {
		return branches.BranchResourceView{}, refusal
	}
	if !touched {
		return branches.BranchResourceView{Type: typ, ID: draft, Project: t.resourceProject(typ, draft), RID: current.RID, Main: current.Spec, Branch: current.Spec}, nil
	}
	base, _ := branch.Base(typ, draft)
	spec, _ := branches.EffectiveSpec(branch, typ, draft)
	project := latest.Project
	if project == "" {
		project = t.resourceProject(typ, draft)
	}
	return branches.BranchResourceView{Type: typ, ID: draft, Project: project, RID: current.RID, Op: latest.Op, Base: base, Main: current.Spec, Branch: spec}, nil
}

func (t *Tenant) planBranch(m platform.Member, branch build.Branch, resolutions []branches.BranchResolution, now time.Time) ([]branches.ResourcePlan, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]branches.ResourcePlan, *kernel.Error) {
		return t.planBranchLocked(m, branch, resolutions, now)
	})
}

func (t *Tenant) planBranchLocked(m platform.Member, branch build.Branch, resolutions []branches.BranchResolution, now time.Time) ([]branches.ResourcePlan, *kernel.Error) {
	var plans []branches.ResourcePlan
	for _, key := range branch.Resources() {
		typ, id, _ := strings.Cut(key, "/")
		latest, _ := branch.Latest(typ, id)
		current, refusal := t.mainDraftOf(m, typ, id, now)
		if refusal != nil {
			return nil, refusal
		}
		base, _ := branch.Base(typ, id)
		spec, _ := branches.EffectiveSpec(branch, typ, id)
		merged, conflicts, err := branches.Merge(base, current.Spec, spec, branches.ResolutionsFor(resolutions, typ, id))
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The branch specs are unreadable: {error}", err.Error())
		}
		branchChanges, _ := branches.Diff(base, spec)
		mainChanges, _ := branches.Diff(base, current.Spec)
		if branchChanges == nil {
			branchChanges = []branches.PathChange{}
		}
		if mainChanges == nil {
			mainChanges = []branches.PathChange{}
		}
		if conflicts == nil {
			conflicts = []branches.PathConflict{}
		}
		project := latest.Project
		if project == "" {
			project = t.resourceProject(typ, id)
		}
		plans = append(plans, branches.ResourcePlan{Type: typ, ID: id, RID: current.RID, Project: project, Op: latest.Op, Base: base, Main: current, Branch: spec, Merged: merged,
			Diff: branches.BranchResourceDiff{Type: typ, ID: id, RID: current.RID, Op: latest.Op, Branch: branchChanges, Main: mainChanges, Conflicts: conflicts, Merged: merged}})
	}
	return plans, nil
}

func (t *Tenant) BranchDiff(m platform.Member, id string, now time.Time) (branches.BranchDiff, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (branches.BranchDiff, *kernel.Error) { return t.scopedBranchDiff(m, id, now) })
}

func (t *Tenant) scopedBranchDiff(m platform.Member, id string, now time.Time) (branches.BranchDiff, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDiff{}, refusal
	}
	plans, refusal := t.planBranch(m, branch, nil, now)
	if refusal != nil {
		return branches.BranchDiff{}, refusal
	}
	out := branches.BranchDiff{Resources: []branches.BranchResourceDiff{}}
	for _, plan := range plans {
		out.Resources = append(out.Resources, plan.Diff)
		out.Conflicts += len(plan.Diff.Conflicts)
	}
	return out, nil
}

func (t *Tenant) splitCodeBuild(plan branches.ResourcePlan) (json.RawMessage, json.RawMessage) {
	var spec map[string]json.RawMessage
	if json.Unmarshal(plan.Merged, &spec) != nil || spec[branches.CodeBuildKey] == nil {
		return plan.Merged, nil
	}
	raw := spec[branches.CodeBuildKey]
	delete(spec, branches.CodeBuildKey)
	authored, _ := json.Marshal(spec)
	var artifact build.CodeArtifact
	if json.Unmarshal(raw, &artifact) != nil || !artifact.Matches(authored) || !t.artifactStored(context.Background(), artifact.Module) || artifact.Module == plan.Main.Module && artifact.BuildHash == plan.Main.BuildHash {
		return authored, nil
	}
	return authored, raw
}

func (t *Tenant) branchActions(plans []branches.ResourcePlan) ([]branches.Action, *kernel.Error) {
	var actions []branches.Action
	for _, plan := range plans {
		item := branches.Action{Plan: plan, Action: "none"}
		merged := plan.Merged
		if plan.Type == build.CodeType && merged != nil {
			merged, item.Artifact = t.splitCodeBuild(plan)
		}
		switch {
		case merged == nil && plan.Main.Exists:
			item.Action, item.Schema = "archive", plan.Type+".archive"
			item.Payload = map[string]json.RawMessage{}
		case merged != nil && !plan.Main.Exists:
			if plan.Project == "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The branch has no project for {resource}", plan.Type+"/"+plan.ID)
			}
			item.Action, item.Schema = "create", plan.Type+".create"
			if json.Unmarshal(merged, &item.Payload) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The merged spec is unreadable")
			}
			item.Payload["project"] = platform.Raw(plan.Project)
		case merged != nil:
			var current map[string]json.RawMessage
			var mergedFields map[string]json.RawMessage
			if json.Unmarshal(merged, &mergedFields) != nil || json.Unmarshal(plan.Main.Spec, &current) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The merged spec is unreadable")
			}
			changed := map[string]json.RawMessage{}
			for name, value := range mergedFields {
				if previous, present := current[name]; !present || string(previous) != string(value) {
					changed[name] = value
				}
			}
			for name := range current {
				if _, kept := mergedFields[name]; !kept {
					changed[name] = json.RawMessage("null")
				}
			}
			if len(changed) > 0 {
				item.Action, item.Schema, item.Payload = "edit", plan.Type+".edit", changed
			} else if item.Artifact != nil {
				item.Action = "edit"
			}
		}
		if item.Action == "archive" {
			item.Artifact = nil
		}
		actions = append(actions, item)
	}
	slices.SortStableFunc(actions, func(a, b branches.Action) int {
		order := func(x branches.Action) int {
			switch {
			case x.Action == "archive":
				return 3
			case x.Plan.Type == build.AppType:
				return 2
			case x.Action == "edit":
				return 1
			}
			return 0
		}
		return order(a) - order(b)
	})
	return actions, nil
}

func (t *Tenant) ReviewBranch(m platform.Member, id string, request branches.BranchReviewRequest, now time.Time) (branches.BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	reviews := 0
	if branch.Proposal != nil {
		reviews = len(branch.Proposal.Reviews)
	}
	if refusal := t.submitBranch(m, "branch:"+id+":review:"+strconv.Itoa(len(branch.Changes))+":"+strconv.Itoa(reviews+1), build.BranchType+".review", id, map[string]any{"decision": request.Decision, "note": request.Note}, now); refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) WithdrawBranch(m platform.Member, id string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	if refusal := t.submitBranch(m, "branch:"+id+":withdraw:"+strconv.Itoa(int(branch.Record.Revision)), build.BranchType+".withdraw", id, map[string]any{}, now); refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) CloseBranch(m platform.Member, id string, now time.Time) (branches.BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	if refusal := t.submitBranch(m, "branch:"+id+":close:"+strconv.Itoa(int(branch.Record.Revision)), build.BranchType+".close", id, map[string]any{}, now); refusal != nil {
		return branches.BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}
