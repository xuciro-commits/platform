package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Platform operations (ADR-0013): work the host owns (event deliveries and
// scheduled jobs, K9), the tenant's connectors (K8), notifications and typed app
// settings. Everything the host runs on its own is an input: it is journaled
// and a replay runs it again through the same code (ADR-0007).

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
	since    int       // attempts before the last manual retry: each retry gets a full schedule
	event    *caused
	job      platform.Job
}

// enqueue queues the input's events for their subscribers, after the platform's
// own observers have seen them; it runs inside the input, and inside a replay.
func (t *Tenant) enqueue(now time.Time) {
	for len(t.events) > 0 {
		e := t.events[0]
		t.events = t.events[1:]
		s := e.Record.GetSubmission()
		names := append([]string{s.GetSchema().GetName()}, t.protocolEvents(e.Event)...)
		if t.relations != nil {
			t.relations.observe(t, e.Event, names[1:])
		}
		t.emit(e.Event, names)
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
			t.deliver(id, e, now)
		}
		if t.flows != nil && e.App != FlowApp && t.flows.interested(names, e.Event) { // flows start and wait on events (ADR-0020)
			t.deliver(FlowApp, e, now)
		}
	}
}

// deliver queues an event for a subscriber as owned work.
func (t *Tenant) deliver(subscriber string, e caused, now time.Time) {
	s := e.Record.GetSubmission()
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.queues[subscriber] = append(t.queues[subscriber], &Task{ID: "delivery:" + subscriber + ":" + e.App + "/" + e.Record.GetChangeId(), Kind: "delivery", App: subscriber,
		Title: s.GetSchema().GetName() + " " + target(s), State: "queued", Due: now, event: &e})
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

func (t *Tenant) automation(app string, replaying bool) platform.Caller {
	return platform.NewCaller(runtime{t}, platform.Member{ID: "app:" + app, Tenant: t.ID, Roles: map[string]string{}}, app, replaying, true)
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
	generation, _, _ := t.works.Start(task.ID, "host")
	t.hops = task.event.hops + 1
	outcome := "ok"
	var err *kernel.Error
	if f, ok := t.app(task.App).(*Flows); ok { // the host's own subscriber, on the attempt's clock
		names := append([]string{task.event.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(task.event.Event)...)
		err = f.handle(t.automation(task.App, replaying), task.event.Event, names, now)
	} else {
		err = t.app(task.App).(platform.Subscriber).Handle(t.automation(task.App, replaying), task.event.Event)
	}
	if err != nil {
		outcome = err.Error()
	}
	t.hops = 0
	t.works.Finish(task.ID, generation, outcome != "ok")
	s := task.event.Record.GetSubmission()
	t.delivered(Delivery{At: now, App: task.event.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: task.App,
		Outcome: outcome, Attempt: int(generation)})
	t.opsMu.Lock()
	task.Attempts, task.Last = int(generation), now
	done := outcome == "ok" || task.Attempts-task.since >= maxAttempts
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
		task.State, task.Error, task.Due = "retrying", outcome, now.Add(backoff(task.Attempts-task.since))
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
	before := t.acted
	generation, _, _ := t.works.Start(task.ID, "host")
	outcome := "ok"
	if err := t.app(task.App).(platform.Runner).Run(t.automation(task.App, replaying), task.job.Name, now); err != nil {
		outcome = err.Error()
	}
	t.works.Finish(task.ID, generation, outcome != "ok")
	t.opsMu.Lock()
	task.Attempts, task.Last, task.Due, task.Error = int(generation), now, now.Add(task.job.Every), map[bool]string{true: "", false: outcome}[outcome == "ok"]
	t.opsMu.Unlock()
	if !replaying && t.acted != before {
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
		task.State, task.Due, task.since = "queued", now, task.Attempts
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

const noticesKept = 2000

func (t *Tenant) notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	members := t.recipients(c, now, to)
	address := map[string]string{} // members who sign in as user:<email> may be mailed (mail.go)
	if d, ok := t.app(PlatformApp).(*Console); ok {
		for _, m := range members {
			address[m] = d.address(m)
		}
	}
	return t.notice(c, n, now, members, address)
}

// recipients are the members to resolves to on now's day (ADR-0012).
func (t *Tenant) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	var members []string
	for _, r := range to {
		if r.Member != "" && !slices.Contains(members, r.Member) {
			members = append(members, r.Member)
		}
		if r.AppRole != "" {
			if d, ok := t.app(PlatformApp).(*Console); ok {
				for _, m := range d.holding(c.App, r.AppRole) {
					if !slices.Contains(members, m) {
						members = append(members, m)
					}
				}
			}
		}
		if r.Unit != "" && t.org != nil {
			for _, m := range t.org.holders(r.Structure, r.Unit, r.Role, now.UTC().Format(time.DateOnly)) {
				if !slices.Contains(members, m) {
					members = append(members, m)
				}
			}
		}
	}
	return members
}

func (t *Tenant) notice(c platform.Caller, n platform.Notification, now time.Time, members []string, address map[string]string) []string {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	var out []string
	for _, m := range members {
		if n.Key != "" && slices.ContainsFunc(t.notices, func(x platform.Notification) bool { return x.Member == m && x.App == c.App && x.Key == n.Key }) {
			continue
		}
		t.noticeSeq++
		x := n
		x.ID, x.Member, x.App, x.At, x.Read = fmt.Sprintf("n-%d", t.noticeSeq), m, c.App, now, false
		t.notices = append(t.notices, x)
		t.mailNotice(x, address[m])
		out = append(out, m)
	}
	if len(t.notices) > noticesKept {
		t.notices = t.notices[len(t.notices)-noticesKept:]
	}
	t.trimEffects()
	t.acted += len(out)
	return out
}

// notificationsFor is member's notifications, newest first.
func (t *Tenant) notificationsFor(member string) []platform.Notification {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []platform.Notification{}
	for i := len(t.notices) - 1; i >= 0; i-- {
		if t.notices[i].Member == member {
			out = append(out, t.notices[i])
		}
	}
	return out
}

// App settings.

func (t *Tenant) setting(c platform.Caller, name string) string {
	a := t.app(c.App)
	if a == nil {
		return ""
	}
	i := slices.IndexFunc(a.Manifest().Settings, func(s platform.Setting) bool { return s.Name == name })
	if i < 0 {
		return ""
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	if v, ok := t.settings[c.App+"/"+name]; ok {
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
	platform.Setting
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

// The console's operations actions: connectors, settings, work, protocol bindings, notifications.
const (
	ConnectorType          = "platform.connector"
	SettingType            = "platform.setting"
	WorkType               = "platform.work"
	NotificationType       = "platform.notification"
	SchemaConnectorOn      = "platform.connector.enable"
	SchemaConnectorOff     = "platform.connector.disable"
	SchemaSettingSet       = "platform.setting.set"
	SchemaWorkRetry        = "platform.work.retry"
	SchemaNotificationRead = "platform.notification.read"
	ProtocolType           = "platform.protocol"
	SchemaProtocolBind     = "platform.protocol.bind"
)

func operationsActions() []platform.Action {
	admin := []string{Admin}
	return []platform.Action{
		{Schema: SchemaConnectorOn, Target: ConnectorType, Capability: "integrations", Title: "Enable connector",
			Description: "Accept the connector's deliveries again.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaConnectorOff, Target: ConnectorType, Capability: "integrations", Title: "Disable connector",
			Description: "Refuse the connector's deliveries until it is enabled; its cursor is kept.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaSettingSet, Target: SettingType, Capability: "settings", Title: "Change app setting",
			Description: "Set an app's setting (target <app>/<name>) to a value of its type.",
			Payload:     []platform.Field{{Name: "value", Type: "string", Required: true, Description: "true/false, a whole number, one of the choices, or text"}}, Roles: admin},
		{Schema: SchemaWorkRetry, Target: WorkType, Capability: "automation", Title: "Retry work",
			Description: "Queue a failed event delivery again, or run a scheduled job now.", Payload: []platform.Field{}, Roles: admin},
		{Schema: SchemaProtocolBind, Target: ProtocolType, Capability: "apps", Title: "Choose protocol provider",
			Description: "Send the tenant's new calls of a protocol (target <name>/<version>) to another app that provides it; what every provider holds stays readable.",
			Payload:     []platform.Field{{Name: "provider", Type: "string", Required: true, Description: "App ID of a provider"}}, Roles: admin},
		{Schema: SchemaNotificationRead, Target: NotificationType, Capability: "notifications", Title: "Mark notification read",
			Description: "Mark one of your notifications as read.", Payload: []platform.Field{}, Roles: []string{platform.AnyMember}},
	}
}

// The console's decisions about operations, one area per target type.

func (t *Tenant) decideConnector(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	t.opsMu.Lock()
	d := t.descriptors[s.GetTarget().GetId()]
	t.opsMu.Unlock()
	if d == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	off := s.GetSchema().GetName() == SchemaConnectorOff
	return func(*pb.ChangeRecord) { t.opsMu.Lock(); d.Disabled = off; t.opsMu.Unlock() }, nil
}

func (t *Tenant) decideSetting(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Value string }
	json.Unmarshal(s.GetPayload(), &p)
	app, name, _ := strings.Cut(id, "/")
	a := t.app(app)
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	i := slices.IndexFunc(a.Manifest().Settings, func(x platform.Setting) bool { return x.Name == name })
	if i < 0 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if !a.Manifest().Settings[i].Accepts(p.Value) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return func(*pb.ChangeRecord) { t.opsMu.Lock(); t.settings[id] = p.Value; t.opsMu.Unlock() }, nil
}

func (t *Tenant) decideBinding(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Provider string }
	json.Unmarshal(s.GetPayload(), &p)
	if !slices.ContainsFunc(t.providers(id), func(b binding) bool { return b.provider.Manifest().ID == p.Provider }) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return func(*pb.ChangeRecord) { t.rebind(id, p.Provider) }, nil
}

func (t *Tenant) decideWork(_ platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	t.opsMu.Lock()
	known := slices.ContainsFunc(t.failed, func(x *Task) bool { return x.ID == id }) || slices.ContainsFunc(t.jobs, func(x *Task) bool { return x.ID == id })
	t.opsMu.Unlock()
	if !known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return func(*pb.ChangeRecord) { t.retry(id, now) }, nil
}

func (t *Tenant) decideNotification(c platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	t.opsMu.Lock()
	i := slices.IndexFunc(t.notices, func(x platform.Notification) bool { return x.ID == id })
	mine := i >= 0 && t.notices[i].Member == c.ID
	t.opsMu.Unlock()
	if i < 0 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if !mine {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return func(*pb.ChangeRecord) {
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		if i := slices.IndexFunc(t.notices, func(x platform.Notification) bool { return x.ID == id }); i >= 0 {
			t.notices[i].Read = true
		}
	}, nil
}
