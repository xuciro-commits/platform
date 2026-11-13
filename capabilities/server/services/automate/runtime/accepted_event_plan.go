package automation

import (
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
)

type AcceptedEventPlanner struct {
	Routed    func(platform.Event) []string
	Effects   func(e platform.Event, names []string) ([]platform.Effect, *kernel.Error)
	Apps      func() []platform.App
	Listeners []string
	Listener  func(id string) platform.Listener
}

func (p AcceptedEventPlanner) Plan(e platform.Event) (platform.AcceptedEventPlan, *kernel.Error) {
	names := append([]string{e.Record.GetSubmission().GetSchema().GetName()}, p.Routed(e)...)
	effects, refusal := p.Effects(e, names)
	if refusal != nil {
		return platform.AcceptedEventPlan{}, refusal
	}
	plan := platform.AcceptedEventPlan{App: e.App, Changed: slices.Clone(e.Changed), Names: names, Effects: effects}
	for _, a := range p.Apps() {
		if slices.ContainsFunc(a.Manifest().Subscribes, func(x string) bool { return slices.Contains(names, x) }) {
			plan.Subscribers = append(plan.Subscribers, a.Manifest().ID)
		}
	}
	for _, id := range p.Listeners {
		if id != e.App && p.Listener(id).Interested(names, e) && !slices.Contains(plan.Subscribers, id) {
			plan.Subscribers = append(plan.Subscribers, id)
		}
	}
	return plan, nil
}
