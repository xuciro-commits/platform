package branches

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
)

type Workspace struct {
	Tenant    string
	Read      func(m platform.Member, typ, id string, now time.Time) (json.RawMessage, *kernel.Error)
	List      func(m platform.Member, typ string, q platform.Query, now time.Time) ([]any, *kernel.Error)
	Fields    func(typ string) ([]platform.FieldInfo, bool)
	Resources func() []build.Resource
	Project   func(id string) (build.Application, bool)
	Role      func(m platform.Member, project string) string
	Base      func() string
	Stored    func(module string) bool
}

type SavePlan struct {
	Branch      build.Branch
	Current     MainDraft
	Op, Project string
	Spec        json.RawMessage
}

func Refuse(message string, args ...any) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, message, args...)
}

func Submission(tenant string, m platform.Member, key, schema, id string, payload any) *pb.Submission {
	raw := platform.Raw(payload)
	return &pb.Submission{TenantId: tenant, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: Key(key, m.ID, schema, raw, nil),
		Target: &pb.EntityRef{Type: build.BranchType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
}

func (w Workspace) Branch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	var branch build.Branch
	raw, refusal := w.Read(m, build.BranchType, id, now)
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

func (w Workspace) ProjectOf(typ, draft string) string {
	for _, resource := range w.Resources() {
		if resource.Type == typ && resource.Draft == draft {
			return resource.Project
		}
	}
	return ""
}

func (w Workspace) canEdit(m platform.Member, project string) bool {
	if app, ok := w.Project(project); !ok || app.Archived {
		return false
	}
	role := w.Role(m, project)
	return role == platform.ProjectOwnerRole || role == platform.ProjectEditorRole
}

func (w Workspace) fields(typ string) (map[string]bool, *kernel.Error) {
	infos, ok := w.Fields(typ)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "{type} is not an installed resource type", typ)
	}
	fields := map[string]bool{}
	for _, f := range infos {
		if len(f.Index) > 0 && f.Index[0] != 0 && !f.ReadOnly && f.AliasOf == "" && f.Name != "id" {
			fields[f.Name] = true
		}
	}
	return fields, nil
}

func (w Workspace) Main(m platform.Member, typ, id string, now time.Time) (MainDraft, *kernel.Error) {
	var out MainDraft
	if !build.BranchDraftType(typ) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{type} is not a resource draft type", typ)
	}
	fields, refusal := w.fields(typ)
	if refusal != nil {
		return out, refusal
	}
	raw, refusal := w.Read(m, typ, id, now)
	if refusal != nil {
		if refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND {
			out.RID = w.RID(typ, id, nil)
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
	out.RID = w.RID(typ, id, spec)
	if !archived {
		out.Spec, _ = json.Marshal(spec)
	}
	return out, nil
}

func (w Workspace) RID(typ, id string, spec map[string]json.RawMessage) string {
	name := id
	var named string
	if json.Unmarshal(spec["name"], &named) == nil && named != "" {
		name = named
	}
	ref, err := build.BranchResource(typ, name)
	if err != nil {
		return ""
	}
	return platform.ResourceID(w.Tenant, ref)
}

func summary(branch build.Branch) BranchDetail {
	return BranchDetail{ID: branch.ID, Name: branch.Name, Title: branch.Title, Description: branch.Description, State: branch.State, Revision: branch.Revision,
		Created: branch.Created, Changed: branch.Changed, Changes: len(branch.Changes), Resources: []BranchResourceStatus{}, Proposal: branch.Proposal, Check: DecodeCheck(branch), Merge: branch.Merge}
}

func (w Workspace) Detail(m platform.Member, branch build.Branch, now time.Time) (BranchDetail, *kernel.Error) {
	detail := summary(branch)
	if detail.Check != nil && (branch.State == build.BranchOpen || branch.State == build.BranchProposed) {
		detail.CheckStale = detail.Check.Base != w.Base()
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
		status := BranchResourceStatus{Type: typ, ID: id, Op: latest.Op, Changes: count, LastSeq: latest.Seq, LastAt: latest.At, LastBy: latest.By}
		if branch.State == build.BranchOpen || branch.State == build.BranchProposed {
			current, refusal := w.Main(m, typ, id, now)
			if refusal != nil {
				return detail, refusal
			}
			status.RID = current.RID
			base, _ := branch.Base(typ, id)
			diff, err := Diff(base, current.Spec)
			status.Drifted = err == nil && len(diff) > 0
		} else {
			status.RID = w.RID(typ, id, nil)
		}
		detail.Resources = append(detail.Resources, status)
	}
	return detail, nil
}

func (w Workspace) DetailOf(m platform.Member, id string, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := w.Branch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	return w.Detail(m, branch, now)
}

func (w Workspace) Branches(m platform.Member, now time.Time) ([]BranchDetail, *kernel.Error) {
	records, refusal := w.List(m, build.BranchType, platform.Query{Limit: 500, Sort: []string{"id"}}, now)
	if refusal != nil {
		return nil, refusal
	}
	out := []BranchDetail{}
	for _, record := range records {
		raw, _ := json.Marshal(record)
		var header struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &header) != nil {
			continue
		}
		branch, refusal := w.Branch(m, header.ID, now)
		if refusal != nil {
			return nil, refusal
		}
		detail := summary(branch)
		for _, key := range branch.Resources() {
			typ, rid, _ := strings.Cut(key, "/")
			latest, _ := branch.Latest(typ, rid)
			detail.Resources = append(detail.Resources, BranchResourceStatus{Type: typ, ID: rid, Op: latest.Op, LastSeq: latest.Seq, LastAt: latest.At, LastBy: latest.By})
		}
		out = append(out, detail)
	}
	return out, nil
}

func (w Workspace) Changes(m platform.Member, id string, specs bool, now time.Time) ([]BranchChangeEntry, *kernel.Error) {
	branch, refusal := w.Branch(m, id, now)
	if refusal != nil {
		return nil, refusal
	}
	out := make([]BranchChangeEntry, 0, len(branch.Changes))
	for _, change := range branch.Changes {
		entry := BranchChangeEntry{Seq: change.Seq, At: change.At, By: change.By, Type: change.Type, ID: change.ID, Project: change.Project, Op: change.Op, Rebased: change.Rebased, RID: w.RID(change.Type, change.ID, nil)}
		if specs {
			entry.Base, entry.Spec = change.Base, change.Spec
		}
		out = append(out, entry)
	}
	return out, nil
}

func (w Workspace) PrepareSave(m platform.Member, id, typ, draft string, request BranchSaveRequest, now time.Time) (SavePlan, *kernel.Error) {
	branch, refusal := w.Branch(m, id, now)
	if refusal != nil {
		return SavePlan{}, refusal
	}
	if branch.State != build.BranchOpen {
		return SavePlan{}, Refuse("The branch is {state}; only an open branch takes saves", branch.State)
	}
	current, refusal := w.Main(m, typ, draft, now)
	if refusal != nil {
		return SavePlan{}, refusal
	}
	previous, touched := branch.Latest(typ, draft)
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
			return SavePlan{}, Refuse("{resource} already exists; save it as an update", typ+"/"+draft)
		}
	case "update", "delete":
		if !existsOnBranch {
			return SavePlan{}, Refuse("{resource} does not exist; save it as a create", typ+"/"+draft)
		}
	default:
		return SavePlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A branch change creates, updates or deletes")
	}
	if op != "delete" {
		fields, refusal := w.fields(typ)
		if refusal != nil {
			return SavePlan{}, refusal
		}
		var spec map[string]json.RawMessage
		if json.Unmarshal(request.Spec, &spec) != nil || spec == nil {
			return SavePlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A saved spec is one JSON object")
		}
		for name := range spec {
			if !fields[name] && !(typ == build.CodeType && name == CodeBuildKey) {
				return SavePlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is not an authored field of {type}", name, typ)
			}
		}
		if typ == build.CodeType {
			delete(spec, CodeBuildKey)
			if kept := KeptCodeBuild(previous, touched, spec); kept != nil {
				spec[CodeBuildKey] = kept
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
		authoritative := w.ProjectOf(typ, draft)
		if authoritative == "" {
			authoritative = previous.Project
		}
		if authoritative == "" {
			return SavePlan{}, Refuse("The resource has no authoritative project")
		}
		if project != "" && project != authoritative {
			return SavePlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A resource stays in the project where it is registered")
		}
		project = authoritative
	}
	if project == "" {
		return SavePlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the project that owns this resource")
	}
	if previousProject := previous.Project; touched && previousProject != "" && previousProject != project {
		return SavePlan{}, Refuse("A resource keeps the project recorded by its first branch change")
	}
	if op == "create" && typ == build.AppType {
		if !m.Holds("platform", "admin") && !build.HoldsIndependentRole(m, build.Builder) {
			return SavePlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Creating a project needs an independent builder")
		}
	} else if !w.canEdit(m, project) {
		return SavePlan{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Editing this resource requires its project")
	}
	return SavePlan{Branch: branch, Current: current, Op: op, Project: project, Spec: request.Spec}, nil
}

func (p SavePlan) Save(tenant string, m platform.Member, typ, draft string) *pb.Submission {
	previous, touched := p.Branch.Latest(typ, draft)
	if touched && previous.Op == p.Op && !previous.Rebased && SameJSON(previous.Spec, p.Spec) {
		return nil
	}
	payload := map[string]any{"type": typ, "id": draft, "op": p.Op, "project": p.Project}
	if p.Op != "delete" {
		payload["spec"] = p.Spec
	}
	if !touched {
		if p.Current.Exists {
			payload["base"] = p.Current.Spec
		} else {
			payload["base"] = nil
		}
	}
	return Submission(tenant, m, "branch:"+p.Branch.ID+":save:"+strconv.Itoa(len(p.Branch.Changes)+1), build.BranchType+".save", p.Branch.ID, payload)
}

func (w Workspace) Resource(m platform.Member, id, typ, draft string, now time.Time) (BranchResourceView, *kernel.Error) {
	branch, refusal := w.Branch(m, id, now)
	if refusal != nil {
		return BranchResourceView{}, refusal
	}
	latest, touched := branch.Latest(typ, draft)
	current, refusal := w.Main(m, typ, draft, now)
	if refusal != nil {
		return BranchResourceView{}, refusal
	}
	if !touched {
		return BranchResourceView{Type: typ, ID: draft, Project: w.ProjectOf(typ, draft), RID: current.RID, Main: current.Spec, Branch: current.Spec}, nil
	}
	base, _ := branch.Base(typ, draft)
	spec, _ := EffectiveSpec(branch, typ, draft)
	project := latest.Project
	if project == "" {
		project = w.ProjectOf(typ, draft)
	}
	return BranchResourceView{Type: typ, ID: draft, Project: project, RID: current.RID, Op: latest.Op, Base: base, Main: current.Spec, Branch: spec}, nil
}

func (w Workspace) Plan(m platform.Member, branch build.Branch, resolutions []BranchResolution, now time.Time) ([]ResourcePlan, *kernel.Error) {
	var plans []ResourcePlan
	for _, key := range branch.Resources() {
		typ, id, _ := strings.Cut(key, "/")
		latest, _ := branch.Latest(typ, id)
		current, refusal := w.Main(m, typ, id, now)
		if refusal != nil {
			return nil, refusal
		}
		base, _ := branch.Base(typ, id)
		spec, _ := EffectiveSpec(branch, typ, id)
		merged, conflicts, err := Merge(base, current.Spec, spec, ResolutionsFor(resolutions, typ, id))
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The branch specs are unreadable: {error}", err.Error())
		}
		branchChanges, _ := Diff(base, spec)
		mainChanges, _ := Diff(base, current.Spec)
		if branchChanges == nil {
			branchChanges = []PathChange{}
		}
		if mainChanges == nil {
			mainChanges = []PathChange{}
		}
		if conflicts == nil {
			conflicts = []PathConflict{}
		}
		project := latest.Project
		if project == "" {
			project = w.ProjectOf(typ, id)
		}
		plans = append(plans, ResourcePlan{Type: typ, ID: id, RID: current.RID, Project: project, Op: latest.Op, Base: base, Main: current, Branch: spec, Merged: merged,
			Diff: BranchResourceDiff{Type: typ, ID: id, RID: current.RID, Op: latest.Op, Branch: branchChanges, Main: mainChanges, Conflicts: conflicts, Merged: merged}})
	}
	return plans, nil
}

func (w Workspace) BranchDiff(m platform.Member, id string, now time.Time) (BranchDiff, *kernel.Error) {
	branch, refusal := w.Branch(m, id, now)
	if refusal != nil {
		return BranchDiff{}, refusal
	}
	plans, refusal := w.Plan(m, branch, nil, now)
	if refusal != nil {
		return BranchDiff{}, refusal
	}
	out := BranchDiff{Resources: []BranchResourceDiff{}}
	for _, plan := range plans {
		out.Resources = append(out.Resources, plan.Diff)
		out.Conflicts += len(plan.Diff.Conflicts)
	}
	return out, nil
}

func (w Workspace) splitCodeBuild(plan ResourcePlan) (json.RawMessage, json.RawMessage) {
	var spec map[string]json.RawMessage
	if json.Unmarshal(plan.Merged, &spec) != nil || spec[CodeBuildKey] == nil {
		return plan.Merged, nil
	}
	raw := spec[CodeBuildKey]
	delete(spec, CodeBuildKey)
	authored, _ := json.Marshal(spec)
	var artifact build.CodeArtifact
	if json.Unmarshal(raw, &artifact) != nil || !artifact.Matches(authored) || !w.Stored(artifact.Module) || artifact.Module == plan.Main.Module && artifact.BuildHash == plan.Main.BuildHash {
		return authored, nil
	}
	return authored, raw
}

func (w Workspace) Actions(plans []ResourcePlan) ([]Action, *kernel.Error) {
	var actions []Action
	for _, plan := range plans {
		item := Action{Plan: plan, Action: "none"}
		merged := plan.Merged
		if plan.Type == build.CodeType && merged != nil {
			merged, item.Artifact = w.splitCodeBuild(plan)
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
	slices.SortStableFunc(actions, func(a, b Action) int {
		order := func(x Action) int {
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
