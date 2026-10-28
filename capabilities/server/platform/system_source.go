package platform

import "time"

// SystemSource is the immutable source of the running system, supplied by
// deployment. Retained files are read-only and never expose arbitrary host paths.
type SystemSource struct {
	Version int
	Digest  string
	Files   []SystemSourceFile
	Read    func(string) (string, error)
}

type SystemSourceFile struct {
	Path     string `json:"path"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
	Mode     uint32 `json:"mode"`
	Editable bool   `json:"editable"`
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
