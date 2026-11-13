package host

import automation "platformserver/services/automate/runtime"

import (
	"context"
	"encoding/json"
	"fmt"
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
		automation.EventRoute{MaxHops: acceptedformat.MaxHops, Apps: t.apps, Listeners: t.listeners, Delivered: t.delivered, Emit: t.emit,
			Names: func(e platform.Event) []string { return t.router().Events(e) },
			Observe: func(e platform.Event, names []string) {
				for _, o := range t.observers {
					o.Observe(e, names)
				}
			},
			Add:    func(effects ...platform.Effect) { t.outbound.Add(t.current(), effects...) },
			Exists: func(id string) bool { return t.App(id) != nil },
			Interested: func(id string, names []string, e platform.Event) bool {
				return t.App(id).(platform.Listener).Interested(names, e)
			},
			Deliver: func(id string, e automation.QueuedEvent) { t.work.Deliver(id, e, now) }}.Route(e, now)
	}
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
		t.work.Adopt(&value.scope.work)
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
	return t.replay(now, replaying).Attempt(task, now)
}

func (t *Tenant) run(task *automation.Task, now time.Time, replaying bool) string {
	if !replaying {
		outcome := t.acceptWork(task, now)
		t.work.Lock()
		task.NextRun = now.Add(task.Job.Every)
		t.work.Unlock()
		return outcome
	}
	return t.replay(now, replaying).Run(task, now)
}

func (t *Tenant) replay(now time.Time, replaying bool) automation.Replay {
	return automation.Replay{Board: &t.work, App: t.App, Delivered: t.delivered, Enqueue: func() { t.enqueue(now) },
		Caller: func(app string) platform.Caller { return t.automation(app, replaying) },
		Names:  func(e platform.Event) []string { return t.router().Events(e) },
		Retry:  t.retryOf,
		Start: func(id string) uint32 {
			generation, _, _ := t.works.Start(id, "host")
			return generation
		},
		Finish:  func(id string, generation uint32, failed bool) { t.works.Finish(id, generation, failed) },
		SetHops: func(n int) { t.hops = n },
		Failed:  func(title, why, id string) { t.failedWork(title, why, id, now, replaying) },
		Observe: func(source, runID, app, resource, status, title, detail string) {
			t.observeOwnerRun(now, source, runID, app, resource, status, title, detail)
		}}
}

func (t *Tenant) failedWork(title, why, id string, now time.Time, replaying bool) {
	if t.App(controlowner.PlatformApp) == nil {
		return
	}
	t.automation(controlowner.PlatformApp, replaying).Notify(platform.Notification{Title: title, Body: why + ". Retry it in Settings → Automation once the cause is fixed.",
		Key: "failed:" + id}, now, platform.Recipient{AppRole: controlowner.Admin})
}

func (t *Tenant) Tasks() []automation.Task { return t.work.Tasks(t.apps) }

func (t *Tenant) retry(id string, now time.Time) bool { return t.work.Retried(id, now) }

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
	return controlowner.Recipients(c.App, to, now.UTC().Format(time.DateOnly),
		func(app, role string) []string {
			if d, ok := t.App(controlowner.PlatformApp).(platform.IdentityDirectory); ok {
				return d.Holding(app, role)
			}
			return nil
		},
		func(structure, unit, role, date string) []string {
			if t.directory == nil {
				return nil
			}
			return t.directory.Holders(structure, unit, role, date)
		})
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
	return t.settings.Value(c.App, name, func(app string) ([]platform.Setting, bool) {
		a := t.App(app)
		if a == nil {
			return nil, false
		}
		return transport.ManifestOf(a, t.definitionsFingerprint()).Settings, true
	})
}

func (t *Tenant) Settings() []controlowner.AppSettings {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	manifests := make([]platform.Manifest, 0, len(t.apps))
	for _, a := range t.apps {
		manifests = append(manifests, a.Manifest())
	}
	return t.settings.Apps(manifests)
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
	id, value, refusal := controlowner.SettingChecks{App: t.App, Logging: t.logging().CheckSetting,
		Values: []func(string, string) *kernel.Error{t.checkEmbeddingSetting, t.modelResolver().CheckSetting}}.Check(c, s, now)
	if refusal != nil {
		return nil, refusal
	}
	bindEmbedding := id == platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel && !c.Replaying && c.Staging()
	space := ""
	if bindEmbedding && value != "" {
		space = t.vectorSpace(value)
	}
	return func(*pb.ChangeRecord) {
		t.settings.Set(id, value)
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
