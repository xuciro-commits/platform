package work

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

const (
	ID               = platform.WorkApp
	ApprovalType     = platform.WorkApprovalType
	TaskType         = platform.WorkTaskType
	Admin            = "admin"
	SchemaRequest    = platform.WorkApprovalRequestSchema
	DelegationType   = "work.delegation"
	SchemaDelegate   = "work.delegation.add"
	SchemaUndelegate = "work.delegation.end"
	ViewType         = platform.WorkViewType
	SchemaViewSave   = "work.view.save"
	SchemaViewRemove = "work.view.remove"
)

type ApprovalRequest = platform.ApprovalRequest

type ApprovalStep = platform.ApprovalStep

func approvalRequired(s ApprovalStep) int {
	if s.All {
		return len(s.Approvers)
	}
	if s.Count > 0 {
		return s.Count
	}
	return 1
}

type Delegation = platform.WorkDelegation

type WorkTask = platform.WorkTask

type SavedView = platform.SavedView

type Work struct {
	mu     sync.Mutex
	host   platform.WorkHost
	ledger *platform.Ledger
}

var _ platform.ApprovalOwner = (*Work)(nil)
var _ platform.AttachedWork = (*Work)(nil)

func (w *Work) AttachWork(h platform.WorkHost) { w.host = h }

func (w *Work) RequestApproval(c platform.Caller, held *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if c.Automation || c.Tenant == "" || held.GetTenantId() != c.Tenant || held.GetPrincipalId() != c.ID || held.GetAuthority() != c.App {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	raw, err := protojson.Marshal(held)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	payload, err := json.Marshal(map[string]any{"requester": c.ID, "submission": json.RawMessage(raw)})
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	id := c.App + "." + held.GetIdempotencyKey()
	automation := w.host.Automation(c, ID)
	request := &pb.Submission{TenantId: c.Tenant, PrincipalId: automation.ID, Authority: ID,
		IdempotencyKey: "approval:" + id, Target: &pb.EntityRef{Type: ApprovalType, Id: id},
		Schema: &pb.SchemaRef{Name: SchemaRequest, Version: 1}, Payload: payload}
	return platform.Decide(automation, w, request, now)
}

func New(tenant string) *Work {
	w := &Work{}
	var actions []platform.Action
	actions = append(actions, retirementAction())
	actions = append(actions, redirectAction())
	actions = append(actions, refusalEscalationAction())
	for _, e := range w.entities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	actions = append(actions, platform.Action{Schema: SchemaRequest, Target: ApprovalType, Capability: "approvals", Title: "Request approval",
		Description: "Hold a submission until its approvers agree (made by the host when an action needs approval).", Payload: []platform.Field{}, Roles: []string{Admin}},
		platform.Action{Schema: SchemaViewSave, Target: ViewType, Capability: "views", Title: "Save view", Description: "Save a view of a list under a name, or change your own.",
			Payload: []platform.Field{{Name: "title", Type: "string", Required: true, Description: "Its name"}, {Name: "entity", Type: "string", Required: true, Description: "The entity type listed"},
				{Name: "state", Type: "string", Description: "The list's state: search, grouping, pivot or chart"}}, Roles: []string{platform.AnyMember}},
		platform.Action{Schema: SchemaViewRemove, Target: ViewType, Capability: "views", Title: "Remove view", Description: "Remove one of your saved views.",
			Payload: []platform.Field{}, Roles: []string{platform.AnyMember}})
	actions = append(actions,
		platform.Action{Schema: SchemaDelegate, Target: DelegationType, New: true, Capability: "delegation", Title: "Delegate approvals",
			Description: "Hand your approvals and tasks to another member from a first to a last day: they decide for you, and the request says so.",
			Payload: []platform.Field{{Name: "to", Type: "string", Required: true, Description: "The delegate's member ID"},
				{Name: "start", Type: "date", Required: true, Description: "First day"}, {Name: "end", Type: "date", Required: true, Description: "Last day"}},
			Roles: []string{platform.AnyMember}},
		platform.Action{Schema: SchemaUndelegate, Target: DelegationType, Capability: "delegation", Title: "End delegation",
			Description: "End one of your delegations.", Payload: []platform.Field{}, Roles: []string{platform.AnyMember}})
	actions = append(actions, w.policyActions()...)
	w.ledger = platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), ApprovalType, TaskType, ViewType, DelegationType, PolicyType)
	return w
}

func (w *Work) entities() []platform.Entity {
	everyone := []string{platform.AnyMember}
	return []platform.Entity{
		{Type: ApprovalType, Title: "Approval request", Model: ApprovalRequest{},
			Scope: platform.Scope{Participants: func(record any) []string {
				a := record.(ApprovalRequest)
				out := []string{a.Requester}
				for _, l := range a.Levels {
					out = append(out, approvalCandidates(l)...)
				}
				return out
			}},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "pending",
				States: []platform.State{{Name: "pending", Title: "Pending", Tone: "warning"}, {Name: "approved", Title: "Approved", Tone: "success"},
					{Name: "rejected", Title: "Rejected", Tone: "danger"}, {Name: "refused", Title: "Refused when run", Tone: "danger"},
					{Name: "withdrawn", Title: "Withdrawn", Tone: "neutral"}, {Name: "redirected", Title: "Redirected", Tone: "info"}},
				Transitions: []platform.Transition{
					{Name: "approve", Title: "Approve", From: []string{"pending"}, To: []string{"pending", "approved", "refused"}, Roles: everyone, Capability: "approvals",
						Description: "Approve at the level you are an approver of; the last approval runs the held action with the rules of that moment.",
						Payload:     []platform.Field{{Name: "note", Type: "string", Description: "Why"}},
						Do:          w.approve, After: w.opened},
					{Name: "reject", Title: "Reject", From: []string{"pending"}, To: []string{"rejected"}, Roles: everyone, Capability: "approvals",
						Description: "Reject the request at the level you are an approver of.", Payload: []platform.Field{{Name: "note", Type: "string", Description: "Why"}},
						Do: w.reject, After: w.ended},
					{Name: "withdraw", Title: "Withdraw", From: []string{"pending"}, To: []string{"withdrawn"}, Roles: everyone, Capability: "approvals",
						Description: "Withdraw your own request.", Do: w.requester, After: w.ended},
				}}},
		{Type: TaskType, Title: "Task", Model: WorkTask{}, Derived: []platform.Derivation{{From: "ref", Fields: []string{"title", "body"}}}, Withheld: "withheld",
			Scope: platform.Scope{Participants: func(record any) []string {
				task := record.(WorkTask)
				return append([]string{task.Assignee}, task.Candidates...)
			}},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "open",
				States: []platform.State{{Name: "open", Title: "Open", Tone: "info"}, {Name: "done", Title: "Done", Tone: "success"}, {Name: "canceled", Title: "Canceled", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "claim", Title: "Take", From: []string{"open"}, To: []string{"open"}, Roles: everyone, Capability: "tasks",
						Description: "Take a task offered to you, so others see it is yours.", Do: w.claim},
					{Name: "complete", Title: "Done", From: []string{"open"}, To: []string{"done"}, Roles: everyone, Capability: "tasks",
						Payload:     []platform.Field{{Name: "answer", Type: "string", Description: "One of the task's answers, when it has any"}},
						Description: "Mark a task of yours done.", Do: w.completer,
						After: func(c platform.Caller, _ *pb.ChangeRecord, record any, _ time.Time) {
							w.closed(c, record.(*WorkTask).ID)
						}},
				}}},
		policyEntity(),
		{Type: ViewType, Title: "Saved view", Model: SavedView{}},
		{Type: DelegationType, Title: "Delegation", Model: Delegation{}, Description: "A member's approvals and tasks handed to another for some days; the delegate decides for them, and the request says so.",
			Scope: platform.Scope{Participants: func(record any) []string { d := record.(Delegation); return []string{d.From, d.To} }}},
	}
}

func (w *Work) SnapshotFormat() string { return "platform.ledger.v1" }

func (w *Work) Snapshot() (json.RawMessage, error) { return w.ledger.Snapshot() }

func (w *Work) Restore(raw json.RawMessage) error { return w.ledger.Restore(raw) }

func (w *Work) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Work", Version: "1", Actions: w.ledger.Catalog, Entities: w.entities(),
		Reads: []string{"inbox", "requests", "views"}, Everyone: []string{"inbox", "requests", "views"},
		Jobs: []platform.Job{{Name: "overdue", Title: "Tell people about overdue tasks", Every: time.Minute}, {Name: "redirects", Title: "Start redirected approvals after release activation", Every: time.Second}, {Name: "refusals", Title: "Escalate repeated business refusals", Every: time.Second}}}
}

func (w *Work) Declarations() []*pb.AuthorityDeclaration { return w.ledger.Declarations() }

func (w *Work) AcceptedLedger() *platform.Ledger { return w.ledger }

func (w *Work) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if record, err, ok := w.ledger.Generated(c, s, now, nil, w.entities()...); ok {
		return record, err
	}
	return w.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		switch s.GetSchema().GetName() {
		case schemaRefusalEscalation:
			return w.escalateRefusal(c, s, now)
		case schemaRetireApproval:
			return w.retireApproval(c, s, now)
		case schemaRedirectApproval:
			return w.redirectApproval(c, s, now)
		case SchemaDelegate, SchemaUndelegate:
			return w.delegate(c, s)
		case SchemaViewSave, SchemaViewRemove:
			return w.view(c, s)
		case SchemaPolicySave, SchemaPolicyRemove:
			return w.policy(c, s)
		}
		if w.host == nil || s.GetSchema().GetName() != SchemaRequest {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
		}
		return w.request(c, s, now)
	})
}

func (w *Work) request(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	var p struct {
		Requester  string          `json:"requester"`
		Submission json.RawMessage `json:"submission"`
	}
	held := &pb.Submission{}
	if json.Unmarshal(s.GetPayload(), &p) != nil || protojson.Unmarshal(p.Submission, held) != nil {
		return nil, invalid
	}
	app, declared, known := w.host.Action(held.GetSchema().GetName())
	requester, ok := w.host.Member(p.Requester)
	if !known || declared.Approval == nil || !ok {
		return nil, invalid
	}
	asker := w.host.Caller(c, requester, app)
	var steps []ApprovalStep
	for _, level := range declared.Approval.Levels {
		if level.When != nil && !level.When(asker, held) {
			continue
		}
		approvers := slices.DeleteFunc(w.approvers(level, requester, app, now), func(m string) bool { return m == requester.ID })
		if len(approvers) == 0 {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		step := ApprovalStep{Title: level.Title, Approvers: approvers, All: level.All, Approved: []string{}, Delegates: w.delegates(c, approvers, now)}
		step.Count = CountFor(step, level.All, w.PolicyCount(c, app, declared.Schema), len(approvers))
		if level.Due > 0 {
			step.Due = now.Add(level.Due)
		}
		if level.WorkingDays > 0 {
			cal := w.host.Calendar("member:"+requester.ID, now.UTC().Format(time.DateOnly))
			step.Due = cal.After(now, level.WorkingDays)
		}
		steps = append(steps, step)
	}
	raw, _ := protojson.Marshal(held)
	release, refusal := w.host.ApprovalRelease(app, declared.Schema, held.GetTarget().GetType())
	if refusal != nil {
		return nil, refusal
	}
	request := ApprovalRequest{Record: platform.Record{ID: s.GetTarget().GetId()}, Action: declared.Schema, Title: declared.Title, App: app,
		Target: held.GetTarget().GetType() + "/" + held.GetTarget().GetId(), Requester: requester.ID, Submission: string(raw), Levels: steps, State: "pending", Release: release}
	if _, known := platform.Get[ApprovalRequest](c, request.ID); known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return func(r *pb.ChangeRecord) {
		if len(steps) == 0 {
			w.run(c, r, request, now)
			return
		}
		c.Put(r, request)
		w.offer(c, r, request, now)
		w.move(c, request, platform.ApprovalHeld, now)
	}, nil
}

func (w *Work) approvers(level platform.ApprovalLevel, requester platform.Member, app string, now time.Time) []string {
	var out []string
	add := func(ms ...string) {
		for _, m := range ms {
			if !slices.Contains(out, m) {
				out = append(out, m)
			}
		}
	}
	if level.Member != "" {
		add(level.Member)
	}
	if level.AppRole != "" {
		add(w.host.Holding(app, level.AppRole)...)
	}
	if level.Role != "" {
		day := now.UTC().Format(time.DateOnly)
		units := w.host.Units("member:"+requester.ID, "", day)
		for _, u := range units {
			add(w.host.Holders(level.Structure, u, level.Role, day)...)
		}
	}
	slices.Sort(out)
	return out
}

func (w *Work) offer(c platform.Caller, r *pb.ChangeRecord, a ApprovalRequest, now time.Time) {
	step := a.Levels[a.Level]
	task := WorkTask{Record: platform.Record{ID: fmt.Sprintf("%s#%d", a.ID, a.Level+1)}, Title: "Approve: " + a.Title + " " + a.Target,
		Body: fmt.Sprintf("%s asks, level %d of %d: %s.", a.Requester, a.Level+1, len(a.Levels), step.Title), Ref: ApprovalType + "/" + a.ID,
		App: a.App, Candidates: approvalCandidates(step), Due: step.Due, State: "open"}
	c.Put(r, task)
	var to []platform.Recipient
	for _, m := range task.Candidates {
		to = append(to, platform.Recipient{Member: m})
	}
	c.Notify(platform.Notification{Title: task.Title, Body: task.Body, Ref: task.Ref, Key: "task:" + task.ID}, now, to...)
}

func (w *Work) close(c platform.Caller, r *pb.ChangeRecord, a ApprovalRequest, level int, state string) {
	if t, ok := platform.Get[WorkTask](c, fmt.Sprintf("%s#%d", a.ID, level+1)); ok && t.State == "open" {
		t.State = state
		c.Put(r, t)
		w.closed(c, t.ID)
	}
}

func (w *Work) decider(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	a := record.(*ApprovalRequest)
	step := a.Levels[a.Level]
	who := approvalActingFor(step, c.ID)
	if c.Agent || c.ID == a.Requester || who == "" || slices.Contains(step.Approved, who) {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return nil
}

func (w *Work) approve(c platform.Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error {
	if err := w.decider(c, record, payload, now); err != nil {
		return err
	}
	a := record.(*ApprovalRequest)
	step := &a.Levels[a.Level]
	who := approvalActingFor(*step, c.ID)
	step.Approved = append(step.Approved, who)
	if who != c.ID {
		if step.DecidedBy == nil {
			step.DecidedBy = map[string]string{}
		}
		step.DecidedBy[who] = c.ID
	}
	if len(step.Approved) < approvalRequired(*step) {
		return nil
	}
	if a.Level+1 < len(a.Levels) {
		a.Level++
		return nil
	}
	a.State = "approved"
	return nil
}

func (w *Work) opened(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	a := *record.(*ApprovalRequest)
	previous := a.Level
	if a.State == "approved" {
		w.close(c, r, a, a.Level, "done")
		w.run(c, r, a, now)
		return
	}
	if len(a.Levels[a.Level].Approved) == 0 {
		w.close(c, r, a, previous-1, "done")
		w.offer(c, r, a, now)
	}
}

func (w *Work) run(c platform.Caller, r *pb.ChangeRecord, a ApprovalRequest, now time.Time) {
	held := &pb.Submission{}
	protojson.Unmarshal([]byte(a.Submission), held)
	held.ExpectedRevision = nil
	execution, err := w.host.ApprovalExecution(c, a)
	if err == nil {
		requester, _ := execution.Member(a.Requester)
		_, err = execution.Attempt(execution.Caller(c, requester, a.App), held, now)
	}
	if err != nil {
		a.State, a.Outcome = "refused", err.Error()
		if err.Message != "" {
			a.Outcome += ": " + err.Message
		}
		w.move(c, a, platform.ApprovalReturned, now)
	} else {
		a.State = "approved"
	}
	c.Put(r, a)
	c.Notify(platform.Notification{Title: fmt.Sprintf("%s %s: %s", a.Title, a.Target, a.State), Body: a.Outcome, Ref: ApprovalType + "/" + a.ID,
		Key: "request:" + a.ID}, now, platform.Recipient{Member: a.Requester})
}

func (w *Work) ended(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	a := *record.(*ApprovalRequest)
	w.close(c, r, a, a.Level, "canceled")
	if a.State != "rejected" {
		w.move(c, a, platform.ApprovalReturned, now)
		return
	}
	w.move(c, a, platform.ApprovalRejected, now)
	to := []platform.Recipient{{Member: a.Requester}}
	for _, l := range a.Levels[:a.Level+1] {
		for _, m := range l.Approved {
			to = append(to, platform.Recipient{Member: m})
		}
	}
	c.Notify(platform.Notification{Title: fmt.Sprintf("%s %s: rejected by %s", a.Title, a.Target, a.RejectedBy), Body: a.Outcome, Ref: ApprovalType + "/" + a.ID,
		Key: "request:" + a.ID}, now, to...)
}

func (w *Work) reject(c platform.Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error {
	if err := w.decider(c, record, payload, now); err != nil {
		return err
	}
	var p struct{ Note string }
	json.Unmarshal(payload, &p)
	a := record.(*ApprovalRequest)
	a.RejectedBy, a.Outcome = c.ID, strings.TrimSpace(p.Note)
	return nil
}

func (w *Work) move(c platform.Caller, a ApprovalRequest, suffix string, now time.Time) {
	held := &pb.Submission{}
	protojson.Unmarshal([]byte(a.Submission), held)
	schema := a.Action + suffix
	execution, err := w.host.ApprovalExecution(c, a)
	if err != nil {
		c.FailDecision(err)
		return
	}
	if _, _, known := execution.Action(schema); !known {
		return
	}
	automation := execution.Automation(c, a.App)
	execution.Submit(automation, &pb.Submission{TenantId: held.GetTenantId(), PrincipalId: automation.ID, Authority: a.App, IdempotencyKey: "approval:" + a.ID + suffix,
		Target: held.GetTarget(), Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte("{}")}, now)
}

func (w *Work) requester(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	if record.(*ApprovalRequest).Requester != c.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return nil
}

func (w *Work) claim(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	t := record.(*WorkTask)
	if !slices.Contains(t.Candidates, c.ID) || t.Assignee != "" && t.Assignee != c.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	t.Assignee = c.ID
	return nil
}

func (w *Work) completer(c platform.Caller, record any, payload json.RawMessage, _ time.Time) *kernel.Error {
	t := record.(*WorkTask)
	var p struct{ Answer string }
	json.Unmarshal(payload, &p)
	if len(t.Answers) > 0 && !slices.Contains(t.Answers, p.Answer) || len(t.Answers) == 0 && p.Answer != "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	t.Answer = p.Answer
	if strings.HasPrefix(t.Ref, ApprovalType+"/") {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if !slices.Contains(t.Candidates, c.ID) || t.Assignee != "" && t.Assignee != c.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	t.Assignee = c.ID
	return nil
}

func (w *Work) inbox(c platform.Caller, now time.Time) []WorkTask {
	open, _ := json.Marshal([]any{[]any{"state", "=", "open"}})
	tasks, _, _ := platform.Find[WorkTask](c, platform.Query{Domain: open, Sort: []string{"due", "id"}})
	out := []WorkTask{}
	for _, t := range tasks {
		if t.Assignee == c.ID || t.Assignee == "" && slices.Contains(t.Candidates, c.ID) {
			out = append(out, t)
		}
	}
	slices.SortStableFunc(out, func(a, b WorkTask) int {
		late := func(t WorkTask) int { return map[bool]int{true: 0, false: 1}[!t.Due.IsZero() && t.Due.Before(now)] }
		return late(a) - late(b)
	})
	return out
}

func (w *Work) view(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	v, exists := platform.Get[SavedView](c, id)
	if exists && v.Owner != c.ID {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if s.GetSchema().GetName() == SchemaViewRemove {
		if !exists || v.Archived {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		v.Archived = true
		return func(r *pb.ChangeRecord) { c.Put(r, v) }, nil
	}
	var p struct{ Title, Entity, State string }
	if json.Unmarshal(s.GetPayload(), &p) != nil || exists && p.Entity != v.Entity {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	v.ID, v.Title, v.Entity, v.State, v.Owner, v.Archived = id, p.Title, p.Entity, p.State, c.ID, false
	if err := c.Check(v); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { c.Put(r, v) }, nil
}

func (w *Work) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name == "inbox" {
		return w.inbox(c, time.Now()), nil
	}
	if name == "views" {
		mine, _ := json.Marshal([]any{[]any{"owner", "=", c.ID}})
		out, _, _ := platform.Find[SavedView](c, platform.Query{Domain: mine, Sort: []string{"title"}})
		return out, nil
	}
	mine, _ := json.Marshal([]any{[]any{"requester", "=", c.ID}})
	out, _, _ := platform.Find[ApprovalRequest](c, platform.Query{Domain: mine, Sort: []string{"-id"}})
	return out, nil
}

func (w *Work) Run(c platform.Caller, name string, now time.Time) *kernel.Error {
	if name == "redirects" {
		return w.runRedirects(c, now)
	}
	if name == "refusals" {
		return w.runRefusalEscalations(c, now)
	}
	open, _ := json.Marshal([]any{[]any{"state", "=", "open"}})
	tasks, _, _ := platform.Find[WorkTask](c, platform.Query{Domain: open})
	for _, t := range tasks {
		if t.Due.IsZero() || !t.Due.Before(now) {
			continue
		}
		to := []platform.Recipient{}
		for _, m := range t.Candidates {
			to = append(to, platform.Recipient{Member: m})
		}
		c.Notify(platform.Notification{Title: "Overdue: " + t.Title, Body: t.Body, Ref: t.Ref, Key: "overdue:" + t.ID}, now, to...)
	}
	return nil
}

func (w *Work) Assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	if a.Close {
		if a.Key == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Closing an assignment needs its original owner key")
		}
		w.Close(c, r, c.App+":"+a.Key)
		return nil
	}
	candidates := w.host.Recipients(c, r.GetRecordedTime().AsTime(), a.To)
	work := w.host.Automation(c, ID)
	id := fmt.Sprintf("%s:%s", c.App, a.Key)
	if a.Key == "" {
		id = fmt.Sprintf("%s:%s", c.App, r.GetChangeId())
	}
	if existing, ok := platform.Get[WorkTask](work, id); ok && existing.State == "open" {
		return nil
	}
	task := WorkTask{Record: platform.Record{ID: id}, Title: a.Title, Body: a.Body, Ref: a.Ref, App: c.App, Key: a.Key, Candidates: candidates, Due: a.Due, State: "open", Answers: a.Answers}
	if err := work.Put(r, task); err != nil {
		return err
	}
	to := []platform.Recipient{}
	for _, m := range candidates {
		to = append(to, platform.Recipient{Member: m})
	}
	work.Notify(platform.Notification{Title: task.Title, Body: task.Body, Ref: task.Ref, Key: "task:" + task.ID}, r.GetRecordedTime().AsTime(), to...)
	return nil
}

func (w *Work) Close(c platform.Caller, r *pb.ChangeRecord, id string) {
	work := w.host.Automation(c, ID)
	if task, ok := platform.Get[WorkTask](work, id); ok && task.State == "open" {
		task.State = "canceled"
		work.Put(r, task)
		w.closed(c, id)
	}
}

func (w *Work) closed(c platform.Caller, id string) { w.host.Seen(c, "task:"+id, "overdue:"+id) }

func approvalCandidates(s ApprovalStep) []string {
	out := slices.Clone(s.Approvers)
	for d := range s.Delegates {
		if !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return out
}

func approvalActingFor(s ApprovalStep, member string) string {
	if slices.Contains(s.Approvers, member) {
		return member
	}
	return s.Delegates[member]
}

func (w *Work) delegates(c platform.Caller, approvers []string, now time.Time) map[string]string {
	day := now.UTC().Format(time.DateOnly)
	all, _, _ := platform.Find[Delegation](w.host.Automation(c, ID), platform.Query{Limit: 1000})
	out := map[string]string{}
	for _, d := range all {
		if !d.Archived && slices.Contains(approvers, d.From) && d.Start <= day && day <= d.End && !slices.Contains(approvers, d.To) {
			out[d.To] = d.From
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (w *Work) delegate(c platform.Caller, s *pb.Submission) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	existing, known := platform.Get[Delegation](c, id)
	if s.GetSchema().GetName() == SchemaUndelegate {
		if !known || existing.Archived || existing.From != c.ID && !c.Replaying {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		existing.Archived = true
		return func(r *pb.ChangeRecord) { c.Put(r, existing) }, nil
	}
	var p struct{ To, Start, End string }
	if json.Unmarshal(s.GetPayload(), &p) != nil || p.To == "" || p.To == c.ID || p.End < p.Start {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if _, ok := w.host.Member(p.To); !ok && !c.Replaying {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	d := Delegation{Record: platform.Record{ID: id}, From: c.ID, To: p.To, Start: p.Start, End: p.End}
	if err := c.Check(d); err != nil {
		return nil, err
	}
	return func(r *pb.ChangeRecord) { c.Put(r, d) }, nil
}

func (w *Work) ForkDecision() (platform.App, error) {
	return &Work{host: w.host, ledger: w.ledger}, nil
}

var _ platform.DecisionForkApp = (*Work)(nil)

func (a *Work) NewSimulationApp(tenant string) platform.App { return New(tenant) }
