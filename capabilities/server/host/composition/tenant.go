package composition

import (
	platformserver "platformserver/host"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	agentevaluation "platformserver/services/ai/evaluation"
	agentowner "platformserver/services/ai/runtime"
	"platformserver/services/ai/sources"
	appsauthoring "platformserver/services/apps/authoring"
	automationauthoring "platformserver/services/automate/authoring"
	automation "platformserver/services/automate/runtime"
	devopsauthoring "platformserver/services/devops/authoring"
	"platformserver/services/devops/packages"
	functionauthoring "platformserver/services/functions/authoring"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/files"
	"platformserver/services/resources/monitoring"
	"platformserver/services/resources/publication"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	enterprise "platformserver/services/security/organization"
	"platformserver/services/work"
	"platformserver/services/work/activity"
)

func NewTenantWithCapabilities(id string, services runtimeprotocol.RuntimeCapabilities, seats ...runtimeprotocol.Seat) (*platformserver.Tenant, error) {
	services.BindOwner = runtimeprotocol.BindOwner
	services.NewAgentEvaluator = agentevaluation.NewAgentEvaluator
	services.NewTransactionExecutor = actions.NewRuntime
	return platformserver.NewTenantWithCapabilities(id, services, BuiltinApps(id, platform.OrgSeed{Memberships: controlowner.Memberships(seats)}, seats...)...)
}
func BuiltinApps(id string, org platform.OrgSeed, seats ...runtimeprotocol.Seat) []platform.Service {
	return []platform.Service{
		NewConsole(id, seats...),
		enterprise.New(id, actions.NewAuthority, org), activity.New(id, actions.NewAuthority), ai.New(id, actions.NewAuthority), work.New(id, actions.NewAuthority), monitoring.New(id, actions.NewAuthority), automation.New(id, actions.NewAuthority, platformserver.PublicationResolver()), sources.New(id, actions.NewAuthority),
		agentowner.New(id, actions.NewAuthority), files.New(id, actions.NewAuthority), build.New(id, actions.NewAuthority, []publication.Hooks{ontology.ActionTypePublication()}, platformserver.PublicationResolver(), ontology.ObjectPublication, objects.QueryPublication, functionauthoring.NewOwner, agentowner.BuilderPublication, agentevaluation.NewOwner, devopsauthoring.TestPlanPublication, agentevaluation.EvaluationPublication, devopsauthoring.SampleSetPublication, build.ApplicationOwners{New: appsauthoring.NewOwner, ObjectPage: appsauthoring.CompileObjectPage, ListDetailPage: appsauthoring.ListDetailPage}, automationauthoring.New),
	}
}

func NewConsole(tenant string, seats ...runtimeprotocol.Seat) *controlowner.Console {
	return controlowner.NewConsole(tenant, actions.NewAuthority, func(id string) controlowner.Owners {
		actions := append(runtimeprotocol.AccessActions(), runtimeprotocol.ProjectActions()...)
		actions = append(actions, packages.Actions()...)
		return controlowner.Owners{Policies: security.NewPolicies(), Packages: packages.New(id, platformserver.PublicationResolver()), Actions: actions}
	}, seats...)
}
