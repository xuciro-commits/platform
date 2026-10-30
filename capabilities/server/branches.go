package platformserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/branches"
	"platformserver/studio/build"
	trialowner "platformserver/studio/trials"
)

type BranchResourceStatus struct {
	Type    string    `json:"type"`
	ID      string    `json:"id"`
	RID     string    `json:"rid,omitempty"`
	Op      string    `json:"op" enum:"create,update,delete"`
	Changes int       `json:"changes"`
	LastSeq int       `json:"lastSeq"`
	LastAt  time.Time `json:"lastAt"`
	LastBy  string    `json:"lastBy"`
	Drifted bool      `json:"drifted,omitempty"`
}

type BranchDetail struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Title       string                 `json:"title"`
	Description string                 `json:"description,omitempty"`
	State       string                 `json:"state" enum:"open,proposed,merged,closed"`
	Revision    uint32                 `json:"revision"`
	Created     platform.Stamp         `json:"created"`
	Changed     platform.Stamp         `json:"changed"`
	Changes     int                    `json:"changes"`
	Resources   []BranchResourceStatus `json:"resources"`
	Proposal    *build.BranchProposal  `json:"proposal,omitempty"`
	Merge       *build.BranchMerge     `json:"merge,omitempty"`
}

type BranchChangeEntry struct {
	Seq     int             `json:"seq"`
	At      time.Time       `json:"at"`
	By      string          `json:"by"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	RID     string          `json:"rid,omitempty"`
	Op      string          `json:"op" enum:"create,update,delete"`
	Rebased bool            `json:"rebased,omitempty"`
	Base    json.RawMessage `json:"base,omitempty"`
	Spec    json.RawMessage `json:"spec,omitempty"`
}

type BranchResourceView struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	RID    string          `json:"rid,omitempty"`
	Op     string          `json:"op,omitempty" enum:"create,update,delete"`
	Base   json.RawMessage `json:"base,omitempty"`
	Main   json.RawMessage `json:"main,omitempty"`
	Branch json.RawMessage `json:"branch,omitempty"`
}

type BranchResourceDiff struct {
	Type      string                  `json:"type"`
	ID        string                  `json:"id"`
	RID       string                  `json:"rid,omitempty"`
	Op        string                  `json:"op" enum:"create,update,delete"`
	Branch    []branches.PathChange   `json:"branch"`
	Main      []branches.PathChange   `json:"main"`
	Conflicts []branches.PathConflict `json:"conflicts"`
	Merged    json.RawMessage         `json:"merged,omitempty"`
}

type BranchDiff struct {
	Resources []BranchResourceDiff `json:"resources"`
	Conflicts int                  `json:"conflicts"`
}

type BranchResourceCheck struct {
	Type     string                `json:"type"`
	ID       string                `json:"id"`
	RID      string                `json:"rid,omitempty"`
	Action   string                `json:"action" enum:"create,edit,archive,none"`
	Accepted bool                  `json:"accepted"`
	Message  string                `json:"message,omitempty"`
	Code     string                `json:"code,omitempty"`
	Issues   []platform.FieldIssue `json:"issues,omitempty"`
}

type BranchChecks struct {
	At        time.Time             `json:"at"`
	Conflicts []BranchResourceDiff  `json:"conflicts"`
	Resources []BranchResourceCheck `json:"resources"`
	Approved  bool                  `json:"approved"`
	Approval  string                `json:"approval,omitempty"`
	Ready     bool                  `json:"ready"`
}

type BranchMergeResult struct {
	Branch    BranchDetail                    `json:"branch"`
	Candidate string                          `json:"candidate,omitempty"`
	Applied   []BranchResourceCheck           `json:"applied"`
	Simulated *trialowner.CandidateSimulation `json:"simulation,omitempty"`
}

type BranchCreateRequest struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type BranchSaveRequest struct {
	Op   string          `json:"op,omitempty" enum:"create,update,delete"`
	Spec json.RawMessage `json:"spec,omitempty"`
}

type BranchResolveRequest struct {
	Resolutions []BranchResolution `json:"resolutions,omitempty"`
}

type BranchResolution struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Path string `json:"path"`
	Take string `json:"take" enum:"branch,main"`
}

type BranchProposeRequest struct {
	Reviewers   []string           `json:"reviewers,omitempty"`
	Note        string             `json:"note,omitempty"`
	Resolutions []BranchResolution `json:"resolutions,omitempty"`
}

type BranchReviewRequest struct {
	Decision string `json:"decision" enum:"approve,changes"`
	Note     string `json:"note,omitempty"`
}

type BranchMergeRequest struct {
	Resolutions []BranchResolution                     `json:"resolutions,omitempty"`
	Simulation  *trialowner.CandidateSimulationRequest `json:"simulation,omitempty"`
}

type BranchConflictError struct {
	Conflicts []BranchResourceDiff `json:"conflicts"`
}

func branchRefuse(message string, args ...any) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, message, args...)
}

func (t *Tenant) branchCaller(m platform.Member) platform.Caller {
	return platform.NewCaller(runtime{t}, m, build.ID, false, false)
}

func (t *Tenant) readBranch(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.readBranchLocked(m, id, now)
}

func (t *Tenant) readBranchLocked(m platform.Member, id string, now time.Time) (build.Branch, *kernel.Error) {
	var branch build.Branch
	raw, refusal := t.readRecordFrom(t.records, t.branchCaller(m), build.BranchType, id, now)
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

type mainDraft struct {
	Spec     json.RawMessage
	Revision uint32
	Exists   bool
	RID      string
}

func (t *Tenant) draftFields(typ string) (map[string]bool, *kernel.Error) {
	t.records.Lock()
	defer t.records.Unlock()
	et := t.records.Type(typ)
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

func (t *Tenant) mainDraft(m platform.Member, typ, id string, now time.Time) (mainDraft, *kernel.Error) {
	var out mainDraft
	if !build.BranchDraftType(typ) {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{type} is not a resource draft type", typ)
	}
	fields, refusal := t.draftFields(typ)
	if refusal != nil {
		return out, refusal
	}
	raw, refusal := t.readRecordFrom(t.records, t.branchCaller(m), typ, id, now)
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

func (t *Tenant) branchDetail(m platform.Member, branch build.Branch, now time.Time) (BranchDetail, *kernel.Error) {
	detail := BranchDetail{ID: branch.ID, Name: branch.Name, Title: branch.Title, Description: branch.Description, State: branch.State, Revision: branch.Revision,
		Created: branch.Created, Changed: branch.Changed, Changes: len(branch.Changes), Resources: []BranchResourceStatus{}, Proposal: branch.Proposal, Merge: branch.Merge}
	t.mu.Lock()
	defer t.mu.Unlock()
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
			current, refusal := t.mainDraft(m, typ, id, now)
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

func (t *Tenant) ListBranches(m platform.Member, now time.Time) ([]BranchDetail, *kernel.Error) {
	t.mu.Lock()
	page, refusal := t.recordsFrom(t.records, m, build.BranchType, platform.Query{Limit: 500, Sort: []string{"id"}}, now)
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
	t.mu.Unlock()
	if refusal != nil {
		return nil, refusal
	}
	out := []BranchDetail{}
	for _, id := range ids {
		branch, refusal := t.readBranch(m, id, now)
		if refusal != nil {
			return nil, refusal
		}
		detail := BranchDetail{ID: branch.ID, Name: branch.Name, Title: branch.Title, Description: branch.Description, State: branch.State, Revision: branch.Revision,
			Created: branch.Created, Changed: branch.Changed, Changes: len(branch.Changes), Resources: []BranchResourceStatus{}, Proposal: branch.Proposal, Merge: branch.Merge}
		for _, key := range branch.Resources() {
			typ, rid, _ := strings.Cut(key, "/")
			latest, _ := branch.Latest(typ, rid)
			detail.Resources = append(detail.Resources, BranchResourceStatus{Type: typ, ID: rid, Op: latest.Op, LastSeq: latest.Seq, LastAt: latest.At, LastBy: latest.By})
		}
		out = append(out, detail)
	}
	return out, nil
}

func branchKey(prefix, member, schema string, payload []byte, revision *uint32) string {
	sum := sha256.New()
	sum.Write([]byte(member + "\x00" + schema + "\x00"))
	sum.Write(payload)
	if revision != nil {
		sum.Write([]byte("\x00" + strconv.FormatUint(uint64(*revision), 10)))
	}
	return prefix + ":" + hex.EncodeToString(sum.Sum(nil))[:24]
}

func (t *Tenant) submitBranch(m platform.Member, key, schema, id string, payload any, now time.Time) *kernel.Error {
	raw := platform.Raw(payload)
	_, refusal := t.Submit(m, &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: branchKey(key, m.ID, schema, raw, nil),
		Target: &pb.EntityRef{Type: build.BranchType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
	return refusal
}

func (t *Tenant) CreateBranch(m platform.Member, request BranchCreateRequest, now time.Time) (BranchDetail, *kernel.Error) {
	if !build.Named(request.Name) {
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A branch name is lower-case letters and digits")
	}
	if refusal := t.submitBranch(m, "branch:"+request.Name+":create", build.BranchType+".create", request.Name, map[string]any{"name": request.Name, "title": request.Title, "description": request.Description}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, request.Name, now)
}

func (t *Tenant) BranchDetail(m platform.Member, id string, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.branchDetail(m, branch, now)
}

func (t *Tenant) BranchChanges(m platform.Member, id string, specs bool, now time.Time) ([]BranchChangeEntry, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return nil, refusal
	}
	out := make([]BranchChangeEntry, 0, len(branch.Changes))
	for _, change := range branch.Changes {
		entry := BranchChangeEntry{Seq: change.Seq, At: change.At, By: change.By, Type: change.Type, ID: change.ID, Op: change.Op, Rebased: change.Rebased, RID: t.branchRID(change.Type, change.ID, nil)}
		if specs {
			entry.Base, entry.Spec = change.Base, change.Spec
		}
		out = append(out, entry)
	}
	return out, nil
}

func (t *Tenant) SaveBranchResource(m platform.Member, id, typ, draft string, request BranchSaveRequest, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if branch.State != build.BranchOpen {
		return BranchDetail{}, branchRefuse("The branch is {state}; only an open branch takes saves", branch.State)
	}
	t.mu.Lock()
	current, refusal := t.mainDraft(m, typ, draft, now)
	t.mu.Unlock()
	if refusal != nil {
		return BranchDetail{}, refusal
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
			return BranchDetail{}, branchRefuse("{resource} already exists; save it as an update", typ+"/"+draft)
		}
	case "update", "delete":
		if !existsOnBranch {
			return BranchDetail{}, branchRefuse("{resource} does not exist; save it as a create", typ+"/"+draft)
		}
	default:
		return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A branch change creates, updates or deletes")
	}
	if op != "delete" {
		fields, refusal := t.draftFields(typ)
		if refusal != nil {
			return BranchDetail{}, refusal
		}
		var spec map[string]json.RawMessage
		if json.Unmarshal(request.Spec, &spec) != nil || spec == nil {
			return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A saved spec is one JSON object")
		}
		for name := range spec {
			if !fields[name] {
				return BranchDetail{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{field} is not an authored field of {type}", name, typ)
			}
		}
	}
	if touched && previous.Op == op && !previous.Rebased && sameJSON(previous.Spec, request.Spec) {
		return t.branchDetail(m, branch, now)
	}
	payload := map[string]any{"type": typ, "id": draft, "op": op}
	if op != "delete" {
		payload["spec"] = request.Spec
	}
	if !touched {
		if current.Exists {
			payload["base"] = current.Spec
		} else {
			payload["base"] = nil
		}
	}
	if refusal := t.submitBranch(m, "branch:"+id+":save:"+strconv.Itoa(len(branch.Changes)+1), build.BranchType+".save", id, payload, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func effectiveSpec(branch build.Branch, typ, id string) (spec json.RawMessage, deleted bool) {
	latest, ok := branch.Latest(typ, id)
	if !ok || latest.Op == "delete" {
		return nil, ok
	}
	return latest.Spec, false
}

func (t *Tenant) BranchResource(m platform.Member, id, typ, draft string, now time.Time) (BranchResourceView, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchResourceView{}, refusal
	}
	latest, touched := branch.Latest(typ, draft)
	t.mu.Lock()
	current, refusal := t.mainDraft(m, typ, draft, now)
	t.mu.Unlock()
	if refusal != nil {
		return BranchResourceView{}, refusal
	}
	if !touched {
		return BranchResourceView{Type: typ, ID: draft, RID: current.RID, Main: current.Spec, Branch: current.Spec}, nil
	}
	base, _ := branch.Base(typ, draft)
	spec, _ := effectiveSpec(branch, typ, draft)
	return BranchResourceView{Type: typ, ID: draft, RID: current.RID, Op: latest.Op, Base: base, Main: current.Spec, Branch: spec}, nil
}

type resourcePlan struct {
	Type, ID string
	RID      string
	Op       string
	Base     json.RawMessage
	Main     mainDraft
	Branch   json.RawMessage
	Merged   json.RawMessage
	Diff     BranchResourceDiff
}

func resolutionsFor(requested []BranchResolution, typ, id string) []branches.Resolution {
	var out []branches.Resolution
	for _, r := range requested {
		if r.Type == typ && r.ID == id {
			out = append(out, branches.Resolution{Path: r.Path, Take: r.Take})
		}
	}
	return out
}

func (t *Tenant) planBranch(m platform.Member, branch build.Branch, resolutions []BranchResolution, now time.Time) ([]resourcePlan, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	var plans []resourcePlan
	for _, key := range branch.Resources() {
		typ, id, _ := strings.Cut(key, "/")
		latest, _ := branch.Latest(typ, id)
		current, refusal := t.mainDraft(m, typ, id, now)
		if refusal != nil {
			return nil, refusal
		}
		base, _ := branch.Base(typ, id)
		spec, _ := effectiveSpec(branch, typ, id)
		merged, conflicts, err := branches.Merge(base, current.Spec, spec, resolutionsFor(resolutions, typ, id))
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
		plans = append(plans, resourcePlan{Type: typ, ID: id, RID: current.RID, Op: latest.Op, Base: base, Main: current, Branch: spec, Merged: merged,
			Diff: BranchResourceDiff{Type: typ, ID: id, RID: current.RID, Op: latest.Op, Branch: branchChanges, Main: mainChanges, Conflicts: conflicts, Merged: merged}})
	}
	return plans, nil
}

func (t *Tenant) BranchDiff(m platform.Member, id string, now time.Time) (BranchDiff, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDiff{}, refusal
	}
	plans, refusal := t.planBranch(m, branch, nil, now)
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

func conflictsOf(plans []resourcePlan) []BranchResourceDiff {
	out := []BranchResourceDiff{}
	for _, plan := range plans {
		if len(plan.Diff.Conflicts) > 0 {
			out = append(out, plan.Diff)
		}
	}
	return out
}

type branchAction struct {
	plan    resourcePlan
	action  string
	schema  string
	payload map[string]json.RawMessage
}

func (t *Tenant) branchActions(plans []resourcePlan) ([]branchAction, *kernel.Error) {
	var actions []branchAction
	for _, plan := range plans {
		item := branchAction{plan: plan, action: "none"}
		switch {
		case plan.Merged == nil && plan.Main.Exists:
			item.action, item.schema = "archive", plan.Type+".archive"
			item.payload = map[string]json.RawMessage{}
		case plan.Merged != nil && !plan.Main.Exists:
			item.action, item.schema = "create", plan.Type+".create"
			if json.Unmarshal(plan.Merged, &item.payload) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The merged spec is unreadable")
			}
		case plan.Merged != nil:
			var merged, current map[string]json.RawMessage
			if json.Unmarshal(plan.Merged, &merged) != nil || json.Unmarshal(plan.Main.Spec, &current) != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The merged spec is unreadable")
			}
			changed := map[string]json.RawMessage{}
			for name, value := range merged {
				if previous, present := current[name]; !present || string(previous) != string(value) {
					changed[name] = value
				}
			}
			for name := range current {
				if _, kept := merged[name]; !kept {
					changed[name] = json.RawMessage("null")
				}
			}
			if len(changed) > 0 {
				item.action, item.schema, item.payload = "edit", plan.Type+".edit", changed
			}
		}
		actions = append(actions, item)
	}
	slices.SortStableFunc(actions, func(a, b branchAction) int {
		order := func(x branchAction) int {
			if x.action == "archive" {
				return 1
			}
			return 0
		}
		return order(a) - order(b)
	})
	return actions, nil
}

func (a branchAction) submission(tenant string, m platform.Member, key string) *pb.Submission {
	payload, _ := json.Marshal(a.payload)
	s := &pb.Submission{TenantId: tenant, PrincipalId: m.ID, Authority: build.ID,
		Target: &pb.EntityRef{Type: a.plan.Type, Id: a.plan.ID}, Schema: &pb.SchemaRef{Name: a.schema, Version: 1}, Payload: payload}
	if a.action != "create" && a.plan.Main.Revision > 0 {
		revision := a.plan.Main.Revision
		s.ExpectedRevision = &revision
	}
	s.IdempotencyKey = branchKey(key, m.ID, a.schema, payload, s.ExpectedRevision)
	return s
}

func (t *Tenant) stageBranchChecks(m platform.Member, branchID string, actions []branchAction, now time.Time) ([]BranchResourceCheck, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	owner := t.owner["action:"+build.BranchType+".create"]
	draft := t.newStagedDecision()
	draft.simulation = true
	out := []BranchResourceCheck{}
	for _, item := range actions {
		check := BranchResourceCheck{Type: item.plan.Type, ID: item.plan.ID, RID: item.plan.RID, Action: item.action, Accepted: true}
		if item.action != "none" && owner != nil {
			s := item.submission(t.ID, m, "branch-check:"+branchID+":"+item.plan.Type+"/"+item.plan.ID)
			s.ExpectedRevision = nil
			a := t.owner["action:"+item.schema]
			if a == nil {
				check.Accepted, check.Message = false, "The resource type has no "+item.action+" action"
			} else {
				s = t.currentSubmissionSchema(a, s)
				if _, refusal := decideAccepted(a, draft, m, s, now); refusal != nil {
					check.Accepted, check.Code = false, refusal.Code.String()
					check.Message = explained(refusal, a, s.GetSchema().GetName(), target(s)).Message
					check.Issues = t.readableFieldIssues(m, draft.inputIssues[refusal], now)
				}
			}
		}
		out = append(out, check)
	}
	return out, nil
}

func (t *Tenant) BranchChecks(m platform.Member, id string, request BranchResolveRequest, now time.Time) (BranchChecks, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchChecks{}, refusal
	}
	return t.branchChecks(m, branch, request.Resolutions, now)
}

func (t *Tenant) branchChecks(m platform.Member, branch build.Branch, resolutions []BranchResolution, now time.Time) (BranchChecks, *kernel.Error) {
	plans, refusal := t.planBranch(m, branch, resolutions, now)
	if refusal != nil {
		return BranchChecks{}, refusal
	}
	checks := BranchChecks{At: now.UTC(), Conflicts: conflictsOf(plans), Resources: []BranchResourceCheck{}}
	if len(checks.Conflicts) == 0 {
		actions, refusal := t.branchActions(plans)
		if refusal != nil {
			return checks, refusal
		}
		checks.Resources, refusal = t.stageBranchChecks(m, branch.ID, actions, now)
		if refusal != nil {
			return checks, refusal
		}
	}
	if err := branch.Approved(); err != nil {
		checks.Approval = err.Error()
	} else {
		checks.Approved = true
	}
	checks.Ready = len(checks.Conflicts) == 0 && len(checks.Resources) > 0 && !slices.ContainsFunc(checks.Resources, func(c BranchResourceCheck) bool { return !c.Accepted })
	return checks, nil
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
	checks, refusal := t.branchChecks(m, branch, request.Resolutions, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if len(checks.Conflicts) > 0 {
		return BranchDetail{}, branchRefuse("The branch conflicts with main in {count} resources; rebase it first", len(checks.Conflicts))
	}
	if rejected := slices.IndexFunc(checks.Resources, func(c BranchResourceCheck) bool { return !c.Accepted }); rejected >= 0 {
		return BranchDetail{}, branchRefuse("{resource} would be refused: {message}", checks.Resources[rejected].Type+"/"+checks.Resources[rejected].ID, checks.Resources[rejected].Message)
	}
	checks.Approved, checks.Approval = false, ""
	summary, _ := json.Marshal(checks)
	reviewers := request.Reviewers
	if reviewers == nil {
		reviewers = []string{}
	}
	if refusal := t.submitBranch(m, "branch:"+id+":propose:"+strconv.Itoa(len(branch.Changes)), build.BranchType+".propose", id, map[string]any{"reviewers": reviewers, "note": request.Note, "checks": json.RawMessage(summary)}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) ReviewBranch(m platform.Member, id string, request BranchReviewRequest, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	reviews := 0
	if branch.Proposal != nil {
		reviews = len(branch.Proposal.Reviews)
	}
	if refusal := t.submitBranch(m, "branch:"+id+":review:"+strconv.Itoa(len(branch.Changes))+":"+strconv.Itoa(reviews+1), build.BranchType+".review", id, map[string]any{"decision": request.Decision, "note": request.Note}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) WithdrawBranch(m platform.Member, id string, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if refusal := t.submitBranch(m, "branch:"+id+":withdraw:"+strconv.Itoa(int(branch.Record.Revision)), build.BranchType+".withdraw", id, map[string]any{}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) CloseBranch(m platform.Member, id string, now time.Time) (BranchDetail, *kernel.Error) {
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return BranchDetail{}, refusal
	}
	if refusal := t.submitBranch(m, "branch:"+id+":close:"+strconv.Itoa(int(branch.Record.Revision)), build.BranchType+".close", id, map[string]any{}, now); refusal != nil {
		return BranchDetail{}, refusal
	}
	return t.BranchDetail(m, id, now)
}

func (t *Tenant) rollbackBranch(m platform.Member, branchID string, done []branchAction, now time.Time) []string {
	var failed []string
	for i := len(done) - 1; i >= 0; i-- {
		item := done[i]
		current, refusal := t.mainDraftSnapshot(m, item.plan.Type, item.plan.ID, now)
		if refusal != nil {
			failed = append(failed, item.plan.Type+"/"+item.plan.ID)
			continue
		}
		key := "branch:" + branchID + ":rollback:" + item.plan.Type + "/" + item.plan.ID
		var s *pb.Submission
		switch item.action {
		case "create":
			rollback := branchAction{plan: item.plan, action: "archive", schema: item.plan.Type + ".archive", payload: map[string]json.RawMessage{}}
			rollback.plan.Main.Revision = current.Revision
			s = rollback.submission(t.ID, m, key)
		case "edit":
			var original map[string]json.RawMessage
			var merged map[string]json.RawMessage
			_ = json.Unmarshal(item.plan.Main.Spec, &original)
			_ = json.Unmarshal(current.Spec, &merged)
			restore := map[string]json.RawMessage{}
			for name, value := range original {
				if now, present := merged[name]; !present || string(now) != string(value) {
					restore[name] = value
				}
			}
			for name := range merged {
				if _, had := original[name]; !had {
					restore[name] = json.RawMessage("null")
				}
			}
			rollback := branchAction{plan: item.plan, action: "edit", schema: item.plan.Type + ".edit", payload: restore}
			rollback.plan.Main.Revision = current.Revision
			s = rollback.submission(t.ID, m, key)
		default:
			continue
		}
		if _, refusal := t.Submit(m, s, now); refusal != nil {
			failed = append(failed, item.plan.Type+"/"+item.plan.ID)
		}
	}
	return failed
}

func (t *Tenant) mainDraftSnapshot(m platform.Member, typ, id string, now time.Time) (mainDraft, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.mainDraft(m, typ, id, now)
}

func (t *Tenant) MergeBranch(m platform.Member, id string, request BranchMergeRequest, now time.Time) (BranchMergeResult, *kernel.Error) {
	var result BranchMergeResult
	branch, refusal := t.readBranch(m, id, now)
	if refusal != nil {
		return result, refusal
	}
	if branch.State != build.BranchProposed {
		return result, branchRefuse("The branch is {state}; only a proposed branch merges", branch.State)
	}
	if err := branch.Approved(); err != nil {
		return result, branchRefuse("{reason}", err.Error())
	}
	plans, refusal := t.planBranch(m, branch, request.Resolutions, now)
	if refusal != nil {
		return result, refusal
	}
	if conflicts := conflictsOf(plans); len(conflicts) > 0 {
		return result, branchRefuse("The branch conflicts with main in {count} resources; rebase it first", len(conflicts))
	}
	actions, refusal := t.branchActions(plans)
	if refusal != nil {
		return result, refusal
	}
	staged, refusal := t.stageBranchChecks(m, id, actions, now)
	if refusal != nil {
		return result, refusal
	}
	for _, check := range staged {
		if !check.Accepted {
			return result, branchRefuse("{resource} would be refused: {message}", check.Type+"/"+check.ID, check.Message)
		}
	}
	var applied []branchAction
	result.Applied = []BranchResourceCheck{}
	fail := func(refusal *kernel.Error, what string) (BranchMergeResult, *kernel.Error) {
		if failed := t.rollbackBranch(m, id, applied, now); len(failed) > 0 {
			return result, branchRefuse("{what}: {message}; rollback needs attention for {resources}", what, refusal.Message, strings.Join(failed, ", "))
		}
		return result, refusal
	}
	var drafts []build.JointDraftRef
	for _, item := range actions {
		if item.action == "none" {
			result.Applied = append(result.Applied, BranchResourceCheck{Type: item.plan.Type, ID: item.plan.ID, RID: item.plan.RID, Action: "none", Accepted: true})
			continue
		}
		if item.action == "archive" {
			continue
		}
		if _, refusal := t.Submit(m, item.submission(t.ID, m, "branch:"+id+":merge:"+item.plan.Type+"/"+item.plan.ID), now); refusal != nil {
			return fail(refusal, item.plan.Type+"/"+item.plan.ID)
		}
		applied = append(applied, item)
		result.Applied = append(result.Applied, BranchResourceCheck{Type: item.plan.Type, ID: item.plan.ID, RID: item.plan.RID, Action: item.action, Accepted: true})
	}
	for _, item := range actions {
		if item.action == "none" || item.action == "archive" {
			continue
		}
		ref, err := build.BranchResource(item.plan.Type, item.plan.ID)
		if err != nil {
			continue
		}
		selected := []build.JointDraftRef{{Kind: ref.Kind, ID: item.plan.ID}}
		if closure, refusal := t.ReferencedDrafts(m, ref.Kind, item.plan.ID); refusal == nil {
			selected = append(selected, closure.Drafts...)
		}
		for _, draft := range selected {
			if !slices.Contains(drafts, draft) {
				drafts = append(drafts, draft)
			}
		}
	}
	if len(drafts) > 0 {
		t.mu.Lock()
		preview, candidate, err := t.previewReleaseLocked(drafts, m)
		t.mu.Unlock()
		if err == nil && preview.Diagnostic != "" {
			err = fmt.Errorf("%s", preview.Diagnostic)
		}
		if err != nil {
			return fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The merged drafts do not form a valid release: {error}", err.Error()), "release")
		}
		saved, err := t.SaveReleaseCandidates(m, drafts, candidate.ID, "branch:"+id+":merge", now)
		if err == nil {
			_, err = t.SealCandidate(saved, now)
		}
		if err != nil {
			return fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The merged drafts could not be saved as a release: {error}", err.Error()), "release")
		}
		result.Candidate = saved
		if request.Simulation != nil {
			simulation := *request.Simulation
			simulation.CandidateID, simulation.At = saved, now
			out, refusal := t.SimulateCandidate(m, simulation)
			if refusal != nil {
				return fail(refusal, "simulation")
			}
			result.Simulated = &out
			if out.Passed != nil && !*out.Passed {
				return fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The candidate simulation failed"), "simulation")
			}
		}
	}
	for _, item := range actions {
		if item.action != "archive" {
			continue
		}
		if _, refusal := t.Submit(m, item.submission(t.ID, m, "branch:"+id+":merge:"+item.plan.Type+"/"+item.plan.ID), now); refusal != nil {
			return fail(refusal, item.plan.Type+"/"+item.plan.ID)
		}
		result.Applied = append(result.Applied, BranchResourceCheck{Type: item.plan.Type, ID: item.plan.ID, RID: item.plan.RID, Action: "archive", Accepted: true})
	}
	summary, _ := json.Marshal(result.Applied)
	if refusal := t.submitBranch(m, "branch:"+id+":merge", build.BranchType+".merge", id, map[string]any{"candidate": result.Candidate, "applied": json.RawMessage(summary)}, now); refusal != nil {
		return result, refusal
	}
	result.Branch, refusal = t.BranchDetail(m, id, now)
	return result, refusal
}

func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	left, _ := json.Marshal(x)
	right, _ := json.Marshal(y)
	return string(left) == string(right)
}
