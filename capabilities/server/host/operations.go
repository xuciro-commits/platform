package host

import automation "platformserver/services/automate/runtime"

import (
	"context"
	"go.opentelemetry.io/otel/trace"
	"log"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/actions"
	controlowner "platformserver/services/security/control"
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

func (t *Tenant) delivered(d platform.Delivery) { t.AuditLog.Delivered(d) }

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
			log.Printf("tenant %s: owned work panicked: %v", t.ID, failure)
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

func (t *Tenant) replay(now time.Time, replaying bool) automation.Replay {
	return automation.Replay{Board: &t.work, App: t.App, Delivered: t.delivered, Enqueue: func() { t.enqueue(now) },
		Caller: func(app string) platform.Caller { return t.automation(app, replaying) },
		Names:  func(e platform.Event) []string { return t.router().Events(e) },
		Retry:  t.retryOf,
		Start: func(id string) uint32 {
			generation, _, _ := t.Works.Start(id, "host")
			return generation
		},
		Finish:  func(id string, generation uint32, failed bool) { t.Works.Finish(id, generation, failed) },
		SetHops: func(n int) { t.hops = n },
		Failed: func(title, why, id string) {
			t.failedWork(func() platform.Caller { return t.automation(controlowner.PlatformApp, replaying) }, title, why, id, now)
		},
		Observe: func(source, runID, app, resource, status, title, detail string) {
			t.observeOwnerRun(now, source, runID, app, resource, status, title, detail)
		}}
}

func (t *Tenant) failedWork(caller func() platform.Caller, title, why, id string, now time.Time) {
	if t.App(controlowner.PlatformApp) == nil {
		return
	}
	caller().Notify(platform.Notification{Title: title, Body: why + ". Retry it in Settings → Automation once the cause is fixed.",
		Key: "failed:" + id}, now, platform.Recipient{AppRole: controlowner.Admin})
}

func (t *Tenant) notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	book := t.recipientBook()
	members := book.Members(c.App, to, now.UTC().Format(time.DateOnly))
	delivery := book.Reach(members)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := t.Notices.Deliver(c.App, n, now, members, delivery, func(x platform.Notification, to platform.Reach) {
		t.outbound.Add(trace.SpanContext{}, t.planMailNotice(x, to)...)
	})
	t.acted += len(out)
	return out
}

func (t *Tenant) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	return t.recipientBook().Members(c.App, to, now.UTC().Format(time.DateOnly))
}

func (t *Tenant) recipientBook() controlowner.RecipientBook {
	var identity controlowner.IdentityOwner
	if t.identityOwner != nil {
		identity = t.identityOwner
	}
	return controlowner.RecipientBook{Identity: identity, Directory: t.directory}
}

func (t *Tenant) setting(c platform.Caller, name string) string {
	return t.settings.ValueFor(c.App, name, t.apps)
}

func (t *Tenant) Settings() []controlowner.AppSettings {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	return t.settings.AppsOf(t.apps)
}

func (t *Tenant) settingDecision() controlowner.Decider {
	return controlowner.SettingDecision{
		Checks: controlowner.SettingChecks{App: t.App, Logging: t.logging().CheckSetting,
			Values: []func(string, string) *kernel.Error{t.checkEmbeddingSetting, t.modelResolver().CheckSetting}},
		Settings:       &t.settings,
		EmbeddingModel: platform.KnowledgeApp + "/" + platform.SettingKnowledgeEmbeddingModel,
		EmbeddingNext:  platform.KnowledgeApp + "/" + platform.SettingKnowledgeEmbeddingNext,
		ActiveSpace:    embeddingActiveSpace,
		VectorSpace:    t.vectorSpace,
	}.Decide
}

func (t *Tenant) workLoop() automation.WorkLoop {
	return automation.Worker{Board: &t.work, Apps: t.apps, Active: t.OwnerActive, Accept: t.acceptWork,
		Replay: func(now time.Time) automation.Replay { return t.replay(now, true) }}.Loop()
}

func (t *Tenant) validateAcceptedIntents(intents []platform.Effect) error {
	return runtimekernel.CheckIntents(t.ID, intents, func(app string) bool { return t.App(app) != nil }, func(id string) bool {
		return t.outbound.Any(func(e actions.Queued) bool { return e.ID == id })
	})
}

func (t *Tenant) installAcceptedIntents(intents []platform.Effect) {
	t.outbound.Add(t.current(), intents...)
}

func (d *stagedDecision) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	return (controlowner.RecipientBook{Directory: d.directoryView()}).Members(c.App, to, now.UTC().Format(time.DateOnly))
}

func (d *stagedDecision) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	console, _ := d.tenant.App(controlowner.PlatformApp).(platform.IdentityProfile)
	if private, ok := d.States[controlowner.PlatformApp].(platform.IdentityProfile); ok {
		console = private
	}
	return d.Notices.Deliver(c.App, n, now, d.recipients(c, now, to), console, func(x platform.Notification, reach platform.Reach) []platform.Effect {
		router := d.routingTenant()
		router.opsMu.Lock()
		defer router.opsMu.Unlock()
		return router.planMailNotice(x, reach)
	})
}

func (d *stagedDecision) Seen(c platform.Caller, keys ...string) { d.Notices.Seen(c.App, keys) }

func (t *Tenant) applyAcceptedNotices(n *acceptedformat.Notices) {
	delta, effects := t.Notices.Apply(n)
	t.opsMu.Lock()
	t.acted += delta
	t.opsMu.Unlock()
	t.outbound.Add(t.current(), effects...)
}
