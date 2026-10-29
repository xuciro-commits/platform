package composition

import "platformserver/platform"

func bindOwner(app platform.App, roles platform.OwnerBindings) {
	if owner, ok := app.(platform.AttachedAITraining); ok {
		owner.AttachAITraining(roles.Training)
	}
	if owner, ok := app.(platform.AttachedSystemDefinitions); ok {
		owner.AttachSystemDefinitions(roles.System)
	}
	if owner, ok := app.(platform.AttachedAgent); ok {
		owner.AttachAgent(roles.Agent)
	}
	if owner, ok := app.(platform.ConsoleAttached); ok {
		owner.AttachConsole(roles.Console)
	}
	if owner, ok := app.(platform.AttachedBuild); ok {
		owner.AttachBuild(roles.Build)
	}
	if owner, ok := app.(platform.AttachedFiles); ok {
		owner.AttachFiles(roles.Files)
	}
	if owner, ok := app.(platform.AttachedKnowledge); ok {
		owner.AttachKnowledge(roles.Knowledge)
	}
	if owner, ok := app.(platform.AttachedRelations); ok {
		owner.AttachRelations(roles.Relations)
	}
	if owner, ok := app.(platform.AttachedFlow); ok {
		owner.AttachFlow(roles.Flow)
	}
	if owner, ok := app.(platform.AttachedWork); ok {
		owner.AttachWork(roles.Work)
	}
}
