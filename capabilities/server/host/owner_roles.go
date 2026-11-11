package host

import (
	"errors"
	"fmt"

	"platformserver/platform"
)

type ownerRoles struct {
	identity  platform.IdentityDirectory
	directory platform.Directory
	linker    platform.Linker
	ai        models
	tasks     platform.Tasks
	procs     platform.Processes
	agents    platform.AgentRuntime
	knowledge platform.Glossary
	observers []platform.Observer
	listeners []string
}

func ownerRolesOf(apps []platform.App, active func(string) bool) (ownerRoles, error) {
	var r ownerRoles
	for _, a := range apps {
		id := a.Manifest().ID
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
