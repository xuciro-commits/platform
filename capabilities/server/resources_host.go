package platformserver

import "platformserver/platform"

// files is the tenant's file store: the deployment's, or memory.
func (t *Tenant) files() platform.FileStore {
	if t.Files != nil {
		return t.Files
	}
	return &t.memFiles
}
