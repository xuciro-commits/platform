package host

import automation "platformserver/services/automate/runtime"

import (
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	controlowner "platformserver/services/security/control"
	"slices"
	"strings"
	"time"
)

const (
	workBurst = 100
)

var deliveryRetry = platform.Retry{Initial: 2 * time.Second, Max: time.Minute, Attempts: 5}

func (t *Tenant) retryOf(app string) platform.Retry {
	if a := t.App(app); a != nil && a.Manifest().Retry != nil {
		return *a.Manifest().Retry
	}
	return deliveryRetry
}

func (t *Tenant) enqueue(now time.Time) {
	for len(t.events) > 0 {
		e := t.events[0]
		t.events = t.events[1:]
		s := e.Record.GetSubmission()
		names := append([]string{s.GetSchema().GetName()}, t.router().Events(e.Event)...)
		if e.Plan != nil {
			for _, id := range e.Plan.Stopped {
				t.delivered(platform.Delivery{At: now, App: e.App, Action: s.GetSchema().GetName(), Target: runtimekernel.Target(s), Subscriber: id,
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
			t.outbound.Add(t.current(), e.Plan.Effects...)
			for _, id := range e.Plan.Subscribers {
				if t.App(id) == nil {
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
				t.delivered(platform.Delivery{At: now, App: e.App, Action: s.GetSchema().GetName(), Target: runtimekernel.Target(s), Subscriber: id,
					Outcome: fmt.Sprintf("stopped: more than %d events caused by events", acceptedformat.MaxHops)})
				continue
			}
			t.deliver(id, e, now)
		}
		for _, id := range t.listeners {
			if id != e.App && t.App(id).(platform.Listener).Interested(names, e.Event) {
				t.deliver(id, e, now)
			}
		}
	}
}

func (t *Tenant) deliver(subscriber string, e automation.QueuedEvent, now time.Time) {
	s := e.Record.GetSubmission()
	t.work.Lock()
	defer t.work.Unlock()
	t.work.Queue(subscriber, &automation.Task{ScheduledTask: platform.ScheduledTask{ID: "delivery:" + subscriber + ":" + e.App + "/" + e.Record.GetChangeId(), Kind: "delivery", App: subscriber,
		Title: s.GetSchema().GetName() + " " + runtimekernel.Target(s), State: "queued", Due: now.UTC()}, Event: &e})
}

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
		value.scope.work.Lock()
		rotation, jobs := value.scope.work.Rotation, slices.Clone(value.scope.work.Jobs)
		value.scope.work.Unlock()
		t.work.Lock()
		t.work.Rotation = rotation
		for _, job := range jobs {
			if current := t.work.Job(job.ID); current != nil {
				current.NextRun = job.NextRun
			}
		}
		t.work.Unlock()
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
			attribute.String("platform.app", task.App), attribute.String("platform.target", runtimekernel.Target(task.Event.Record.GetSubmission())))
		defer func() { end(outcome); counted(t, "delivery", task.App, outcome) }()
	}
	var err *kernel.Error
	if x, ok := t.App(task.App).(platform.Listener); ok {
		names := append([]string{task.Event.Record.GetSubmission().GetSchema().GetName()}, t.router().Events(task.Event.Event)...)
		if task.Event.Plan != nil {
			names = task.Event.Plan.Names
		}
		err = x.Listen(t.automation(task.App, replaying), task.Event.Event, names, now)
	} else {
		err = t.App(task.App).(platform.Subscriber).Handle(t.automation(task.App, replaying), task.Event.Event)
	}
	if err != nil {
		outcome = err.Error()
	}
	t.hops = 0
	t.works.Finish(task.ID, generation, outcome != "ok")
	s := task.Event.Record.GetSubmission()
	t.delivered(platform.Delivery{At: now, App: task.Event.App, Action: s.GetSchema().GetName(), Target: runtimekernel.Target(s), Subscriber: task.App,
		Outcome: outcome, Attempt: int(generation)})
	t.work.Lock()
	task.ScheduledTask = task.Settle(int(generation), outcome, t.retryOf(task.App), now)
	if task.State == "done" || task.State == "failed" {
		t.work.Settled(task)
	}
	t.work.Unlock()
	if task.State == "failed" {
		t.failedWork("Delivery to "+task.App+" failed: "+task.Title, outcome, task.ID, now, replaying)
	}
	runStatus := "ok"
	if outcome != "ok" {
		runStatus = "failed"
	}
	t.observeOwnerRun(now, "run", task.Kind+":"+task.App+":"+task.ID, task.App, runtimekernel.Target(s), runStatus,
		"Delivery to "+task.App, outcome)

	t.enqueue(now)
	return outcome
}

func (t *Tenant) run(task *automation.Task, now time.Time, replaying bool) string {
	if !replaying {
		outcome := t.acceptWork(task, now)
		t.work.Lock()
		task.NextRun = now.Add(task.Job.Every)
		t.work.Unlock()
		return outcome
	}
	generation, _, _ := t.works.Start(task.ID, "host")
	outcome := "ok"
	if err := t.App(task.App).(platform.Runner).Run(t.automation(task.App, replaying), task.Job.Name, now); err != nil {
		outcome = err.Error()
	}
	t.works.Finish(task.ID, generation, outcome != "ok")
	t.work.Lock()
	task.ScheduledTask = task.Settle(int(generation), outcome, platform.Retry{}, now)
	t.work.Unlock()
	runStatus := "ok"
	if outcome != "ok" {
		runStatus = "failed"
	}
	t.observeOwnerRun(now, "worker", "job:"+task.App+":"+task.ID, task.App, task.ID, runStatus, "Scheduled job "+task.Title, outcome)

	t.enqueue(now)
	return outcome
}

func (t *Tenant) failedWork(title, why, id string, now time.Time, replaying bool) {
	if t.App(controlowner.PlatformApp) == nil {
		return
	}
	t.automation(controlowner.PlatformApp, replaying).Notify(platform.Notification{Title: title, Body: why + ". Retry it in Settings → Automation once the cause is fixed.",
		Key: "failed:" + id}, now, platform.Recipient{AppRole: controlowner.Admin})
}

func (t *Tenant) Tasks() []automation.Task {
	t.work.Lock()
	defer t.work.Unlock()
	return t.work.All(t.apps)
}

func (t *Tenant) retry(id string, now time.Time) bool {
	t.work.Lock()
	defer t.work.Unlock()
	return t.work.Retry(id, now)
}

func (t *Tenant) Connect(descriptors ...*pb.ConnectorDescriptor) error {
	return t.connectors.Register(descriptors...)
}

func (t *Tenant) refused(member, input string, err *kernel.Error, now time.Time) {
	t.connectors.Refused(member, input, err, now)
}

func (t *Tenant) Connectors(now time.Time) []runtimekernel.ConnectorView {
	return t.connectors.Views(now)
}

func (t *Tenant) notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	members := t.recipients(c, now, to)
	delivery := map[string]platform.Reach{}
	if d, ok := t.App(controlowner.PlatformApp).(platform.IdentityProfile); ok {
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
			if d, ok := t.App(controlowner.PlatformApp).(platform.IdentityDirectory); ok {
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
		t.outbound.Add(trace.SpanContext{}, t.planMailNotice(x, to)...)
		out = append(out, m)
	}
	t.acted += len(out)
	return out
}

func (t *Tenant) setting(c platform.Caller, name string) string {
	app := c.App
	if tenantWide, ok := strings.CutPrefix(name, controlowner.PlatformApp+"/"); ok {
		app, name = controlowner.PlatformApp, tenantWide
	}
	a := t.App(app)
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
	id := s.GetTarget().GetId()
	if !t.connectors.Known(id) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	off := s.GetSchema().GetName() == controlowner.SchemaConnectorOff
	return func(*pb.ChangeRecord) { t.connectors.SetDisabled(id, off) }, nil
}

func (t *Tenant) decideSetting(c platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Value string }
	json.Unmarshal(s.GetPayload(), &p)
	app, name, _ := strings.Cut(id, "/")
	a := t.App(app)
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if app == controlowner.PlatformApp && strings.HasPrefix(name, "logPolicy/") {
		targetApp := strings.TrimPrefix(name, "logPolicy/")
		if t.App(targetApp) == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if !c.Replaying {
			if refusal := controlowner.CheckLogPolicy(p.Value, now); refusal != nil {
				return nil, refusal
			}
		}
		return func(*pb.ChangeRecord) { t.settings.Set(id, p.Value) }, nil
	}
	if !c.Replaying {
		if refusal := t.logging().CheckSetting(name, p.Value, now); refusal != nil {
			return nil, refusal
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
		if err := t.modelResolver().CheckSetting(id, p.Value); err != nil {
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

func (t *Tenant) decideBinding(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	var p struct{ Provider string }
	json.Unmarshal(s.GetPayload(), &p)
	if !slices.ContainsFunc(t.router().Providers(id), func(b actions.Binding) bool { return b.ID() == p.Provider }) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return func(*pb.ChangeRecord) { t.router().Rebind(id, p.Provider) }, nil
}

func (t *Tenant) decideWork(_ platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	t.work.Lock()
	known := t.work.Knows(id)
	t.work.Unlock()
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
	return automation.WorkLoop{Board: &t.work, Apps: t.apps, Active: t.OwnerActive, Attempt: t.attempt, Run: t.run}
}
