package actions

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

type EndpointView struct {
	Endpoint
	Pending  int    `json:"pending"`
	Failing  int    `json:"failing"`
	Health   string `json:"health"`
	Delivers int    `json:"delivered"`
}

func EventEffects(tenant string, endpoints []Endpoint, e platform.Event, names []string) []platform.Effect {
	s := e.Record.GetSubmission()
	target := s.GetTarget().GetType() + "/" + s.GetTarget().GetId()
	var planned []platform.Effect
	for _, ep := range endpoints {
		i := slices.IndexFunc(names, func(n string) bool { return slices.Contains(ep.Events, n) })
		if i < 0 {
			continue
		}
		var payload any = string(s.GetPayload())
		if json.Valid(s.GetPayload()) {
			payload = json.RawMessage(s.GetPayload())
		}
		at := e.Record.GetRecordedTime().AsTime()
		body, _ := json.Marshal(map[string]any{"type": names[i], "timestamp": at, "data": map[string]any{
			"app": e.App, "action": s.GetSchema().GetName(), "schemaVersion": s.GetSchema().GetVersion(), "entity": target, "changeId": e.Record.GetChangeId(),
			"principal": s.GetPrincipalId(), "revision": e.Record.GetRevision(), "payload": payload}})
		planned = append(planned, platform.Effect{ID: fmt.Sprintf("%s:%s:%s:%s", tenant, e.App, e.Record.GetChangeId(), ep.ID),
			Endpoint: ep.ID, Event: names[i], Target: target, At: at, State: "pending", Due: at, Body: string(body)})
	}
	return planned
}

func AppEffects(endpoints []Endpoint, template platform.Effect, tenant, kind string, irreversible, agent bool, exists func(id string) bool) []platform.Effect {
	name := template.App + "/" + kind
	var planned []platform.Effect
	for _, ep := range endpoints {
		id := fmt.Sprintf("%s:%s:%s:%s:%s", tenant, template.App, kind, template.Key, ep.ID)
		if !slices.Contains(ep.Effects, name) || exists(id) {
			continue
		}
		x := template
		x.ID, x.Endpoint, x.Event, x.State = id, ep.ID, name, "pending"
		if irreversible || agent && ep.Irreversible {
			x.State = "held"
		}
		planned = append(planned, x)
	}
	return planned
}

func EndpointHealth(endpoints []Endpoint, effects []Queued, secretMissing func(Endpoint) bool) []EndpointView {
	out := []EndpointView{}
	for _, ep := range endpoints {
		v := EndpointView{Endpoint: ep, Health: "ok"}
		for _, x := range effects {
			switch {
			case x.Endpoint != ep.ID:
			case x.State == "delivered":
				v.Delivers++
			case !platform.EffectSettled(x.State):
				v.Pending++
				if x.State == "retrying" && v.Failing == 0 {
					v.Failing, v.Health = x.Attempts, "failing"
				}
			}
		}
		if secretMissing(ep) {
			v.Health = "secret missing"
		}
		out = append(out, v)
	}
	return out
}
