package platformserver

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

// Work is the platform's work app (ADR-0017): approval requests for actions
// that wait for approvers, and tasks for people, with one inbox. Both are
// entity types (ADR-0016) with lifecycles, so they get lists, record pages and
// history, and replay like any records.
const (
	WorkApp      = "work"
	ApprovalType = "work.approval"
	TaskType     = "work.task"
	WorkAdmin    = "admin"
	// SchemaRequest holds a submission for approval; the host submits it for the requester.
	SchemaRequest = "work.approval.request"
)

// ApprovalRequest is a held submission and its chain of approvers.
type ApprovalRequest struct {
	platform.Record
	Action     string         `json:"action" field:"readonly,search"`
	Title      string         `json:"title" field:"readonly,search"`
	App        string         `json:"app" field:"readonly"`
	Target     string         `json:"target" field:"readonly,search"` // "<type>/<id>"
	Requester  string         `json:"requester" field:"readonly"`
	Submission string         `json:"submission" field:"readonly" type:"longtext"` // the held submission, as protojson
	Level      int            `json:"level" field:"readonly"`                      // the level deciding now
	Levels     []ApprovalStep `json:"levels" field:"readonly"`
	State      string         `json:"state" field:"readonly" choices:"pending,approved,rejected,refused,withdrawn"`
	Outcome    string         `json:"outcome,omitempty" field:"readonly"` // why a request was refused when it ran
}

// ApprovalStep is one level: who may approve, and who did.
type ApprovalStep struct {
	Title     string    `json:"title"`
	Approvers []string  `json:"approvers"`
	All       bool      `json:"all,omitempty"`
	Approved  []string  `json:"approved"`
	Due       time.Time `json:"due,omitzero"`
}

// WorkTask is work for people: who may take it, by when, and whether it is done.
type WorkTask struct {
	platform.Record
	Title      string    `json:"title" field:"readonly,search"`
	Body       string    `json:"body,omitempty" field:"readonly" type:"longtext"`
	Ref        string    `json:"ref,omitempty" field:"readonly"`
	App        string    `json:"app" field:"readonly"`
	Key        string    `json:"key,omitempty" field:"readonly"`
	Candidates []string  `json:"candidates" field:"readonly"`
	Assignee   string    `json:"assignee,omitempty" field:"readonly"`
	Due        time.Time `json:"due,omitzero" field:"readonly"`
	State      string    `json:"state" field:"readonly" choices:"open,done,canceled"`
}

type Work struct {
	mu     sync.Mutex
	t      *Tenant // the tenant running it, once composed (NewTenant)
	ledger *platform.Ledger
}

// NewWork is a tenant's work app. Every member uses it through two reads, the
// inbox and their requests; the records themselves (lists, pages, history) are
// for holders of its admin role.
func NewWork(tenant string) *Work {
	w := &Work{}
	var actions []platform.Action
	for _, e := range w.entities() {
		actions = append(actions, platform.EntityActions(e)...)
	}
	actions = append(actions, platform.Action{Schema: SchemaRequest, Target: ApprovalType, Capability: "approvals", Title: "Request approval",
		Description: "Hold a submission until its approvers agree (made by the host when an action needs approval).", Payload: []platform.Field{}, Roles: []string{WorkAdmin}})
	w.ledger = platform.NewLedger(tenant, WorkApp, platform.NewCatalog(actions...), ApprovalType, TaskType)
	return w
}

func (w *Work) entities() []platform.Entity {
	everyone := []string{platform.AnyMember}
	return []platform.Entity{
		{Type: ApprovalType, Title: "Approval request", Model: ApprovalRequest{},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "pending",
				States: []platform.State{{Name: "pending", Title: "Pending", Tone: "warning"}, {Name: "approved", Title: "Approved", Tone: "success"},
					{Name: "rejected", Title: "Rejected", Tone: "danger"}, {Name: "refused", Title: "Refused when run", Tone: "danger"},
					{Name: "withdrawn", Title: "Withdrawn", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "approve", Title: "Approve", From: []string{"pending"}, To: []string{"pending", "approved", "refused"}, Roles: everyone, Capability: "approvals",
						Description: "Approve at the level you are an approver of; the last approval runs the held action with the rules of that moment.",
						Payload:     []platform.Field{{Name: "note", Type: "string", Description: "Why"}},
						Do:          w.approve, After: w.opened},
					{Name: "reject", Title: "Reject", From: []string{"pending"}, To: []string{"rejected"}, Roles: everyone, Capability: "approvals",
						Description: "Reject the request at the level you are an approver of.", Payload: []platform.Field{{Name: "note", Type: "string", Description: "Why"}},
						Do: w.decider, After: w.closed},
					{Name: "withdraw", Title: "Withdraw", From: []string{"pending"}, To: []string{"withdrawn"}, Roles: everyone, Capability: "approvals",
						Description: "Withdraw your own request.", Do: w.requester, After: w.closed},
				}}},
		{Type: TaskType, Title: "Task", Model: WorkTask{},
			Lifecycle: &platform.Lifecycle{Field: "state", Initial: "open",
				States: []platform.State{{Name: "open", Title: "Open", Tone: "info"}, {Name: "done", Title: "Done", Tone: "success"}, {Name: "canceled", Title: "Canceled", Tone: "neutral"}},
				Transitions: []platform.Transition{
					{Name: "claim", Title: "Take", From: []string{"open"}, To: []string{"open"}, Roles: everyone, Capability: "tasks",
						Description: "Take a task offered to you, so others see it is yours.", Do: w.claim},
					{Name: "complete", Title: "Done", From: []string{"open"}, To: []string{"done"}, Roles: everyone, Capability: "tasks",
						Description: "Mark a task of yours done.", Do: w.completer},
				}}},
	}
}

func (w *Work) Manifest() platform.Manifest {
	return platform.Manifest{ID: WorkApp, Title: "Work", Version: "1", Actions: w.ledger.Catalog, Entities: w.entities(),
		Reads: []string{"inbox", "requests"}, Everyone: []string{"inbox", "requests"},
		Jobs: []platform.Job{{Name: "overdue", Title: "Tell people about overdue tasks", Every: time.Minute}}}
}

func (w *Work) Declarations() []*pb.AuthorityDeclaration { return w.ledger.Declarations() }

func (w *Work) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (w *Work) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if record, err, ok := w.ledger.Generated(c, s, now, nil, w.entities()...); ok {
		return record, err
	}
	return w.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if w.t == nil || s.GetSchema().GetName() != SchemaRequest {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
		}
		return w.request(c, s, now)
	})
}

// request opens an approval request for the held submission in the payload,
// on behalf of the requester it names; the approvers of each level are
// resolved now, on the request's day (ADR-0012).
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
	a := w.t.owner["action:"+held.GetSchema().GetName()]
	if a == nil {
		return nil, invalid
	}
	declared, _ := a.Manifest().Actions.Action(held.GetSchema().GetName())
	requester, ok := w.t.member(p.Requester)
	if declared.Approval == nil || !ok {
		return nil, invalid
	}
	asker := platform.NewCaller(runtime{w.t}, requester, a.Manifest().ID, c.Replaying, false)
	var steps []ApprovalStep
	for _, level := range declared.Approval.Levels {
		if level.When != nil && !level.When(asker, held) {
			continue
		}
		approvers := slices.DeleteFunc(w.t.approvers(level, requester, a.Manifest().ID, now), func(m string) bool { return m == requester.ID })
		if len(approvers) == 0 {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED} // nobody could approve it
		}
		step := ApprovalStep{Title: level.Title, Approvers: approvers, All: level.All, Approved: []string{}}
		if level.Due > 0 {
			step.Due = now.Add(level.Due)
		}
		steps = append(steps, step)
	}
	raw, _ := protojson.Marshal(held)
	request := ApprovalRequest{Record: platform.Record{ID: s.GetTarget().GetId()}, Action: declared.Schema, Title: declared.Title, App: a.Manifest().ID,
		Target: target(held), Requester: requester.ID, Submission: string(raw), Levels: steps, State: "pending"}
	if _, known := platform.Get[ApprovalRequest](c, request.ID); known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return func(r *pb.ChangeRecord) {
		if len(steps) == 0 { // no level applies: the action runs at once
			w.run(c, r, request, now)
			return
		}
		c.Put(r, request)
		w.offer(c, r, request, now)
	}, nil
}

// approvers are the members a level names, on now's day.
func (t *Tenant) approvers(level platform.ApprovalLevel, requester platform.Member, app string, now time.Time) []string {
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
		if d, ok := t.app(PlatformApp).(*Console); ok {
			add(d.holding(app, level.AppRole)...)
		}
	}
	if level.Role != "" && t.org != nil {
		day := now.UTC().Format(time.DateOnly)
		t.org.mu.Lock()
		units := t.org.units("member:"+requester.ID, "", day) // the requester's own units
		t.org.mu.Unlock()
		for _, u := range units {
			add(t.org.holders(level.Structure, u, level.Role, day)...)
		}
	}
	slices.Sort(out)
	return out
}

// offer opens the task of the request's current level for its approvers.
func (w *Work) offer(c platform.Caller, r *pb.ChangeRecord, a ApprovalRequest, now time.Time) {
	step := a.Levels[a.Level]
	task := WorkTask{Record: platform.Record{ID: fmt.Sprintf("%s#%d", a.ID, a.Level+1)}, Title: "Approve: " + a.Title + " " + a.Target,
		Body: fmt.Sprintf("%s asks, level %d of %d: %s.", a.Requester, a.Level+1, len(a.Levels), step.Title), Ref: ApprovalType + "/" + a.ID,
		App: a.App, Candidates: step.Approvers, Due: step.Due, State: "open"}
	c.Put(r, task)
	var to []platform.Recipient
	for _, m := range step.Approvers {
		to = append(to, platform.Recipient{Member: m})
	}
	c.Notify(platform.Notification{Title: task.Title, Body: task.Body, Ref: task.Ref, Key: "task:" + task.ID}, now, to...)
}

// close ends the tasks of a request's level that are still open.
func (w *Work) close(c platform.Caller, r *pb.ChangeRecord, a ApprovalRequest, level int, state string) {
	if t, ok := platform.Get[WorkTask](c, fmt.Sprintf("%s#%d", a.ID, level+1)); ok && t.State == "open" {
		t.State = state
		c.Put(r, t)
	}
}

// decider refuses a caller who is not an approver of the current level, the
// requester, or an AI agent (a person decides, ADR-0014 D6).
func (w *Work) decider(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	a := record.(*ApprovalRequest)
	step := a.Levels[a.Level]
	if c.Agent || c.ID == a.Requester || !slices.Contains(step.Approvers, c.ID) || slices.Contains(step.Approved, c.ID) {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return nil
}

// approve records the approver's approval; the level ends when one approver
// (or all, for an all-approvers level) approved; the last level runs the held
// submission now, as the requester, and its rules decide (D3).
func (w *Work) approve(c platform.Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error {
	if err := w.decider(c, record, payload, now); err != nil {
		return err
	}
	a := record.(*ApprovalRequest)
	step := &a.Levels[a.Level]
	step.Approved = append(step.Approved, c.ID)
	if step.All && len(step.Approved) < len(step.Approvers) {
		return nil // stays pending at this level
	}
	if a.Level+1 < len(a.Levels) {
		a.Level++
		return nil
	}
	a.State = "approved"
	return nil
}

// opened follows an approval: the level's task closes and the next opens, or the held action runs.
func (w *Work) opened(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	a := *record.(*ApprovalRequest)
	previous := a.Level
	if a.State == "approved" {
		w.close(c, r, a, a.Level, "done")
		w.run(c, r, a, now)
		return
	}
	if len(a.Levels[a.Level].Approved) == 0 { // a new level opened
		w.close(c, r, a, previous-1, "done")
		w.offer(c, r, a, now)
	}
}

// run submits the held submission as the requester, inside this input: a
// refusal marks the request refused with the reason.
func (w *Work) run(c platform.Caller, r *pb.ChangeRecord, a ApprovalRequest, now time.Time) {
	held := &pb.Submission{}
	protojson.Unmarshal([]byte(a.Submission), held)
	requester, _ := w.t.member(a.Requester)
	app := w.t.app(a.App)
	var err *kernel.Error = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	if app != nil {
		_, err = app.Submit(w.t.caller(requester, app, c.Replaying), held, now)
	}
	if err != nil {
		a.State, a.Outcome = "refused", err.Error()
	} else {
		a.State = "approved"
	}
	c.Put(r, a)
	c.Notify(platform.Notification{Title: fmt.Sprintf("%s %s: %s", a.Title, a.Target, a.State), Body: a.Outcome, Ref: ApprovalType + "/" + a.ID,
		Key: "request:" + a.ID}, now, platform.Recipient{Member: a.Requester})
}

func (w *Work) closed(c platform.Caller, r *pb.ChangeRecord, record any, now time.Time) {
	a := *record.(*ApprovalRequest)
	w.close(c, r, a, a.Level, "canceled")
	if a.State == "rejected" {
		c.Notify(platform.Notification{Title: fmt.Sprintf("%s %s: rejected", a.Title, a.Target), Ref: ApprovalType + "/" + a.ID,
			Key: "request:" + a.ID}, now, platform.Recipient{Member: a.Requester})
	}
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

func (w *Work) completer(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	t := record.(*WorkTask)
	if strings.HasPrefix(t.Ref, ApprovalType+"/") {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT} // an approval's task ends with the approval
	}
	if !slices.Contains(t.Candidates, c.ID) || t.Assignee != "" && t.Assignee != c.ID {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	t.Assignee = c.ID
	return nil
}

// Inbox is a member's open tasks, overdue first, then by due time.
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

// Read "inbox": the caller's open tasks; "requests": the caller's approval requests.
func (w *Work) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name == "inbox" {
		return w.inbox(c, time.Now()), nil
	}
	mine, _ := json.Marshal([]any{[]any{"requester", "=", c.ID}})
	out, _, _ := platform.Find[ApprovalRequest](c, platform.Query{Domain: mine, Sort: []string{"-id"}})
	return out, nil
}

// Run tells the people of each overdue task, once.
func (w *Work) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
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

// assign creates an app's task (Caller.Assign), resolving its recipients now.
func (t *Tenant) assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	if t.work == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	candidates := t.recipients(c, r.GetRecordedTime().AsTime(), a.To)
	work := platform.NewCaller(runtime{t}, platform.Member{ID: "app:" + WorkApp, Tenant: t.ID, Roles: map[string]string{}}, WorkApp, c.Replaying, true)
	id := fmt.Sprintf("%s:%s", c.App, a.Key)
	if a.Key == "" {
		id = fmt.Sprintf("%s:%s", c.App, r.GetChangeId())
	}
	if existing, ok := platform.Get[WorkTask](work, id); ok && existing.State == "open" {
		return nil // one open task per app and key
	}
	task := WorkTask{Record: platform.Record{ID: id}, Title: a.Title, Body: a.Body, Ref: a.Ref, App: c.App, Key: a.Key, Candidates: candidates, Due: a.Due, State: "open"}
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

// member is a member of the tenant by ID, with its current roles.
func (t *Tenant) member(id string) (platform.Member, bool) {
	d, ok := t.app(PlatformApp).(*Console)
	if !ok {
		return platform.Member{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[id]
	if m == nil {
		return platform.Member{}, false
	}
	return clone(m), true
}
