package host

import "time"

// SystemBuildRequest is one approved system change handed to the builder: the
// frozen file set, already reduced to path and content by the owning app.
type SystemBuildRequest struct {
	Change  string
	Version int
	Files   map[string]string
	Deleted []string
}

// SystemBuildResult is the evidence one build leaves: what it produced, with
// which toolchain, and why it failed when it did.
type SystemBuildResult struct {
	OK          bool
	Work        string
	Binary      string
	Web         string
	Toolchain   string
	Diagnostics string
	// Manifest is the digest of every file the change froze, so a release can
	// be exported and checked against what was built rather than rebuilt.
	Manifest map[string]string
	Duration time.Duration
}

// SystemActivationRequest asks the updater to make a published release live.
type SystemActivationRequest struct {
	Release int
	// Files are the release's workspaces the updater needs to start it again.
	Manifest string
	Previous int
}

// SystemHost is what the system app may ask of the host: build an approved
// change and request activation. It never compiles or restarts itself.
type SystemHost interface {
	BuildSystem(SystemBuildRequest) (SystemBuildResult, error)
	ActivateSystem(SystemActivationRequest) error
	SystemUpdater() string
}
