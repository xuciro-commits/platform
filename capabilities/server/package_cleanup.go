package platformserver

import "platformserver/internal/host"

// PackageRemoval describes retirement without discarding retained data.
type PackageRemoval struct {
	ID           string   `json:"id"`
	OK           bool     `json:"ok"`
	NeedsCleanup bool     `json:"needsCleanup"`
	Blockers     []string `json:"blockers"`
}

// Driver execution is outside this composition. A retained installation with
// external resources must be closed by its original owner before retirement.
func (t *Tenant) packageBlockers(id string, proofs map[string]string) ([]string, bool) {
	console, ok := t.app(PlatformApp).(host.PackageCleanupOwner)
	if !ok {
		return []string{"Package management is unavailable"}, false
	}
	external, err := console.RetainedPackageRuntime(id)
	if err != nil {
		return []string{err.Error()}, false
	}
	if external {
		return []string{"This host cannot verify cleanup of a retained external driver; use its original resource owner"}, true
	}
	return nil, false
}
