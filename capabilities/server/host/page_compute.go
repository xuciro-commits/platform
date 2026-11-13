package host

import (
	"platformserver/platform"
	"platformserver/services/apps/pages"
	"platformserver/services/functions/runtime"
)

func (t *Tenant) pageOperation(binding *platform.AssetBinding) (platform.Operation, int, error) {
	return pages.PinnedOperation{App: t.App, Definition: compute.OperationDefinition, Registry: t.assetRegistryLocked}.Resolve(binding)
}
