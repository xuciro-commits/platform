package actions

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type DispatchJob struct {
	Effect   platform.Effect
	Endpoint Endpoint
	Span     trace.SpanContext
}

func ClaimJobs(outbox *Outbox, endpoints []Endpoint, model string, allow func(id string) bool, now time.Time) []DispatchJob {
	var jobs []DispatchJob
	for _, ep := range append(endpoints, Endpoint{ID: model, Kind: model}) {
		if ep.Kind != model && !allow(ep.ID) {
			continue
		}
		if x, ok := outbox.Claim(ep.ID, now); ok {
			jobs = append(jobs, DispatchJob{x.Effect, ep, x.Span})
		}
	}
	return jobs
}

type DispatchRound struct {
	Model       string
	Stopped     func() bool
	Quarantined func() bool
	Allow       func(id string) bool
	Begin       func(platform.Effect) bool
	Span        func(DispatchJob) func(outcome string)
	Count       func(app, result string)
	Send        func(Endpoint, platform.Effect) (platform.Outcome, *platform.AIUsage)
	Report      func(id string, ok bool)
	Settle      func(id string, o platform.Outcome, usage *platform.AIUsage)
}

func (r DispatchRound) Sends(outbox *Outbox, endpoints []Endpoint, now time.Time) []func() {
	var sends []func()
	for _, j := range ClaimJobs(outbox, endpoints, r.Model, r.Allow, now) {
		sends = append(sends, func() {
			if r.Stopped() || !r.Begin(j.Effect) {
				return
			}
			end := r.Span(j)
			outcome, usage := r.Send(j.Endpoint, j.Effect)
			end(map[bool]string{true: "ok", false: outcome.Result + ": " + outcome.Detail}[outcome.Result == "delivered"])
			if r.Quarantined() {
				return
			}
			r.Count(j.Effect.App, outcome.Result)
			if j.Endpoint.Kind != r.Model {
				r.Report(j.Endpoint.ID, outcome.Result != "retry")
			}
			r.Settle(j.Effect.ID, outcome, usage)
		})
	}
	return sends
}

func RunnableIDs(queued []Queued, now time.Time, limit int) []string {
	ids := []string{}
	active := 0
	for _, x := range queued {
		if x.Sending {
			active++
		}
	}
	for _, x := range queued {
		if !x.Sending && !platform.EffectSettled(x.State) && !x.Due.After(now) && len(ids)+active < limit {
			ids = append(ids, x.ID)
		}
	}
	return ids
}

type AppEffectPlan struct {
	Tenant    string
	App       platform.App
	Endpoints []Endpoint
	Run       string
	Bind      func(platform.AssetRef) ([]platform.AssetBinding, error)
	Exists    func(id string) bool
}

func (p AppEffectPlan) Plan(c platform.Caller, kind, key, entity string, data any, now time.Time, staged []platform.Effect) ([]platform.Effect, *kernel.Error) {
	if p.App == nil || key == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	emits := p.App.Manifest().Emits
	i := slices.IndexFunc(emits, func(e platform.EffectKind) bool { return e.Name == kind })
	if i < 0 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	typ, _, _ := strings.Cut(entity, "/")
	resource := platform.AssetRef{App: c.App, Kind: platform.AssetObject, Name: typ}
	if typ == "" {
		resource = platform.AssetRef{App: c.App, Kind: platform.AssetApp, Name: c.App}
	}
	applications, err := p.Bind(resource)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The effect application cannot be bound")
	}
	agent, run := "", ""
	if c.Agent {
		agent, run = c.ID, p.Run
	}
	body, err := json.Marshal(map[string]any{"type": c.App + "/" + kind, "timestamp": now, "data": data})
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	exists := func(id string) bool {
		return p.Exists(id) || slices.ContainsFunc(staged, func(x platform.Effect) bool { return x.ID == id })
	}
	return AppEffects(p.Endpoints, platform.Effect{Applications: applications, App: c.App, Key: key, Target: entity, At: now, Agent: agent, Run: run, Due: now, Body: string(body)},
		p.Tenant, kind, c.Agent && emits[i].Irreversible, c.Agent, exists), nil
}

func HeldNotices(app platform.App, kind, entity, agent string, planned []platform.Effect, ref string) []platform.Notification {
	emits := app.Manifest().Emits
	title := emits[slices.IndexFunc(emits, func(e platform.EffectKind) bool { return e.Name == kind })].Title
	var out []platform.Notification
	for _, x := range planned {
		if x.State == "held" {
			out = append(out, platform.Notification{Title: fmt.Sprintf("Approve %s for %s", title, entity),
				Body: fmt.Sprintf("The AI agent %s caused it, and it cannot be recalled once sent. Approve or discard it in Settings → Integrations.", agent),
				Ref:  ref + "/" + x.ID, Key: "approve:" + x.ID})
		}
	}
	return out
}
