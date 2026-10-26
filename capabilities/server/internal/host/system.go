package host

import "time"

// SystemBuildRequest is one approved system change handed to the builder: the
// frozen file set, already reduced to path and content by the owning app.
type SystemBuildRequest struct {
	Change       string
	Base         int
	Source       string
	SourceDigest string
	Files        map[string]string
	Deleted      []string
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
	Manifest     map[string]string
	Source       string
	SourceDigest string
	Duration     time.Duration
}
