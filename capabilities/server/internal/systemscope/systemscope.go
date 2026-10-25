// Package systemscope is the kernel gate of self-hosting: which repository
// paths a system change may never touch. It is a pure function so the same
// rule runs when a change is saved and again before anything is built. Paths
// outside the frozen kernel stay editable; the kernel itself only moves by an
// ordinary version release.
package systemscope

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// Limits bound one system change. They keep a single decision small enough to
// be journaled and reviewed; larger deliveries are split into several changes.
const (
	MaxFileBytes   = 2 << 20  // 2 MiB per file
	MaxChangeBytes = 20 << 20 // 20 MiB per change
	MaxFiles       = 2000
)

// Tenant is the host-level tenant that owns the system itself. It is created
// once per host and cannot be created, renamed or deleted through a tenant.
const Tenant = "system"

// The system app's public names. The host, the app and the updater share them,
// so the wire between them is one declaration.
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

// frozen names the actual owners of the frozen kernel. Entries include files,
// directory prefixes and root-package filename families split out of host.go.
var frozen = []string{
	"contract/",
	"go.work",
	"go.work.sum",
	"capabilities/server/host.go",
	"capabilities/server/server.go",
	"capabilities/server/routes.go",
	"capabilities/server/routes_core.go",
	"capabilities/server/submission_schema.go",
	"capabilities/server/staged_decision.go",
	"capabilities/server/runtime.go",
	"capabilities/server/hostview.go",
	"capabilities/server/runtime_state.go",
	"capabilities/server/runtime_snapshot.go",
	"capabilities/server/decision_definition.go",
	"capabilities/server/accepted_",
	"capabilities/server/committed.go",
	"capabilities/server/replay",
	"capabilities/server/snapshot",
	"capabilities/server/journal_bridge.go",
	"capabilities/server/journal/",
	"capabilities/server/records",
	"capabilities/server/record_",
	"capabilities/server/authorization",
	"capabilities/server/access.go",
	"capabilities/server/console.go",
	"capabilities/server/console_identity.go",
	"capabilities/server/console_member.go",
	"capabilities/server/identity_roles.go",
	"capabilities/server/lifecycle.go",
	"capabilities/server/hostadmin.go",
	"capabilities/server/host_control.go",
	"capabilities/server/tenant_registry.go",
	"capabilities/server/internal/acceptance/",
	"capabilities/server/internal/records/",
	"capabilities/server/internal/host/",
	"capabilities/server/internal/systemscope/",
	"capabilities/server/platform/app.go",
	"capabilities/server/platform/ledger.go",
	"capabilities/server/platform/actions.go",
	"capabilities/server/platform/authorization.go",
	"capabilities/server/platform/record_image.go",
	"capabilities/server/platform/snapshot.go",
	"capabilities/server/platform/authz/",
	"capabilities/server/idp/",
	"capabilities/server/go.mod",
	"capabilities/server/go.sum",
}

// secret names are refused wherever they appear.
var secretSuffixes = []string{".key", ".pem", ".p12", ".pfx", ".env"}

// Sensitive identifies credential files both the edit gate and source capture
// must exclude. Frozen kernel files remain readable build inputs.
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

// Verdict says why a path is frozen, or "" when a system change may write it.
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
	for _, entry := range frozen {
		// Deleting a parent directory also deletes its frozen descendants.
		if match == strings.TrimSuffix(entry, "/") || strings.HasPrefix(match, entry) || strings.HasPrefix(entry, match+"/") {
			return "the kernel is frozen: " + entry + " moves by version release only"
		}
	}
	return ""
}

// Violation is one refused path with the rule it broke.
type Violation struct {
	Path   string
	Reason string
}

func (v Violation) Error() string { return fmt.Sprintf("%s: %s", v.Path, v.Reason) }

// Check refuses a change that writes any frozen path.
func Check(paths []string) error {
	for _, p := range paths {
		if reason := Verdict(p); reason != "" {
			return Violation{Path: p, Reason: reason}
		}
	}
	return nil
}

// ErrTooLarge refuses a change beyond the journal budget.
var ErrTooLarge = errors.New("the change is larger than one system decision may carry")

// CheckBudget refuses a change whose files exceed the limits.
func CheckBudget(sizes map[string]int) error {
	if len(sizes) > MaxFiles {
		return fmt.Errorf("%w: %d files (limit %d)", ErrTooLarge, len(sizes), MaxFiles)
	}
	total := 0
	for _, size := range sizes {
		if size > MaxFileBytes {
			return fmt.Errorf("%w: one file is %d bytes (limit %d)", ErrTooLarge, size, MaxFileBytes)
		}
		total += size
	}
	if total > MaxChangeBytes {
		return fmt.Errorf("%w: %d bytes (limit %d)", ErrTooLarge, total, MaxChangeBytes)
	}
	return nil
}

// Digest is a file's content identity: sha256 over its bytes, hex.
func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Manifest is the canonical digest of a change's file set: sorted
// "<digest>  <path>\n" lines. Two changes with the same bytes and paths have
// the same manifest, whatever order they were recorded in.
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

// Editable source records carry text; binary assets need their original owner.
func CheckContent(p, content string) error {
	if strings.IndexByte(content, 0) >= 0 || !utf8.ValidString(content) {
		return Violation{Path: p, Reason: "system source edits must be UTF-8 text"}
	}
	return nil
}
