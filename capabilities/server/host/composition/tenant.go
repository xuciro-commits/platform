package composition

import (
	platformserver "platformserver/host"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	agentowner "platformserver/services/ai/runtime"
	"platformserver/services/ai/sources"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/devops/packages"
	"platformserver/services/functions/authoring"
	"platformserver/services/ontology"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/files"
	"platformserver/services/resources/monitoring"
	"platformserver/services/resources/publication"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	enterprise "platformserver/services/security/organization"
	"platformserver/services/work"
	"platformserver/services/work/activity"
)

func NewTenantWithCapabilities(id string, services platform.RuntimeCapabilities, seats ...platform.Seat) (*platformserver.Tenant, error) {
	services.BindOwner = bindOwner
	services.NewAgentEvaluator = authoring.NewAgentEvaluator
	services.NewTransactionExecutor = actions.NewRuntime
	return platformserver.NewTenantWithCapabilities(id, services, BuiltinApps(id, seats...)...)
}

func BuiltinApps(id string, seats ...platform.Seat) []platform.App {
	apps := []platform.App{
		NewConsole(id, seats...),
		enterprise.New(id, actions.NewAuthority, platform.OrgSeed{Memberships: controlowner.Memberships(seats)}), relations.New(id, actions.NewAuthority), ai.New(id, actions.NewAuthority), work.New(id, actions.NewAuthority), monitoring.New(id, actions.NewAuthority), automation.New(id, actions.NewAuthority), knowledge.New(id, actions.NewAuthority),
		agentowner.New(id, actions.NewAuthority), files.New(id, actions.NewAuthority), build.New(id, actions.NewAuthority, []publication.Hooks{ontology.ActionTypePublication()}),
	}
	return apps
}

func NewConsole(tenant string, seats ...platform.Seat) *controlowner.Console {
	return controlowner.NewConsole(tenant, actions.NewAuthority, func(id string) controlowner.Owners {
		actions := append(platform.AccessActions(), platform.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: security.NewPolicies(), Packages: packages.New(id), Actions: actions}
	}, seats...)
}
