package host

import (
	"fmt"
	"maps"
	"net/http"

	"platformserver/hostroles"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"platformserver/services/ai/gateway"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/devops/packages"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

type ownerRoles struct {
	identity      platform.IdentityDirectory
	identityOwner controlowner.IdentityOwner
	directory     platform.Directory
	linker        platform.Linker
	ai            aigateway.Models
	aiTraining    platform.AITrainingOwner
	tasks         platform.Tasks
	procs         automation.FlowProcesses
	packagesOwner packages.PreparationOwner
	agents        platform.AgentRuntime
	knowledge     platform.Glossary
	observers     []platform.Observer
	listeners     []string
}

func ownerRolesOf(owners ownerSet, active func(string) bool) (ownerRoles, error) {
	roles, err := hostroles.Resolve(owners.capabilities, owners.ids, active)
	if err != nil {
		return ownerRoles{}, err
	}
	return ownerRoles{identity: roles.Identity, identityOwner: roles.IdentityOwner, directory: roles.Directory, linker: roles.Linker, ai: roles.Models, aiTraining: roles.Training, tasks: roles.Tasks, procs: roles.Processes, packagesOwner: roles.PackageOwner, agents: roles.Agents, knowledge: roles.Knowledge, observers: roles.Observers, listeners: roles.Listeners}, nil
}

type tenantPorts struct {
	Secrets  func(name string) ([]byte, bool)
	Outbound func(req *http.Request, allowPrivate bool) (*http.Response, error)
	AIClient func(req *http.Request) (*http.Response, error)
	Files    platform.FileStore
}
type ownerSet struct {
	apps          []platform.App
	ids           []string
	appIndex      map[string]int
	owner         map[string]string
	states        map[string]platform.AcceptedStateApp
	inputs        map[string]platform.AcceptedInputApp
	assets        map[string]platform.AssetLifecycleOwner
	answerers     map[string]platform.Answerer
	builders      map[string]build.ConstructionDrafts
	operations    map[string]runtimekernel.OperationOwner
	dynamicAgents map[string]platform.DynamicAgentDefinitions
	simulations   trialowner.SimulationOwners
	capabilities  hostroles.Capabilities
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
	s.capabilities = hostroles.CapabilitiesOf(apps)
	s.states, s.inputs, s.assets, s.answerers = s.capabilities.States, s.capabilities.Inputs, s.capabilities.Assets, s.capabilities.Answerers
	s.builders, s.operations, s.dynamicAgents = s.capabilities.Builders, s.capabilities.Operations, s.capabilities.DynamicAgents
	s.simulations = trialowner.SimulationOwners{Factories: s.capabilities.SimulationFactories, Builder: s.builders[build.ID], Identities: s.capabilities.SimulationIdentities, Organizations: s.capabilities.SimulationOrganizations}
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

func (s ownerSet) dynamicAgentList() []platform.DynamicAgentDefinitions {
	var out []platform.DynamicAgentDefinitions
	for _, id := range s.ids {
		if owner := s.dynamicAgents[id]; owner != nil {
			out = append(out, owner)
		}
	}
	return out
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
