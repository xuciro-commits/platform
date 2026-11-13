package actions

import (
	"slices"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

const effectsKept = 1000

const bodyKept = 30 * 24 * time.Hour

type Queued struct {
	platform.Effect
	Since   int
	Sending bool
	Span    trace.SpanContext
}

type Outbox struct {
	mu  sync.Mutex
	all []*Queued
}

func (o *Outbox) Add(span trace.SpanContext, effects ...platform.Effect) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, x := range effects {
		o.all = append(o.all, &Queued{Effect: x, Span: span})
	}
	for len(o.all) > effectsKept {
		i := slices.IndexFunc(o.all, func(x *Queued) bool { return platform.EffectSettled(x.State) })
		if i < 0 {
			return
		}
		o.all = slices.Delete(o.all, i, i+1)
	}
}

func (o *Outbox) Get(id string) (Queued, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if i := slices.IndexFunc(o.all, func(x *Queued) bool { return x.ID == id }); i >= 0 {
		return *o.all[i], true
	}
	return Queued{}, false
}

func (o *Outbox) Any(match func(Queued) bool) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.ContainsFunc(o.all, func(x *Queued) bool { return match(*x) })
}

func (o *Outbox) Select(match func(Queued) bool) []Queued {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []Queued
	for _, x := range o.all {
		if match == nil || match(*x) {
			out = append(out, *x)
		}
	}
	return out
}

func (o *Outbox) Update(id string, change func(*Queued)) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	i := slices.IndexFunc(o.all, func(x *Queued) bool { return x.ID == id })
	if i < 0 {
		return false
	}
	change(o.all[i])
	return true
}

func (o *Outbox) UpdateAll(change func(*Queued)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, x := range o.all {
		change(x)
	}
}

func (o *Outbox) Restore(items []Queued) {
	o.mu.Lock()
	defer o.mu.Unlock()
	existing := map[string]*Queued{}
	for _, x := range o.all {
		existing[x.ID] = x
	}
	o.all = make([]*Queued, 0, len(items))
	for _, item := range items {
		if prior := existing[item.ID]; prior != nil {
			item.Sending, item.Span = prior.Sending, prior.Span
		}
		o.all = append(o.all, &item)
	}
}

func (o *Outbox) Claim(endpoint string, now time.Time) (Queued, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	i := slices.IndexFunc(o.all, func(x *Queued) bool {
		return x.Endpoint == endpoint && !platform.EffectSettled(x.State) && x.State != "held"
	})
	if i >= 0 && o.all[i].State == "unknown" {
		blocked := o.all[i].ID
		i = slices.IndexFunc(o.all, func(x *Queued) bool {
			return x.Endpoint == endpoint && x.Compensates == blocked && x.State == "pending"
		})
	}
	if i < 0 || o.all[i].State == "unknown" || o.all[i].Sending || o.all[i].Due.After(now) {
		return Queued{}, false
	}
	o.all[i].Sending = true
	return *o.all[i], true
}

func ConnectionEndpoints(queued []Queued, prefix string) []Endpoint {
	var out []Endpoint
	for _, x := range queued {
		if !strings.HasPrefix(x.Endpoint, prefix) || platform.EffectSettled(x.State) {
			continue
		}
		if !slices.ContainsFunc(out, func(ep Endpoint) bool { return ep.ID == x.Endpoint }) {
			out = append(out, Endpoint{ID: x.Endpoint, Kind: "connection"})
		}
	}
	return out
}

func RedactedEffects(queued []Queued, now time.Time) []platform.Effect {
	out := []platform.Effect{}
	for i := len(queued) - 1; i >= 0; i-- {
		x := queued[i].Effect
		if now.Sub(x.At) > bodyKept && x.State != "unknown" {
			x.Body = ""
		}
		out = append(out, x)
	}
	return out
}

func (o *Outbox) State(id string) (acceptedformat.EffectState, bool) {
	current, found := o.Get(id)
	if !found {
		return acceptedformat.EffectState{}, false
	}
	return acceptedformat.EffectState{Effect: current.Effect, Since: current.Since}, true
}

func (o *Outbox) Settled(id string, after acceptedformat.EffectState) {
	o.Update(id, func(x *Queued) { x.Effect, x.Since, x.Sending = after.Effect, after.Since, false })
}
