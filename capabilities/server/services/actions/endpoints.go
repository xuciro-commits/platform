package actions

import (
	"net/url"
	"slices"
	"strings"
	"sync"

	"platformserver/platform"
)

type Endpoints struct {
	mu  sync.Mutex
	all []*Endpoint
}

func (e *Endpoints) All() []Endpoint {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Endpoint, 0, len(e.all))
	for _, ep := range e.all {
		out = append(out, *ep)
	}
	return out
}

func (e *Endpoints) Saved() []*Endpoint {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.all)
}

func (e *Endpoints) Restore(all []*Endpoint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.all = slices.Clone(all)
}

func (e *Endpoints) Has(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.ContainsFunc(e.all, func(ep *Endpoint) bool { return ep.ID == id })
}

func (e *Endpoints) Add(ep Endpoint) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.all = append(e.all, &ep)
}

func (e *Endpoints) Remove(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.all = slices.DeleteFunc(e.all, func(ep *Endpoint) bool { return ep.ID == id })
}

func (ep *Endpoint) Valid(app func(string) platform.App, declared func(event string) bool) bool {
	u, err := url.Parse(ep.URL)
	if err != nil || u.Host == "" {
		return false
	}
	switch ep.Kind {
	case "", "webhook":
		if ep.Secret == "" || len(ep.Events)+len(ep.Effects) == 0 || len(ep.Notifications) > 0 || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
			return false
		}
		ep.Kind = "webhook"
	case "a2a":
		if len(ep.Effects) == 0 || len(ep.Events)+len(ep.Notifications) > 0 || u.Scheme != "https" && !(u.Scheme == "http" && ep.AllowPrivate) {
			return false
		}
	case "email":
		if u.Scheme != "smtp" || !strings.Contains(ep.From, "@") || len(ep.Notifications) == 0 || len(ep.Events)+len(ep.Effects) > 0 || u.User != nil && ep.Secret == "" {
			return false
		}
		for _, id := range ep.Notifications {
			if app(id) == nil {
				return false
			}
		}
	default:
		return false
	}
	for _, event := range ep.Events {
		if !declared(event) {
			return false
		}
	}
	for _, kind := range ep.Effects {
		id, name, _ := strings.Cut(kind, "/")
		if a := app(id); a == nil || !slices.ContainsFunc(a.Manifest().Emits, func(e platform.EffectKind) bool { return e.Name == name }) {
			return false
		}
	}
	return true
}
