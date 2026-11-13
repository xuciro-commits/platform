package host

import (
	"context"
	"platformserver/services/functions/runtime"

	"platformserver/platform"
)

func (h hostView) operationInstall() compute.OperationInstall {
	return compute.OperationInstall{App: h.app, Rules: h.t.services.Rules, RuleQuery: h.RuleQuery, Entity: h.t.Entity,
		Retained: func(module string) bool { return h.t.artifacts().Exists(context.Background(), module) },
		Describe: func(def platform.Definition) (platform.Definition, error) {
			return h.t.describer().DescribeDefinition(def)
		},
		Definitions: func() []platform.Definition { return h.t.definitions },
		Store:       h.t.storeDefinition}
}
func (h hostView) ValidateInstallOperation(c platform.Caller, o platform.Operation) error {
	return h.operationInstall().Validate(c, o)
}
func (h hostView) InstallOperation(c platform.Caller, o platform.Operation, version int) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.operationInstall().Install(c, o, version)
}
