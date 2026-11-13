package host

import (
	"errors"
	"fmt"
	"maps"
	"net/http"

	"platformserver/platform"
	"platformserver/services/ai/gateway"
)

type ownerRoles struct {
	identity  platform.IdentityDirectory
	directory platform.Directory
	linker    platform.Linker
	ai        aigateway.Models
	tasks     platform.Tasks
	procs     platform.Processes
	agents    platform.AgentRuntime
	knowledge platform.Glossary
	observers []platform.Observer
	listeners []string
}

func ownerRolesOf(owners ownerSet, active func(string) bool) (ownerRoles, error) {
	var r ownerRoles
	for i, a := range owners.apps {
		id := owners.ids[i]
		if !active(id) {
			continue
		}
		if x, ok := a.(platform.Observer); ok {
			r.observers = append(r.observers, x)
		}
		if _, ok := a.(platform.Listener); ok {
			r.listeners = append(r.listeners, id)
		}
		if err := errors.Join(claimRole(&r.identity, a, "identity"), claimRole(&r.directory, a, "directory"),
			claimRole(&r.linker, a, "link"), claimRole(&r.ai, a, "model"), claimRole(&r.tasks, a, "task"),
			claimRole(&r.procs, a, "process"), claimRole(&r.agents, a, "agent"), claimRole(&r.knowledge, a, "glossary")); err != nil {
			return ownerRoles{}, err
		}
	}
	return r, nil
}

type tenantPorts struct {
	Secrets  func(name string) ([]byte, bool)
	Outbound func(req *http.Request, allowPrivate bool) (*http.Response, error)
	AIClient func(req *http.Request) (*http.Response, error)
	Files    platform.FileStore
}

type ownerSet struct {
	apps     []platform.App
	ids      []string
	appIndex map[string]int
	owner    map[string]string
	states   map[string]platform.AcceptedStateApp
	inputs   map[string]platform.AcceptedInputApp
	assets   map[string]platform.AssetLifecycleOwner
}

func newOwnerSet(apps []platform.App) (ownerSet, error) {
	s := ownerSet{ids: make([]string, len(apps)), appIndex: map[string]int{}, owner: map[string]string{}}
	for i, app := range apps {
		id := app.Manifest().ID
		if _, exists := s.appIndex[id]; exists || id == "" {
			return ownerSet{}, fmt.Errorf("owner identity %q is empty or repeated", id)
		}
		s.appIndex[id] = i
		s.ids[i] = id
	}
	s.place(apps)
	return s, nil
}

func (s *ownerSet) place(apps []platform.App) {
	s.apps = apps
	s.states, s.inputs, s.assets = map[string]platform.AcceptedStateApp{}, map[string]platform.AcceptedInputApp{}, map[string]platform.AssetLifecycleOwner{}
	for i, a := range apps {
		id := s.ids[i]
		register(s.states, id, a)
		register(s.inputs, id, a)
		register(s.assets, id, a)
	}
}

func (s ownerSet) with(apps []platform.App) ownerSet {
	out := ownerSet{ids: s.ids, appIndex: s.appIndex, owner: maps.Clone(s.owner)}
	out.place(apps)
	return out
}

func (s ownerSet) App(id string) platform.App {
	i, found := s.appIndex[id]
	if !found {
		return nil
	}
	return s.apps[i]
}

func (s ownerSet) assetOwners(active func(string) bool) []platform.AssetLifecycleOwner {
	var out []platform.AssetLifecycleOwner
	for _, id := range s.ids {
		if owner := s.assets[id]; owner != nil && active(id) {
			out = append(out, owner)
		}
	}
	return out
}

func register[T any](m map[string]T, id string, a platform.App) {
	if x, ok := a.(T); ok {
		m[id] = x
	}
}

func claimRole[T any](slot *T, a platform.App, role string) error {
	x, ok := a.(T)
	if !ok {
		return nil
	}
	if any(*slot) != nil {
		return fmt.Errorf("two %s owners", role)
	}
	*slot = x
	return nil
}
