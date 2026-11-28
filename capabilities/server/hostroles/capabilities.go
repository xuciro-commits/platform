package hostroles

import (
	"fmt"
	"maps"
	"platformserver/kernel/protocol"
	"slices"

	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	aigateway "platformserver/services/ai/gateway"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

type Capabilities struct {
	States                  map[string]platform.AcceptedStateApp
	Inputs                  map[string]platform.AcceptedInputApp
	Assets                  map[string]protocol.AssetLifecycleOwner
	Answerers               map[string]platform.Answerer
	Builders                map[string]build.ConstructionDrafts
	Operations              map[string]runtimekernel.OperationOwner
	SimulationFactories     map[string]protocol.SimulationFactory
	SimulationIdentities    map[string]protocol.SimulationIdentityFactory
	SimulationOrganizations map[string]protocol.SimulationOrganizationFactory
	Observers               map[string]platform.Observer
	Listeners               map[string]platform.Listener
	Identity                map[string]platform.IdentityDirectory
	IdentityOwners          map[string]controlowner.IdentityOwner
	Directories             map[string]platform.Directory
	Linkers                 map[string]platform.Linker
	Models                  map[string]aigateway.Models
	Training                map[string]protocol.AITrainingOwner
	Tasks                   map[string]platform.Tasks
	Processes               map[string]automation.FlowProcesses
	PackageOwners           map[string]packages.PreparationOwner
	PackageCatalogs         map[string]packages.Catalog
	IdentityProfiles        map[string]protocol.IdentityProfile
	InputReceivers          map[string]platform.InputReceiver
	RelationSources         map[string]protocol.LegacyRelationSource
	FunctionAssets          map[string]platform.RetainedFunctionAssets
	Agents                  map[string]protocol.AgentRuntime
	Knowledge               map[string]platform.Glossary
}

func CapabilitiesOf(apps []platform.Service) Capabilities {
	out := Capabilities{
		States: map[string]platform.AcceptedStateApp{}, Inputs: map[string]platform.AcceptedInputApp{},
		Assets: map[string]protocol.AssetLifecycleOwner{}, Answerers: map[string]platform.Answerer{},
		Builders: map[string]build.ConstructionDrafts{}, Operations: map[string]runtimekernel.OperationOwner{},
		SimulationFactories:  map[string]protocol.SimulationFactory{},
		SimulationIdentities: map[string]protocol.SimulationIdentityFactory{}, SimulationOrganizations: map[string]protocol.SimulationOrganizationFactory{},
		Observers: map[string]platform.Observer{}, Listeners: map[string]platform.Listener{}, Identity: map[string]platform.IdentityDirectory{},
		IdentityOwners: map[string]controlowner.IdentityOwner{}, Directories: map[string]platform.Directory{}, Linkers: map[string]platform.Linker{},
		Models: map[string]aigateway.Models{}, Training: map[string]protocol.AITrainingOwner{}, Tasks: map[string]platform.Tasks{},
		Processes: map[string]automation.FlowProcesses{}, PackageOwners: map[string]packages.PreparationOwner{},
		PackageCatalogs: map[string]packages.Catalog{}, IdentityProfiles: map[string]protocol.IdentityProfile{},
		InputReceivers: map[string]platform.InputReceiver{}, RelationSources: map[string]protocol.LegacyRelationSource{},
		FunctionAssets: map[string]platform.RetainedFunctionAssets{},
		Agents:         map[string]protocol.AgentRuntime{}, Knowledge: map[string]platform.Glossary{},
	}
	for _, app := range apps {
		id := app.Manifest().ID
		switch owner := app.(type) {
		case platform.AcceptedStateApp:
			out.States[id] = owner
		}
		switch owner := app.(type) {
		case platform.AcceptedInputApp:
			out.Inputs[id] = owner
		}
		switch owner := app.(type) {
		case protocol.AssetLifecycleOwner:
			out.Assets[id] = owner
		}
		switch owner := app.(type) {
		case platform.Answerer:
			out.Answerers[id] = owner
		}
		switch owner := app.(type) {
		case build.ConstructionDrafts:
			out.Builders[id] = owner
		}
		switch owner := app.(type) {
		case runtimekernel.OperationOwner:
			out.Operations[id] = owner
		}
		switch owner := app.(type) {
		case protocol.SimulationFactory:
			out.SimulationFactories[id] = owner
		}
		switch owner := app.(type) {
		case protocol.SimulationIdentityFactory:
			out.SimulationIdentities[id] = owner
		}
		switch owner := app.(type) {
		case protocol.SimulationOrganizationFactory:
			out.SimulationOrganizations[id] = owner
		}
		switch owner := app.(type) {
		case platform.Observer:
			out.Observers[id] = owner
		}
		switch owner := app.(type) {
		case platform.Listener:
			out.Listeners[id] = owner
		}
		switch owner := app.(type) {
		case platform.IdentityDirectory:
			out.Identity[id] = owner
		}
		switch owner := app.(type) {
		case controlowner.IdentityOwner:
			out.IdentityOwners[id] = owner
		}
		switch owner := app.(type) {
		case platform.Directory:
			out.Directories[id] = owner
		}
		switch owner := app.(type) {
		case platform.Linker:
			out.Linkers[id] = owner
		}
		switch owner := app.(type) {
		case aigateway.Models:
			out.Models[id] = owner
		}
		switch owner := app.(type) {
		case protocol.AITrainingOwner:
			out.Training[id] = owner
		}
		switch owner := app.(type) {
		case platform.Tasks:
			out.Tasks[id] = owner
		}
		switch owner := app.(type) {
		case automation.FlowProcesses:
			out.Processes[id] = owner
		}
		switch owner := app.(type) {
		case packages.PreparationOwner:
			out.PackageOwners[id] = owner
		}
		switch owner := app.(type) {
		case packages.Catalog:
			out.PackageCatalogs[id] = owner
		}
		switch owner := app.(type) {
		case protocol.IdentityProfile:
			out.IdentityProfiles[id] = owner
		}
		switch owner := app.(type) {
		case platform.InputReceiver:
			out.InputReceivers[id] = owner
		}
		switch owner := app.(type) {
		case protocol.LegacyRelationSource:
			out.RelationSources[id] = owner
		}
		switch owner := app.(type) {
		case platform.RetainedFunctionAssets:
			out.FunctionAssets[id] = owner
		}
		switch owner := app.(type) {
		case protocol.AgentRuntime:
			out.Agents[id] = owner
		}
		switch owner := app.(type) {
		case platform.Glossary:
			out.Knowledge[id] = owner
		}
	}
	return out
}

func UniqueOwner[T any](values map[string]T, active func(string) bool, role string) (string, T, error) {
	var found string
	var out T
	for _, id := range slices.Sorted(maps.Keys(values)) {
		if !active(id) {
			continue
		}
		if found != "" {
			var zero T
			return "", zero, fmt.Errorf("two %s owners: %s and %s", role, found, id)
		}
		found, out = id, values[id]
	}
	return found, out, nil
}

func Unique[T any](values map[string]T, active func(string) bool, role string) (T, error) {
	_, value, err := UniqueOwner(values, active, role)
	return value, err
}

type Roles struct {
	Identity        platform.IdentityDirectory
	IdentityOwner   controlowner.IdentityOwner
	Directory       platform.Directory
	DirectoryOwner  string
	Linker          platform.Linker
	Models          aigateway.Models
	Training        protocol.AITrainingOwner
	Tasks           platform.Tasks
	Processes       automation.FlowProcesses
	PackageOwner    packages.PreparationOwner
	PackageCatalog  packages.Catalog
	IdentityProfile protocol.IdentityProfile
	InputReceivers  map[string]platform.InputReceiver
	RelationSource  protocol.LegacyRelationSource
	FunctionAssets  map[string]platform.RetainedFunctionAssets
	Agents          protocol.AgentRuntime
	Glossary        platform.Glossary
	Observers       []platform.Observer
	Listeners       []string
}

func Resolve(c Capabilities, ids []string, active func(string) bool) (Roles, error) {
	var roles Roles
	for _, id := range ids {
		if !active(id) {
			continue
		}
		if observer := c.Observers[id]; observer != nil {
			roles.Observers = append(roles.Observers, observer)
		}
		if c.Listeners[id] != nil {
			roles.Listeners = append(roles.Listeners, id)
		}
	}
	var err error
	if roles.Identity, err = Unique(c.Identity, active, "identity"); err != nil {
		return roles, err
	}
	if roles.IdentityOwner, err = Unique(c.IdentityOwners, active, "identity owner"); err != nil {
		return roles, err
	}
	if roles.DirectoryOwner, roles.Directory, err = UniqueOwner(c.Directories, active, "directory"); err != nil {
		return roles, err
	}
	if roles.Linker, err = Unique(c.Linkers, active, "link"); err != nil {
		return roles, err
	}
	if roles.Models, err = Unique(c.Models, active, "model"); err != nil {
		return roles, err
	}
	if roles.Training, err = Unique(c.Training, active, "AI training"); err != nil {
		return roles, err
	}
	if roles.Tasks, err = Unique(c.Tasks, active, "task"); err != nil {
		return roles, err
	}
	if roles.Processes, err = Unique(c.Processes, active, "process"); err != nil {
		return roles, err
	}
	if roles.PackageOwner, err = Unique(c.PackageOwners, active, "package preparation"); err != nil {
		return roles, err
	}
	if roles.PackageCatalog, err = Unique(c.PackageCatalogs, active, "package catalog"); err != nil {
		return roles, err
	}
	if roles.IdentityProfile, err = Unique(c.IdentityProfiles, active, "identity profile"); err != nil {
		return roles, err
	}
	if roles.RelationSource, err = Unique(c.RelationSources, active, "relation source"); err != nil {
		return roles, err
	}
	roles.InputReceivers = c.InputReceivers
	roles.FunctionAssets = c.FunctionAssets
	if roles.Agents, err = Unique(c.Agents, active, "agent"); err != nil {
		return roles, err
	}
	roles.Glossary, err = Unique(c.Knowledge, active, "glossary")
	return roles, err
}

type PrivateRoles struct {
	IdentityProfiles map[string]protocol.IdentityProfile
	InputReceivers   map[string]platform.InputReceiver
	Directories      map[string]platform.Directory
}

func (p *PrivateRoles) Add(app platform.Service) {
	id := app.Manifest().ID
	switch owner := app.(type) {
	case protocol.IdentityProfile:
		if p.IdentityProfiles == nil {
			p.IdentityProfiles = map[string]protocol.IdentityProfile{}
		}
		p.IdentityProfiles[id] = owner
	}
	switch owner := app.(type) {
	case platform.InputReceiver:
		if p.InputReceivers == nil {
			p.InputReceivers = map[string]platform.InputReceiver{}
		}
		p.InputReceivers[id] = owner
	}
	switch owner := app.(type) {
	case platform.Directory:
		if p.Directories == nil {
			p.Directories = map[string]platform.Directory{}
		}
		p.Directories[id] = owner
	}
}
