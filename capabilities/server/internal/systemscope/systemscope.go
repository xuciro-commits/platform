package systemscope

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

const Tenant = "system"

const (
	App            = "system"
	FileType       = "system.file"
	BlobType       = "system.blob"
	ChangeType     = "system.change"
	ReleaseType    = "system.release"
	ActivationType = "system.activation"

	SchemaFileSave = "system.file.save"
	SchemaPropose  = "system.change.propose"
	SchemaApprove  = "system.change.approve"
	SchemaBuilt    = "system.change.built"
	SchemaActivate = "system.release.activate"
	SchemaRollback = "system.release.rollback"
	SchemaCancel   = "system.release.cancel"
	SchemaFinished = "system.release.finished"

	Builder   = "builder"
	Publisher = "publisher"

	StateDraft      = "draft"
	StateApproved   = "approved"
	StateBuilt      = "built"
	StatePublished  = "published"
	StateFailed     = "failed"
	StateRejected   = "rejected"
	StateRequested  = "requested"
	StateActive     = "active"
	StateSuperseded = "superseded"
	StateRolledBack = "rolled-back"
)

var secretSuffixes = []string{".key", ".pem", ".p12", ".pfx", ".env"}

func Sensitive(p string) bool {
	name := strings.ToLower(path.Base(p))
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return true
	}
	for _, suffix := range secretSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func Verdict(p string) string {
	if p == "" {
		return "an empty path is not a file"
	}
	if path.IsAbs(p) || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return "only repository-relative paths are allowed"
	}
	clean := path.Clean(p)
	if clean != p {
		return "use one canonical repository-relative path without dot segments or repeated separators"
	}
	if clean == "." {
		return "a repository root is not a file"
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "a path may not leave the repository"
	}
	match := strings.ToLower(clean)
	if match == ".git" || strings.HasPrefix(match, ".git/") {
		return "version-control state is not a system asset"
	}
	if Sensitive(clean) {
		return "credentials are never system assets"
	}
	return "dynamic platform source changes are frozen; use the static deployment workflow"
}

func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func Manifest(files map[string]string) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(files[p])
		b.WriteString("  ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
