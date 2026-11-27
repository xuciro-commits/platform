package host

import (
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
)

const SchemaVersionHeader = "Platform-Schema-Version"

func (t *Tenant) actionSchemaVersion(app platform.Service, action platform.Action) (platform.SchemaVersion, error) {
	return releaseowner.ActionSchemaVersion(t.definitions, app, action)
}
