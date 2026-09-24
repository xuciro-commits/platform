package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Platform operations (ADR-0013): work the host owns (event deliveries and
// scheduled jobs, K9), the tenant's connectors (K8), notifications and typed app
// settings. Everything the host runs on its own is an input: it is journaled
// and a replay runs it again through the same code (ADR-0007).

// Job is work an app runs on a schedule, as "app:<id>" (manifest).
type Job struct {
	Name  string        `json:"name"`
	Title string        `json:"title"`
	Every time.Duration `json:"every"`
}

// Runner is an app with scheduled jobs. A run acts only through decisions and
// notifications, so a run that did neither needs no journal entry.
type Runner interface {
	Run(c Caller, job string, now time.Time) *kernel.Error
}

// Setting is a typed per-tenant value an app declares and administrators set in
// Settings; a value within the app's rules, never a rule (ADR-0008 point 2).
type Setting struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Type        string   `json:"type"` // boolean, integer, text, choice
	Default     string   `json:"default"`
	Choices     []string `json:"choices,omitempty"`
}

func (s Setting) accepts(v string) bool {
	switch s.Type {
	case "boolean":
		return v == "true" || v == "false"
	case "integer":
		_, err := strconv.Atoi(v)
		return err == nil
	case "choice":
		return slices.Contains(s.Choices, v)
	}
	return s.Type == "text"
}

const (
	maxAttempts = 5   // a delivery then fails and its queue moves on
	maxHops     = 100 // events caused by handlers of events …: a subscription cycle
	workBurst   = 100 // attempts per subscriber per Work call
)

func backoff(attempts int) time.Duration { return time.Second << attempts } // 2 s, 4 s, 8 s, 16 s

// Task is one piece of owned work: an event for a subscriber, or a job.
type Task struct {
	ID       string    `json:"id"`
	Kind     string    `json:"kind"` // delivery, job
	App      string    `json:"app"`  // the subscriber, or the job's app
	Title    string    `json:"title"`
	State    string    `json:"state"` // queued, retrying, failed, scheduled
	Attempts int       `json:"attempts"`
	Last     time.Time `json:"last,omitzero"`
	Due      time.Time `json:"due,omitzero"`
	Error    string    `json:"error,omitempty"`
	event    *Event
	job      Job
}

// enqueue queues the input's events for their subscribers, after the platform's
// own observers have seen them; it runs inside the input, and inside a replay.
func (t *Tenant) enqueue(now time.Time) {
	for len(t.events) > 0 {
		e := t.events[0]
		t.events = t.events[1:]
		s := e.Record.GetSubmission()
		names := append([]string{s.GetSchema().GetName()}, t.protocolEvents(e)...)
		if t.relations != nil {
			t.relations.observe(t, e, names[1:])
		}
		for _, a := range t.apps {
			if !slices.ContainsFunc(a.Manifest().Subscribes, func(x string) bool { return slices.Contains(names, x) }) {
				continue
			}
			id := a.Manifest().ID
			if e.hops > maxHops {
				t.delivered(Delivery{At: now, App: e.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: id,
					Outcome: fmt.Sprintf("stopped: more than %d events caused by events", maxHops)})
				continue
			}
			ev := e
			t.opsMu.Lock()
			t.queues[id] = append(t.queues[id], &Task{ID: "delivery:" + id + ":" + e.App + "/" + e.Record.GetChangeId(), Kind: "delivery", App: id,
				Title: s.GetSchema().GetName() + " " + target(s), State: "queued", Due: now, event: &ev})
			t.opsMu.Unlock()
		}
	}
}

func target(s *pb.Submission) string { return s.GetTarget().GetType() + "/" + s.GetTarget().GetId() }

func (t *Tenant) delivered(d Delivery) {
	t.auditMu.Lock()
	defer t.auditMu.Unlock()
	t.deliveries = append(t.deliveries, d)
	if len(t.deliveries) > auditKept {
		t.deliveries = t.deliveries[len(t.deliveries)-auditKept:]
	}
}

func (t *Tenant) automation(app string, replaying bool) Caller {
	return Caller{Member: Member{ID: "app:" + app, Tenant: t.ID, Roles: map[string]string{}}, App: app, Replaying: replaying, Automation: true, tenant: t}
}

type workBody struct {
	Work    string `json:"work"`
	Outcome string `json:"outcome"`
}

// Work runs what is due at now: the head of each subscriber's queue, and jobs.
// The host calls it every second; tests call it with their clock.
func (t *Tenant) Work(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, a := range t.apps {
		for range workBurst {
			t.opsMu.Lock()
			q := t.queues[a.Manifest().ID]
			var head *Task
			if len(q) > 0 && !q[0].Due.After(now) {
				head = q[0]
			}
			t.opsMu.Unlock()
			if head == nil {
				break
			}
			t.attempt(head, now, false)
		}
	}
	for _, j := range t.jobs {
		if !j.Due.After(now) {
			t.run(j, now, false)
		}
	}
}

// attempt hands a queued event to its subscriber once; the outcome is journaled.
func (t *Tenant) attempt(task *Task, now time.Time, replaying bool) string {
	sub := t.app(task.App).(Subscriber)
	generation, _, _ := t.works.Start(task.ID, "host")
	t.hops = task.event.hops + 1
	outcome := "ok"
	if err := sub.Handle(t.automation(task.App, replaying), *task.event); err != nil {
		outcome = err.Error()
	}
	t.hops = 0
	t.works.Finish(task.ID, generation, outcome != "ok")
	s := task.event.Record.GetSubmission()
	t.delivered(Delivery{At: now, App: task.event.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: task.App,
		Outcome: outcome, Attempt: int(generation)})
	t.opsMu.Lock()
	task.Attempts, task.Last = int(generation), now
	done := outcome == "ok" || task.Attempts >= maxAttempts
	if done {
		t.queues[task.App] = slices.DeleteFunc(t.queues[task.App], func(x *Task) bool { return x == task })
	}
	switch {
	case outcome == "ok":
		task.State, task.Error = "done", ""
	case done:
		task.State, task.Error = "failed", outcome
		t.failed = append(t.failed, task)
	default:
		task.State, task.Error, task.Due = "retrying", outcome, now.Add(backoff(task.Attempts))
	}
	t.opsMu.Unlock()
	if !replaying {
		body, _ := json.Marshal(workBody{Work: task.ID, Outcome: outcome})
		t.record(t.app(task.App), "delivery", t.automation(task.App, false).Member, body, now)
	}
	t.enqueue(now)
	return outcome
}

// run runs a job once; it is journaled when it decided or notified something.
func (t *Tenant) run(task *Task, now time.Time, replaying bool) string {
	before := t.effects
	generation, _, _ := t.works.Start(task.ID, "host")
	outcome := "ok"
	if err := t.app(task.App).(Runner).Run(t.automation(task.App, replaying), task.job.Name, now); err != nil {
		outcome = err.Error()
	}
	t.works.Finish(task.ID, generation, outcome != "ok")
	t.opsMu.Lock()
	task.Attempts, task.Last, task.Due, task.Error = int(generation), now, now.Add(task.job.Every), map[bool]string{true: "", false: outcome}[outcome == "ok"]
	t.opsMu.Unlock()
	if !replaying && t.effects != before {
		body, _ := json.Marshal(workBody{Work: task.ID, Outcome: outcome})
		t.record(t.app(task.App), "job", t.automation(task.App, false).Member, body, now)
	}
	t.enqueue(now)
	return outcome
}

// replayWork runs a journaled attempt or job run again; it must end the same way.
func (t *Tenant) replayWork(kind string, raw []byte, at time.Time) error {
	var b workBody
	if json.Unmarshal(raw, &b) != nil {
		return fmt.Errorf("bad %s entry", kind)
	}
	var task *Task
	t.opsMu.Lock()
	if kind == "job" {
		if i := slices.IndexFunc(t.jobs, func(x *Task) bool { return x.ID == b.Work }); i >= 0 {
			task = t.jobs[i]
		}
	} else {
		for _, q := range t.queues {
			if len(q) > 0 && q[0].ID == b.Work {
				task = q[0]
			}
		}
	}
	t.opsMu.Unlock()
	if task == nil {
		return fmt.Errorf("%s %s is not due in the replay", kind, b.Work)
	}
	outcome := map[bool]func(*Task, time.Time, bool) string{true: t.run, false: t.attempt}[kind == "job"](task, at, true)
	if outcome != b.Outcome {
		return fmt.Errorf("%s %s ended %s, recorded %s", kind, b.Work, outcome, b.Outcome)
	}
	return nil
}

// Tasks lists the tenant's owned work: jobs, queued and retrying deliveries, and failed ones.
func (t *Tenant) Tasks() []Task {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []Task{}
	for _, j := range t.jobs {
		out = append(out, *j)
	}
	for _, a := range t.apps {
		for _, q := range t.queues[a.Manifest().ID] {
			out = append(out, *q)
		}
	}
	for _, f := range t.failed {
		out = append(out, *f)
	}
	return out
}

// retry queues a failed delivery again at the head of its queue, or makes a job due.
func (t *Tenant) retry(id string, now time.Time) bool {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	if i := slices.IndexFunc(t.failed, func(x *Task) bool { return x.ID == id }); i >= 0 {
		task := t.failed[i]
		t.failed = slices.Delete(t.failed, i, i+1)
		task.State, task.Due = "queued", now
		t.queues[task.App] = append([]*Task{task}, t.queues[task.App]...)
		return true
	}
	if i := slices.IndexFunc(t.jobs, func(x *Task) bool { return x.ID == id }); i >= 0 {
		t.jobs[i].Due = now
		return true
	}
	return false
}

// Connectors (K8): the deployment connects descriptors; the connector is the
// member that delivers. Cursors and deliveries replay; heartbeats and the last
// refused input are volatile.

// ConnectorView is a connector as Settings shows it.
type ConnectorView struct {
	ID          string          `json:"id"`
	Direction   string          `json:"direction"`
	DataClasses []string        `json:"dataClasses"`
	Heartbeat   string          `json:"heartbeat"`
	Health      string          `json:"health"`
	LastSeen    *time.Time      `json:"lastSeen,omitempty"`
	Cursor      string          `json:"cursor,omitempty"`
	Disabled    bool            `json:"disabled"`
	LastError   *ConnectorError `json:"lastError,omitempty"`
}

type ConnectorError struct {
	At    time.Time `json:"at"`
	Input string    `json:"input"`
	Error string    `json:"error"`
}

// Connect registers connectors with the tenant, before its journal is replayed.
func (t *Tenant) Connect(descriptors ...*pb.ConnectorDescriptor) error {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, d := range descriptors {
		d = proto.Clone(d).(*pb.ConnectorDescriptor)
		d.TenantId = t.ID
		if err := t.connectors.Register(d); err != nil {
			return fmt.Errorf("tenant %s: connector %s: %v", t.ID, d.GetConnectorId(), err)
		}
		t.descriptors[d.GetConnectorId()] = d
	}
	return nil
}

// Deliver accepts one batch from the calling connector about dataClass; a poll
// page moves the cursor from → to (K8).
func (c Caller) Deliver(dataClass, from, to string, now time.Time) *kernel.Error {
	if c.tenant == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	c.tenant.opsMu.Lock()
	defer c.tenant.opsMu.Unlock()
	return c.tenant.connectors.Deliver(c.Tenant, c.ID, dataClass, from, to, now)
}

func (t *Tenant) refused(member, input string, err *kernel.Error, now time.Time) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	if t.descriptors[member] != nil {
		t.lastError[member] = ConnectorError{At: now, Input: input, Error: err.Error()}
	}
}

func (t *Tenant) Connectors(now time.Time) []ConnectorView {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []ConnectorView{}
	for _, d := range t.connectors.Descriptors(t.ID) {
		s, _ := t.connectors.Status(t.ID, d.GetConnectorId(), now)
		v := ConnectorView{ID: d.GetConnectorId(), Direction: strings.ToLower(strings.TrimPrefix(d.GetDirection().String(), "CONNECTOR_DIRECTION_")),
			DataClasses: d.GetDataClasses(), Heartbeat: d.GetHeartbeat().AsDuration().String(), Cursor: s.GetCursor(), Disabled: d.GetDisabled(),
			Health: strings.ToLower(strings.TrimPrefix(s.GetHealth().String(), "CONNECTOR_HEALTH_"))}
		if s.GetLastSeen() != nil {
			seen := s.GetLastSeen().AsTime()
			v.LastSeen = &seen
		}
		if e, ok := t.lastError[d.GetConnectorId()]; ok {
			v.LastError = &e
		}
		out = append(out, v)
	}
	return out
}

// Notifications: an app tells members something from any input (ADR-0013).

// Recipient is a member, or whoever holds a membership (with Role, when given)
// in Unit or in a unit above it in Structure (ADR-0012).
type Recipient struct {
	Member                string
	Structure, Unit, Role string
}

type Notification struct {
	ID     string    `json:"id"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Title  string    `json:"title"`
	Body   string    `json:"body,omitempty"`
	Ref    string    `json:"ref,omitempty"` // the entity it is about, "<type>/<id>"
	Key    string    `json:"key,omitempty"` // one notification per member, app and key
	At     time.Time `json:"at"`
	Read   bool      `json:"read"`
}

const noticesKept = 2000

// Notify resolves recipients on now's day and gives each one n, unless it has
// one with n's key from this app already; it returns who received it.
func (c Caller) Notify(n Notification, now time.Time, to ...Recipient) []string {
	t := c.tenant
	if t == nil {
		return nil
	}
	var members []string
	for _, r := range to {
		if r.Member != "" && !slices.Contains(members, r.Member) {
			members = append(members, r.Member)
		}
		if r.Unit != "" && t.org != nil {
			for _, m := range t.org.holders(r.Structure, r.Unit, r.Role, now.UTC().Format(time.DateOnly)) {
				if !slices.Contains(members, m) {
					members = append(members, m)
				}
			}
		}
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	var out []string
	for _, m := range members {
		if n.Key != "" && slices.ContainsFunc(t.notices, func(x Notification) bool { return x.Member == m && x.App == c.App && x.Key == n.Key }) {
			continue
		}
		t.noticeSeq++
		x := n
		x.ID, x.Member, x.App, x.At, x.Read = fmt.Sprintf("n-%d", t.noticeSeq), m, c.App, now, false
		t.notices = append(t.notices, x)
		out = append(out, m)
	}
	if len(t.notices) > noticesKept {
		t.notices = t.notices[len(t.notices)-noticesKept:]
	}
	t.effects += len(out)
	return out
}

// notificationsFor is member's notifications, newest first.
func (t *Tenant) notificationsFor(member string) []Notification {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []Notification{}
	for i := len(t.notices) - 1; i >= 0; i-- {
		if t.notices[i].Member == member {
			out = append(out, t.notices[i])
		}
	}
	return out
}

// App settings.

// Setting is the current value of the calling app's setting (its default until set).
func (c Caller) Setting(name string) string {
	if c.tenant == nil {
		return ""
	}
	a := c.tenant.app(c.App)
	if a == nil {
		return ""
	}
	i := slices.IndexFunc(a.Manifest().Settings, func(s Setting) bool { return s.Name == name })
	if i < 0 {
		return ""
	}
	c.tenant.opsMu.Lock()
	defer c.tenant.opsMu.Unlock()
	if v, ok := c.tenant.settings[c.App+"/"+name]; ok {
		return v
	}
	return a.Manifest().Settings[i].Default
}

// AppSettings are an app's declared settings with their current values.
type AppSettings struct {
	App      string         `json:"app"`
	Settings []SettingValue `json:"settings"`
}

type SettingValue struct {
	Setting
	Value string `json:"value"`
}

func (t *Tenant) Settings() []AppSettings {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []AppSettings{}
	for _, a := range t.apps {
		m := a.Manifest()
		if len(m.Settings) == 0 {
			continue
		}
		s := AppSettings{App: m.ID}
		for _, x := range m.Settings {
			v, ok := t.settings[m.ID+"/"+x.Name]
			if !ok {
				v = x.Default
			}
			s.Settings = append(s.Settings, SettingValue{Setting: x, Value: v})
		}
		out = append(out, s)
	}
	return out
}

// The platform app's operations decisions: connectors, settings, work, notifications.
const (
	ConnectorType      = "platform.connector"
	SettingType        = "platform.setting"
	WorkType           = "platform.work"
	NotificationType   = "platform.notification"
	SchemaConnectorOn  = "platform.connector.enable"
	SchemaConnectorOff = "platform.connector.disable"
	SchemaSettingSet   = "platform.setting.set"
	SchemaWorkRetry    = "platform.work.retry"
	SchemaNoticeRead   = "platform.notification.read"
)

func operationsActions() []Action {
	admin := []string{Admin}
	return []Action{
		{Schema: SchemaConnectorOn, Target: ConnectorType, Capability: "integrations", Title: "Enable connector",
			Description: "Accept the connector's deliveries again.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaConnectorOff, Target: ConnectorType, Capability: "integrations", Title: "Disable connector",
			Description: "Refuse the connector's deliveries until it is enabled; its cursor is kept.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaSettingSet, Target: SettingType, Capability: "settings", Title: "Change app setting",
			Description: "Set an app's setting (target <app>/<name>) to a value of its type.",
			Payload:     []Field{{Name: "value", Type: "string", Required: true, Description: "true/false, a whole number, one of the choices, or text"}}, Roles: admin},
		{Schema: SchemaWorkRetry, Target: WorkType, Capability: "automation", Title: "Retry work",
			Description: "Queue a failed event delivery again, or run a scheduled job now.", Payload: []Field{}, Roles: admin},
		{Schema: SchemaNoticeRead, Target: NotificationType, Capability: "notifications", Title: "Mark notification read",
			Description: "Mark one of your notifications as read.", Payload: []Field{}, Roles: []string{AnyMember}},
	}
}

// operate decides the platform app's operations actions; ok is false for other actions.
func operate(c Caller, s *pb.Submission, now time.Time) (apply func(*pb.ChangeRecord), err *kernel.Error, ok bool) {
	t := c.tenant
	id := s.GetTarget().GetId()
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	switch s.GetSchema().GetName() {
	case SchemaConnectorOn, SchemaConnectorOff:
		t.opsMu.Lock()
		d := t.descriptors[id]
		t.opsMu.Unlock()
		if d == nil {
			return nil, notFound, true
		}
		off := s.GetSchema().GetName() == SchemaConnectorOff
		return func(*pb.ChangeRecord) { t.opsMu.Lock(); d.Disabled = off; t.opsMu.Unlock() }, nil, true
	case SchemaSettingSet:
		var p struct{ Value string }
		json.Unmarshal(s.GetPayload(), &p)
		app, name, _ := strings.Cut(id, "/")
		a := t.app(app)
		if a == nil {
			return nil, notFound, true
		}
		i := slices.IndexFunc(a.Manifest().Settings, func(x Setting) bool { return x.Name == name })
		if i < 0 {
			return nil, notFound, true
		}
		if !a.Manifest().Settings[i].accepts(p.Value) {
			return nil, invalid, true
		}
		return func(*pb.ChangeRecord) { t.opsMu.Lock(); t.settings[id] = p.Value; t.opsMu.Unlock() }, nil, true
	case SchemaWorkRetry:
		t.opsMu.Lock()
		known := slices.ContainsFunc(t.failed, func(x *Task) bool { return x.ID == id }) || slices.ContainsFunc(t.jobs, func(x *Task) bool { return x.ID == id })
		t.opsMu.Unlock()
		if !known {
			return nil, notFound, true
		}
		return func(*pb.ChangeRecord) { t.retry(id, now) }, nil, true
	case SchemaNoticeRead:
		t.opsMu.Lock()
		i := slices.IndexFunc(t.notices, func(x Notification) bool { return x.ID == id })
		mine := i >= 0 && t.notices[i].Member == c.ID
		t.opsMu.Unlock()
		if i < 0 {
			return nil, notFound, true
		}
		if !mine {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}, true
		}
		return func(*pb.ChangeRecord) {
			t.opsMu.Lock()
			defer t.opsMu.Unlock()
			if i := slices.IndexFunc(t.notices, func(x Notification) bool { return x.ID == id }); i >= 0 {
				t.notices[i].Read = true
			}
		}, nil, true
	}
	return nil, nil, false
}
