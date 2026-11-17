package packages

import (
	"maps"
	"slices"
	"strings"

	runtimeprotocol "platformserver/kernel/protocol"
)

type HostComposition struct {
	Current               map[string]string                  `json:"current"`
	Desired               map[string]string                  `json:"desired"`
	Available             []runtimeprotocol.CapabilityModule `json:"available"`
	State                 string                             `json:"state"`
	Reason                string                             `json:"reason,omitempty"`
	Owners                []runtimeprotocol.CapabilityModule `json:"owners"`
	DisabledOwners        []string                           `json:"disabledOwners"`
	DesiredDisabledOwners []string                           `json:"desiredDisabledOwners"`
	Storage               runtimeprotocol.CapabilityModule   `json:"storage"`
}

type CompositionState struct {
	Current, Desired                      map[string]string
	Available, Base                       []runtimeprotocol.CapabilityModule
	Storage                               runtimeprotocol.CapabilityModule
	DisabledOwners, DesiredDisabledOwners []string
	Suspended                             bool
	PlatformApp                           string
}

func MissingRuntime(selection map[string]string) bool {
	for _, provider := range selection {
		if provider == "" {
			return true
		}
	}
	return false
}

func (s CompositionState) View() HostComposition {
	view := HostComposition{Current: s.Current, Desired: s.Desired, Available: s.Available, State: "ready", Storage: s.Storage,
		DisabledOwners: s.DisabledOwners, DesiredDisabledOwners: s.DesiredDisabledOwners, Owners: []runtimeprotocol.CapabilityModule{}}
	for _, module := range s.Base {
		if strings.HasPrefix(module.ID, "platform.owner.") {
			view.Owners = append(view.Owners, module)
		}
	}
	if MissingRuntime(view.Desired) || slices.Contains(view.DesiredDisabledOwners, s.PlatformApp) {
		view.State, view.Reason = "maintenance", "A required implementation is removed; business access remains stopped"
	} else if !maps.Equal(view.Current, view.Desired) || !slices.Equal(view.DisabledOwners, view.DesiredDisabledOwners) {
		view.State, view.Reason = "restart-required", "The selection is saved; restart this host to activate the checked composition"
	} else if s.Suspended {
		view.State = "suspended"
	}
	return view
}
