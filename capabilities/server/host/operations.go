package host

import automation "platformserver/services/automate/runtime"

import (
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/transport"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	workBurst = 100
)

var deliveryRetry = platform.Retry{Initial: 2 * time.Second, Max: time.Minute, Attempts: 5}

func (t *Tenant) retryOf(app string) platform.Retry {
	if a := t.app(app); a != nil && a.Manifest().Retry != nil {
		return *a.Manifest().Retry
	}
	return deliveryRetry
}

func (t *Tenant) enqueue(now time.Time) {
	for len(t.events) > 0 {
		e := t.events[0]
		t.events = t.events[1:]
		s := e.Record.GetSubmission()
		names := append([]string{s.GetSchema().GetName()}, t.protocolEvents(e.Event)...)
		if e.Plan != nil {
			for _, id := range e.Plan.Stopped {
				t.delivered(platform.Delivery{At: now, App: e.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: id,
					Outcome: fmt.Sprintf("stopped: more than %d events caused by events", acceptedformat.MaxHops)})
			}
			names = e.Plan.Names
		}
		if e.Plan == nil || !e.Plan.Observed {
			for _, o := range t.observers {
				o.Observe(e.Event, names[1:])
			}
		}
		if e.Plan != nil {
			t.opsMu.Lock()
			for _, planned := range e.Plan.Effects {
				t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
			}
			t.trimEffects()
			t.opsMu.Unlock()
			for _, id := range e.Plan.Subscribers {
				if t.app(id) == nil {
					panic(fmt.Errorf("accepted result requires missing subscriber %s", id))
				}
				t.deliver(id, e, now)
			}
			continue
		}
		t.emit(e.Event, names)
		for _, a := range t.apps {
			if !slices.ContainsFunc(a.Manifest().Subscribes, func(x string) bool { return slices.Contains(names, x) }) {
				continue
			}
			id := a.Manifest().ID
			if e.Hops > acceptedformat.MaxHops {
				t.delivered(platform.Delivery{At: now, App: e.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: id,
					Outcome: fmt.Sprintf("stopped: more than %d events caused by events", acceptedformat.MaxHops)})
				continue
			}
			t.deliver(id, e, now)
		}
		for _, id := range t.listeners {
			if id != e.App && t.app(id).(platform.Listener).Interested(names, e.Event) {
				t.deliver(id, e, now)
			}
		}
	}
}

func (t *Tenant) deliver(subscriber string, e automation.QueuedEvent, now time.Time) {
	s := e.Record.GetSubmission()
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.work.Queue(subscriber, &automation.Task{ScheduledTask: platform.ScheduledTask{ID: "delivery:" + subscriber + ":" + e.App + "/" + e.Record.GetChangeId(), Kind: "delivery", App: subscriber,
		Title: s.GetSchema().GetName() + " " + target(s), State: "queued", Due: now.UTC()}, Event: &e})
}

func target(s *pb.Submission) string { return s.GetTarget().GetType() + "/" + s.GetTarget().GetId() }

func (t *Tenant) delivered(d platform.Delivery) { t.audit.Delivered(d) }

func (t *Tenant) automation(app string, replaying bool) platform.Caller {
	return platform.NewCaller(runtime{t: t, scoped: true}, platform.Member{ID: "app:" + app, Tenant: t.ID, Roles: map[string]string{}}, app, replaying, true)
}

func (t *Tenant) automated(parent platform.Caller, app string) platform.Caller {
	if parent.Staging() {
		return platform.ActingCaller(parent, platform.Member{ID: "app:" + app, Tenant: t.ID}, app, true)
	}
	return t.automation(app, parent.Replaying)
}

func (t *Tenant) Work(now time.Time) {
	for range workBurst {
		if !t.Round(now, len(t.apps)) {
			return
		}
	}
}

func (t *Tenant) Round(now time.Time, budget int) (more bool) {
	for range max(budget, 1) {
		if more = t.step(now); !more {
			return false
		}
	}
	return more
}

func (t *Tenant) step(now time.Time) (more bool) {
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("owned work panicked: %v", failure))
			more = false
		}
	}()
	type progress struct {
		more  bool
		took  int
		scope *Tenant
	}
	result, refusal := decideOnImage(t, context.Background(), "work", func(scope *Tenant) (progress, *kernel.Error) {
		more, took := scope.workLoop().Round(now, 1)
		if scope.readOrigin == nil && took > 0 {
			scope.WorkEpoch.Add(1)
		}
		return progress{more: more, took: took, scope: scope}, nil
	}, func(t *Tenant, candidate preparedAccepted, value progress, refusal *kernel.Error) (progress, *kernel.Error) {
		if value.took == 0 {
			return value, refusal
		}
		if len(candidate.entry.Body) > 0 {
			committed, err := t.acceptResult(candidate.entry, candidate.key, candidate.hash)
			if err != nil {
				return progress{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			t.finishCommittedWork(candidate.entry.App, candidate.key, candidate.hash, committed)
			if t.quarantined() {
				return progress{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
		}
		t.opsMu.Lock()
		t.rotation = value.scope.rotation
		for _, job := range value.scope.work.Jobs {
			if current := t.work.Job(job.ID); current != nil {
				current.NextRun = job.NextRun
			}
		}
		t.opsMu.Unlock()
		t.WorkEpoch.Add(1)
		value.more = t.workLoop().Pending(now)
		return value, refusal
	})
	return refusal == nil && result.more
}

func (t *Tenant) attempt(task *automation.Task, now time.Time, replaying bool) string {
	if !replaying {
		return t.acceptWork(task, now)
	}
	generation, _, _ := t.works.Start(task.ID, "host")
	t.hops = task.Event.Hops + 1
	outcome := "ok"
	end := func(string) {}
	if !replaying {
		end = t.begin("deliver "+task.Event.Record.GetSubmission().GetSchema().GetName()+" to "+task.App, task.Event.Span,
			attribute.String("platform.app", task.App), attribute.String("platform.target", target(task.Event.Record.GetSubmission())))
		defer func() { end(outcome); counted(t, "delivery", task.App, outcome) }()
	}
	var err *kernel.Error
	if x, ok := t.app(task.App).(platform.Listener); ok {
		names := append([]string{task.Event.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(task.Event.Event)...)
		if task.Event.Plan != nil {
			names = task.Event.Plan.Names
		}
		err = x.Listen(t.automation(task.App, replaying), task.Event.Event, names, now)
	} else {
		err = t.app(task.App).(platform.Subscriber).Handle(t.automation(task.App, replaying), task.Event.Event)
	}
	if err != nil {
		outcome = err.Error()
	}
	t.hops = 0
	t.works.Finish(task.ID, generation, outcome != "ok")
	s := task.Event.Record.GetSubmission()
	t.delivered(platform.Delivery{At: now, App: task.Event.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: task.App,
		Outcome: outcome, Attempt: int(generation)})
	t.opsMu.Lock()
	task.Attempts, task.Last = int(generation), now
	retry := t.retryOf(task.App)
	done := outcome == "ok" || task.Attempts-task.Since >= retry.Attempts
	switch {
	case outcome == "ok":
		task.State, task.Error = "done", ""
	case done:
		task.State, task.Error = "failed", outcome
	default:
		task.State, task.Error, task.Due = "retrying", outcome, now.Add(retry.After(task.Attempts-task.Since))
	}
	if done {
		t.work.Settled(task)
	}
	t.opsMu.Unlock()
	if task.State == "failed" {
		t.failedWork("Delivery to "+task.App+" failed: "+task.Title, outcome, task.ID, now, replaying)
	}
	runStatus := "ok"
	if outcome != "ok" {
		runStatus = "failed"
	}
	t.observeOwnerRun(now, "run", task.Kind+":"+task.App+":"+task.ID, task.App, target(s), runStatus,
		"Delivery to "+task.App, outcome)

	t.enqueue(now)
	return outcome
}

func (t *Tenant) run(task *automation.Task, now time.Time, replaying bool) string {
	if !replaying {
		outcome := t.acceptWork(task, now)
		t.opsMu.Lock()
		task.NextRun = now.Add(task.Job.Every)
		t.opsMu.Unlock()
		return outcome
	}
	generation, _, _ := t.works.Start(task.ID, "host")
	outcome := "ok"
	if err := t.app(task.App).(platform.Runner).Run(t.automation(task.App, replaying), task.Job.Name, now); err != nil {
		outcome = err.Error()
	}
	t.works.Finish(task.ID, generation, outcome != "ok")
	t.opsMu.Lock()
	task.Attempts, task.Last, task.Due, task.Error = int(generation), now, now.Add(task.Job.Every), map[bool]string{true: "", false: outcome}[outcome == "ok"]
	t.opsMu.Unlock()
	runStatus := "ok"
	if outcome != "ok" {
		runStatus = "failed"
	}
	t.observeOwnerRun(now, "worker", "job:"+task.App+":"+task.ID, task.App, task.ID, runStatus, "Scheduled job "+task.Title, outcome)

	t.enqueue(now)
	return outcome
}

func (t *Tenant) failedWork(title, why, id string, now time.Time, replaying bool) {
	if t.app(controlowner.PlatformApp) == nil {
		return
	}
	t.automation(controlowner.PlatformApp, replaying).Notify(platform.Notification{Title: title, Body: why + ". Retry it in Settings → Automation once the cause is fixed.",
		Key: "failed:" + id}, now, platform.Recipient{AppRole: controlowner.Admin})
}

func (t *Tenant) Tasks() []automation.Task {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	return t.work.All(t.apps)
}

func (t *Tenant) retry(id string, now time.Time) bool {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	return t.work.Retry(id, now)
}

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

func (t *Tenant) Connect(descriptors ...*pb.ConnectorDescriptor) error {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, d := range descriptors {
		d = proto.Clone(d).(*pb.ConnectorDescriptor)
		d.TenantId = t.ID
		if err := t.connectors.register(d); err != nil {
			return fmt.Errorf("tenant %s: connector %s: %v", t.ID, d.GetConnectorId(), err)
		}
	}
	return nil
}

func (t *Tenant) refused(member, input string, err *kernel.Error, now time.Time) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.connectors.refused(member, input, err, now)
}

func (t *Tenant) Connectors(now time.Time) []ConnectorView {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := []ConnectorView{}
	for _, d := range t.connectors.kernel.Descriptors(t.ID) {
		s, _ := t.connectors.kernel.Status(t.ID, d.GetConnectorId(), now)
		v := ConnectorView{ID: d.GetConnectorId(), Direction: strings.ToLower(strings.TrimPrefix(d.GetDirection().String(), "CONNECTOR_DIRECTION_")),
			DataClasses: d.GetDataClasses(), Heartbeat: d.GetHeartbeat().AsDuration().String(), Cursor: s.GetCursor(), Disabled: d.GetDisabled(),
			Health: strings.ToLower(strings.TrimPrefix(s.GetHealth().String(), "CONNECTOR_HEALTH_"))}
		if s.GetLastSeen() != nil {
			seen := s.GetLastSeen().AsTime()
			v.LastSeen = &seen
		}
		if e, ok := t.connectors.lastError[d.GetConnectorId()]; ok {
			v.LastError = &e
		}
		out = append(out, v)
	}
	return out
}

func (t *Tenant) notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	members := t.recipients(c, now, to)
	delivery := map[string]platform.Reach{}
	if d, ok := t.app(controlowner.PlatformApp).(platform.IdentityProfile); ok {
		for _, m := range members {
			delivery[m] = d.Reach(m)
		}
	}
	return t.notice(c, n, now, members, delivery)
}

func (t *Tenant) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	var members []string
	for _, r := range to {
		if r.Member != "" && !slices.Contains(members, r.Member) {
			members = append(members, r.Member)
		}
		if r.AppRole != "" {
			if d, ok := t.app(controlowner.PlatformApp).(platform.IdentityDirectory); ok {
				for _, m := range d.Holding(c.App, r.AppRole) {
					if !slices.Contains(members, m) {
						members = append(members, m)
					}
				}
			}
		}
		if r.Unit != "" && t.directory != nil {
			for _, m := range t.directory.Holders(r.Structure, r.Unit, r.Role, now.UTC().Format(time.DateOnly)) {
				if !slices.Contains(members, m) {
					members = append(members, m)
				}
			}
		}
	}
	return members
}

func (t *Tenant) notice(c platform.Caller, n platform.Notification, now time.Time, members []string, delivery map[string]platform.Reach) []string {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	var out []string
	for _, m := range members {
		to, known := delivery[m]
		if !known {
			to = platform.Reach{InApp: true}
		}
		x := n
		x.App, x.At, x.Read = c.App, now, !to.InApp
		x, posted := t.notices.Post(x, m)
		if !posted {
			continue
		}
		t.mailNotice(x, to)
		out = append(out, m)
	}
	t.trimEffects()
	t.acted += len(out)
	return out
}

func (t *Tenant) setting(c platform.Caller, name string) string {
	app := c.App
	if tenantWide, ok := strings.CutPrefix(name, controlowner.PlatformApp+"/"); ok {
		app, name = controlowner.PlatformApp, tenantWide
	}
	a := t.app(app)
	if a == nil {
		return ""
	}
	if v, ok := t.settings.Get(app + "/" + name); ok {
		return v
	}
	settings := transport.ManifestOf(a, t.definitionsFingerprint()).Settings
	i := slices.IndexFunc(settings, func(s platform.Setting) bool { return s.Name == name })
	if i < 0 {
		return ""
	}
	return settings[i].Default
}

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
			v, ok := t.settings.Get(m.ID + "/" + x.Name)
			if !ok {
				v = x.Default
			}
			s.Settings = append(s.Settings, SettingValue{Setting: x, Value: v})
		}
		out = append(out, s)
	}
	return out
}

func (t *Tenant) decideConnector(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	t.opsMu.Lock()
	d := t.connectors.byID[s.GetTarget().GetId()]
	t.opsMu.Unlock()
	if d == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	off := s.GetSchema().GetName() == controlowner.SchemaConnectorOff
	return func(*pb.ChangeRecord) { t.opsMu.Lock(); d.Disabled = off; t.opsMu.Unlock() }, nil
}

func (t *Tenant) decideSetting(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Value string }
	json.Unmarshal(s.GetPayload(), &p)
	app, name, _ := strings.Cut(id, "/")
	a := t.app(app)
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if app == controlowner.PlatformApp && strings.HasPrefix(name, "logPolicy/") {
		targetApp := strings.TrimPrefix(name, "logPolicy/")
		if t.app(targetApp) == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if !c.Replaying {
			if refusal := validateLogPolicy(p.Value, now, t.platformSetting(controlowner.SettingLogLevel)); refusal != nil {
				return nil, refusal
			}
		}
		return func(*pb.ChangeRecord) { t.settings.Set(id, p.Value) }, nil
	}
	if !c.Replaying {
		if name == controlowner.SettingLogQuota {
			quota, err := strconv.Atoi(p.Value)
			if err != nil || quota < 1 || quota > 1<<20 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The daily log quota must be between 1 and 1048576 MiB")
			}
		}
		if name == controlowner.SettingLogDebugUntil {
			if p.Value != "" {
				until, err := time.Parse(time.RFC3339, p.Value)
				if err != nil || until.Before(now) || until.After(now.Add(30*time.Minute)) {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug must end within the next 30 minutes")
				}
				if strings.TrimSpace(t.platformSetting(controlowner.SettingLogDebugReason)) == "" {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug needs a reason")
				}
			}
		}
		if name == controlowner.SettingLogDebugReason && len(p.Value) > 500 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug reason is too long")
		}
		if name == controlowner.SettingLogLevel && p.Value == "debug" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a temporary debug window instead of a permanent debug level")
		}
	}
	i := slices.IndexFunc(a.Manifest().Settings, func(x platform.Setting) bool { return x.Name == name })
	if i < 0 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if !a.Manifest().Settings[i].Accepts(p.Value) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if err := t.checkEmbeddingSetting(id, p.Value); err != nil {
			return nil, err
		}
		if err := t.checkModelSetting(id, p.Value); err != nil {
			return nil, err
		}
	}
	if !c.Replaying && id == "platform/enterpriseRoot" && p.Value != "" {
		element, ok := c.Enterprise().Element(p.Value, now)
		if !ok || element.Stereotype != "ActualOrganization" || element.Owner != "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose an active local organization for this tenant")
		}
	}
	bindEmbedding := id == platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel && !c.Replaying && c.Staging()
	space := ""
	if bindEmbedding && p.Value != "" {
		space = t.vectorSpace(p.Value)
	}
	return func(*pb.ChangeRecord) {
		t.settings.Set(id, p.Value)
		if bindEmbedding {
			t.settings.Set(embeddingActiveSpace, space)
			t.settings.Set(platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingNext, "")
		}
	}, nil
}

type logPolicyValue struct {
	Level         string `json:"level,omitempty"`
	RetentionDays *int   `json:"retentionDays,omitempty"`
	DebugUntil    string `json:"debugUntil,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

func validateLogPolicy(raw string, now time.Time, fallbackLevel string) *kernel.Error {
	if strings.TrimSpace(raw) == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A log policy needs a JSON object")
	}
	var policy logPolicyValue
	if json.Unmarshal([]byte(raw), &policy) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A log policy needs a JSON object")
	}
	if policy.Level != "" && !slices.Contains([]string{"info", "warn", "error", "debug"}, policy.Level) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose info, warn or error for a permanent level")
	}
	if policy.Level == "debug" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Use a temporary debug window instead of a permanent debug level")
	}
	if policy.RetentionDays != nil && (*policy.RetentionDays < 0 || *policy.RetentionDays > 3650) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Retention must be between 0 and 3650 days")
	}
	if policy.DebugUntil != "" {
		until, err := time.Parse(time.RFC3339, policy.DebugUntil)
		if err != nil || until.Before(now) || until.After(now.Add(30*time.Minute)) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug must end within the next 30 minutes")
		}
		if strings.TrimSpace(policy.Reason) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Temporary debug needs a reason")
		}
	}
	_ = fallbackLevel
	return nil
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
	known := t.work.Knows(id)
	t.opsMu.Unlock()
	if !known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return func(*pb.ChangeRecord) { t.retry(id, now) }, nil
}

func (t *Tenant) decideNotification(c platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	owner, found := t.notices.Owner(id)
	if !found {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if owner != c.ID {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return func(*pb.ChangeRecord) { t.notices.MarkRead(id) }, nil
}

func (t *Tenant) workLoop() automation.WorkLoop {
	return automation.WorkLoop{Board: &t.work, Rotation: &t.rotation, Apps: t.apps, Active: t.ownerActive, Lock: t.opsMu.Lock, Unlock: t.opsMu.Unlock, Attempt: t.attempt, Run: t.run}
}
