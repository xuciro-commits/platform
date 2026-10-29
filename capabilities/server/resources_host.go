package platformserver

import "platformserver/platform"

func (t *Tenant) files() platform.FileStore {
	if t.Files != nil {
		return t.Files
	}
	return &t.memFiles
}
