package host

import (
	"platformserver/platform"
	"platformserver/services/apps/pages"
)

func (t *Tenant) pageOperation(binding *platform.AssetBinding) (platform.Operation, int, error) {
	return pages.PinnedOperation{App: t.App, Definition: t.operationDefinition, Registry: t.assetRegistryLocked}.Resolve(binding)
}
