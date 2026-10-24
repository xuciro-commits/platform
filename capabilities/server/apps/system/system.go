// Package system is the platform's self-hosting app: the host-level `system`
// tenant keeps its own source files, changes, releases and activations as
// ordinary records, so a person or an AI agent iterates the system through the
// same submit, accept, install path as any other application.
//
// The kernel is frozen. `internal/systemscope` refuses every path that carries
// the kernel, the submission pipeline, accepted results, the journal, records
// or identity; the app runs that gate when a change is proposed, so a prompt
// cannot ask for what a person cannot write either.
package system

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/systemscope"
	"platformserver/platform"
)

const (
	ID             = systemscope.App
	FileType       = systemscope.FileType
	BlobType       = systemscope.BlobType
	ChangeType     = systemscope.ChangeType
	ReleaseType    = systemscope.ReleaseType
	ActivationType = systemscope.ActivationType

	SchemaFileSave   = systemscope.SchemaFileSave
	SchemaPropose    = systemscope.SchemaPropose
	SchemaApprove    = systemscope.SchemaApprove
	SchemaBuilt      = systemscope.SchemaBuilt
	SchemaActivate   = systemscope.SchemaActivate
	SchemaDeactivate = systemscope.SchemaRollback
	SchemaCancel     = systemscope.SchemaCancel
	SchemaFinished   = systemscope.SchemaFinished

	Builder   = systemscope.Builder
	Publisher = systemscope.Publisher

	StateDraft      = systemscope.StateDraft
	StateApproved   = systemscope.StateApproved
	StatePublished  = systemscope.StatePublished
	StateFailed     = systemscope.StateFailed
	StateRequested  = systemscope.StateRequested
	StateActive     = systemscope.StateActive
	StateRolledBack = systemscope.StateRolledBack
	StateSuperseded = systemscope.StateSuperseded
)

// File is one system source file. Its bytes live in a Blob keyed by their
// sha256, so two paths with the same content store it once; the file keeps its
// own revision and tombstone. A write names the revision it expects, so two
// writers racing on one file cannot silently overwrite each other.
type File struct {
	platform.Record
	Path     string `json:"path" field:"required,search" source:"user-input" help:"Repository-relative path"`
	Digest   string `json:"digest" field:"readonly,search"`
	Size     int    `json:"size" field:"readonly"`
	Revision int    `json:"revision" field:"readonly"`
	Deleted  bool   `json:"deleted,omitempty" field:"readonly"`
	By       string `json:"by,omitempty" field:"readonly" source:"user-input"`
}

// Blob is one content-addressed byte string. It is written once per digest and
// never rewritten: a file revision points at it, a tombstone does not free it.
type Blob struct {
	platform.Record
	Digest  string `json:"digest"`
	Content string `json:"content" type:"longtext"`
	Size    int    `json:"size"`
}

// FileView is a file with its bytes resolved from the blob it names, for the
// editor and the builder. It is a read shape, not a record.
type FileView struct {
	Path     string `json:"path"`
	Digest   string `json:"digest"`
	Content  string `json:"content"`
	Size     int    `json:"size"`
	Revision int    `json:"revision"`
	Deleted  bool   `json:"deleted,omitempty"`
	By       string `json:"by,omitempty"`
}

// ChangeFile is the frozen content of one path in a change.
type ChangeFile struct {
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	Digest  string `json:"digest"`
	Deleted bool   `json:"deleted,omitempty"`
}

// Change is one proposed iteration of the system: where it came from (a person
// or a prompt), the base release, the frozen file set, who approved it, and
// what the builder reported.
type Change struct {
	platform.Record
	Title       string       `json:"title" field:"required,search" source:"user-input"`
	Rationale   string       `json:"rationale" field:"required" type:"longtext" source:"user-input"`
	Origin      string       `json:"origin" field:"readonly" choices:"ui,prompt"`
	Prompt      string       `json:"prompt,omitempty" field:"readonly" type:"longtext" source:"user-input"`
	Agent       string       `json:"agent,omitempty" field:"readonly"`
	Base        int          `json:"base,omitempty" field:"readonly"`
	Files       []ChangeFile `json:"files" field:"readonly" type:"json" title:"Frozen file set"`
	Manifest    string       `json:"manifest,omitempty" field:"readonly" source:"external-input"`
	State       string       `json:"state" field:"readonly" choices:"draft,approved,built,published,failed,rejected"`
	ApprovedBy  string       `json:"approvedBy,omitempty" field:"readonly" source:"user-input"`
	ApprovedAt  time.Time    `json:"approvedAt,omitzero" field:"readonly"`
	Diagnostics string       `json:"diagnostics,omitempty" field:"readonly" type:"longtext" source:"external-input"`
	Release     int          `json:"release,omitempty" field:"readonly"`
}

// Release is one immutable system version: what was built, with which
// toolchain, and which release it replaces.
type Release struct {
	platform.Record
	Version        int    `json:"version" field:"required,search"`
	Change         string `json:"change" field:"required" source:"external-input"`
	Manifest       string `json:"manifest" field:"required" type:"longtext" source:"external-input"`
	ManifestDigest string `json:"manifestDigest" field:"readonly" source:"external-input"`
	Binary         string `json:"binary,omitempty" field:"readonly" source:"external-input"`
	Web            string `json:"web,omitempty" field:"readonly" source:"external-input"`
	Toolchain      string `json:"toolchain,omitempty" field:"readonly" source:"external-input"`
	Previous       int    `json:"previous,omitempty" field:"readonly"`
	State          string `json:"state" field:"readonly" choices:"published,requested,active,superseded,failed"`
	Diagnostics    string `json:"diagnostics,omitempty" field:"readonly" type:"longtext" source:"external-input"`
}

// Activation is the evidence of making a release live, or of rolling back.
type Activation struct {
	platform.Record
	Release  int       `json:"release" field:"required"`
	From     int       `json:"from,omitempty" field:"readonly"`
	Result   string    `json:"result" field:"required" choices:"requested,active,rolled-back,aborted"`
	Window   int       `json:"window,omitempty" field:"readonly" help:"Seconds before the swap during which a person may cancel"`
	Evidence string    `json:"evidence,omitempty" type:"longtext" source:"user-input"`
	Failure  string    `json:"failure,omitempty" type:"longtext" source:"external-input"`
	By       string    `json:"by,omitempty" field:"readonly" source:"user-input"`
	At       time.Time `json:"at,omitzero" field:"readonly"`
}

// System is the tenant's self-hosting app.
type System struct {
	mu     sync.Mutex
	ledger *platform.Ledger
	// Roles is what a deployment may grant: a builder edits and proposes, a
	// publisher approves, builds and activates. No role here approves its own
	// proposal; that rule is checked per decision.
}

// New is a tenant's system app. Its records are meaningful in the system
// tenant; other tenants refuse writes and read empty collections.
func New(tenant string) *System {
	any := []string{Builder, Publisher}
	publish := []string{Publisher}
	actions := []platform.Action{
		{Schema: SchemaFileSave, Target: FileType, Title: "Save system file", Capability: "system",
			Description: "Write one system source file at its expected revision; the kernel gate refuses frozen paths.",
			Roles:       any, Payload: []platform.Field{{Name: "path", Type: "string", Required: true, Description: "Repository-relative path"}, {Name: "content", Type: "string", Description: "The file's new content"}, {Name: "revision", Type: "integer", Description: "The revision the writer expects; 0 creates a new path"}, {Name: "deleted", Type: "boolean", Description: "Append a tombstone instead of writing bytes"}}},
		{Schema: SchemaPropose, Target: ChangeType, Title: "Propose system change", Capability: "system",
			Description: "Freeze a title, a rationale, a prompt when one produced it, and the files this iteration touches.",
			Roles:       any, Payload: []platform.Field{{Name: "title", Type: "string", Required: true}, {Name: "rationale", Type: "string", Required: true}, {Name: "origin", Type: "string", Description: "ui (default) or prompt"}, {Name: "prompt", Type: "string"}, {Name: "base", Type: "integer"}, {Name: "files", Type: "json", Required: true, Description: "[{path, content}] or [{path, deleted:true}]"}}},
		{Schema: SchemaApprove, Target: ChangeType, Title: "Approve system change", Capability: "system",
			Description: "A publisher approves a proposed change; its file set freezes and the builder may run.",
			Roles:       publish, Payload: []platform.Field{}},
		{Schema: SchemaBuilt, Target: ChangeType, Title: "Record build", Capability: "system",
			Description: "The updater reports what the approved change built: a release, or why it did not build.",
			Roles:       publish, Payload: []platform.Field{{Name: "ok", Type: "boolean", Required: true}, {Name: "binary", Type: "string"}, {Name: "web", Type: "string"}, {Name: "toolchain", Type: "string"}, {Name: "diagnostics", Type: "string"}}},
		{Schema: SchemaActivate, Target: ReleaseType, Title: "Activate release", Capability: "system",
			Description: "Ask the updater to make a published release live; a person may cancel inside the window.",
			Roles:       publish, Payload: []platform.Field{{Name: "window", Type: "integer", Description: "Seconds before the swap; default 30"}}},
		{Schema: SchemaDeactivate, Target: ReleaseType, Title: "Roll back", Capability: "system",
			Description: "Return to the release this one replaced.",
			Roles:       publish, Payload: []platform.Field{}},
		{Schema: SchemaCancel, Target: ReleaseType, Title: "Cancel activation", Capability: "system",
			Description: "Cancel a requested activation while the window is still open; the running release stays.",
			Roles:       publish, Payload: []platform.Field{{Name: "reason", Type: "string"}}},
		{Schema: SchemaFinished, Target: ActivationType, Title: "Record activation", Capability: "system",
			Description: "The updater reports the swap outcome: active, rolled-back or aborted.",
			Roles:       publish, Payload: []platform.Field{{Name: "result", Type: "string", Required: true, Choices: []string{StateActive, StateRolledBack, "aborted"}}, {Name: "evidence", Type: "string"}}},
	}
	actions = append(actions, platform.EntityActions(entity(FileType, "File", File{}, "path"))...)
	actions = append(actions, platform.EntityActions(entity(ChangeType, "System change", Change{}, "title"))...)
	actions = append(actions, platform.EntityActions(entity(ReleaseType, "System release", Release{}, "version"))...)
	actions = append(actions, platform.EntityActions(entity(ActivationType, "Activation", Activation{}, "result"))...)
	return &System{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), BlobType, FileType, ChangeType, ReleaseType, ActivationType)}
}

func entity(typ, title string, model any, display string) platform.Entity {
	return platform.Entity{Type: typ, Title: title, Model: model, Display: display, Description: "Self-hosting: the system's own " + strings.ToLower(title) + "."}
}

func (s *System) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Platform", Version: "1", Actions: s.ledger.Catalog,
		Entities: []platform.Entity{
			// Blobs hold file bytes keyed by digest; they have no actions of
			// their own and are written only as part of a file save.
			entity(BlobType, "Blob", Blob{}, "digest"),
			entity(FileType, "File", File{}, "path"), entity(ChangeType, "System change", Change{}, "title"),
			entity(ReleaseType, "System release", Release{}, "version"), entity(ActivationType, "Activation", Activation{}, "result"),
		},
		Reads: []string{"system-files", "system-changes", "system-releases", "system-activations"},
		ReadSources: map[string]string{"system-files": "record", "system-changes": "record",
			"system-releases": "record", "system-activations": "record"},
		Roles: []string{Builder, Publisher},
		Agents: []platform.Agent{{
			Name:  "architect",
			Title: "System architect",
			Description: "Reads the system's own source and proposes one reviewed change at a time. " +
				"It cannot approve, build or activate what it proposes.",
			Instructions: "You iterate the platform itself. Read the files you need, then write only the " +
				"files the request really requires and propose one change with a title, a rationale and " +
				"the reason each file changes. Never propose kernel paths (contract/, the submission " +
				"pipeline, accepted results, the journal, records, authorization or identity): the kernel " +
				"moves by version release only. One change, one purpose. A person approves and builds it.",
			Tools:  []string{SchemaFileSave, SchemaPropose},
			Budget: platform.Budget{Steps: 20, Tokens: 60000, Actions: 8, Cost: 2},
		}},
		Settings: []platform.Setting{{Name: "build-timeout", Title: "Build minutes", Type: "integer", Default: "10",
			Description: "The updater stops a build that runs longer."}},
	}
}

func (s *System) Declarations() []*pb.AuthorityDeclaration { return s.ledger.Declarations() }
func (s *System) AcceptedLedger() *platform.Ledger         { return s.ledger }
func (s *System) SnapshotFormat() string                   { return "platform.ledger.v1" }
func (s *System) Snapshot() (json.RawMessage, error)       { return s.ledger.Snapshot() }
func (s *System) Restore(raw json.RawMessage) error        { return s.ledger.Restore(raw) }

// Read answers the system's own collections, newest first. They are empty in a
// tenant that is not the system.
func (s *System) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if c.Tenant != systemscope.Tenant {
		return []any{}, nil
	}
	switch name {
	case "system-files":
		files, _, err := platform.Find[File](c, platform.Query{Sort: []string{"path"}})
		if err != nil {
			return nil, err
		}
		out := make([]FileView, 0, len(files))
		for _, file := range files {
			view := FileView{Path: file.Path, Digest: file.Digest, Size: file.Size, Revision: file.Revision, Deleted: file.Deleted, By: file.By}
			if !file.Deleted {
				if blob, ok := platform.Get[Blob](c, blobID(file.Digest)); ok {
					view.Content = blob.Content
				}
			}
			out = append(out, view)
		}
		return out, nil
	case "system-changes":
		out, _, err := platform.Find[Change](c, platform.Query{Sort: []string{"-id"}})
		return out, err
	case "system-releases":
		out, _, err := platform.Find[Release](c, platform.Query{Sort: []string{"-version"}})
		return out, err
	default:
		out, _, err := platform.Find[Activation](c, platform.Query{Sort: []string{"-id"}})
		return out, err
	}
}

func (s *System) Submit(c platform.Caller, submission *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ledger.Receive(c, submission, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		if c.Tenant != systemscope.Tenant {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the system tenant may change the system")
		}
		switch submission.GetSchema().GetName() {
		case SchemaFileSave:
			return s.save(c, submission, now)
		case SchemaPropose:
			return s.propose(c, submission, now)
		case SchemaApprove:
			return s.approve(c, submission, now)
		case SchemaBuilt:
			return s.built(c, submission, now)
		case SchemaActivate:
			return s.activate(c, submission, now)
		case SchemaDeactivate:
			return s.rollback(c, submission, now)
		case SchemaCancel:
			return s.cancel(c, submission, now)
		case SchemaFinished:
			return s.finished(c, submission, now)
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	})
}

// ForkDecision gives a private view the same ledger, as other platform apps do.
func (s *System) ForkDecision() (platform.App, error) {
	return &System{ledger: s.ledger}, nil
}

var _ platform.DecisionForkApp = (*System)(nil)

func (s *System) save(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct {
		Path     string
		Content  string
		Revision int
		Deleted  bool
	}
	if json.Unmarshal(submission.GetPayload(), &p) != nil || strings.TrimSpace(p.Path) == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A file needs a path")
	}
	if err := systemscope.Check([]string{p.Path}); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The kernel gate refused {path}: {reason}", p.Path, err.Error())
	}
	id := fileID(p.Path)
	existing, known := platform.Get[File](c, id)
	current := 0
	if known {
		current = existing.Revision
	}
	if p.Revision != current {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT,
			"{path} is at revision {current}; reload it before saving", p.Path, current)
	}
	file := File{Path: p.Path, Revision: current + 1, Deleted: p.Deleted, By: c.ID}
	if known {
		file.Record = existing.Record
	} else {
		file.Record = platform.Record{ID: id}
	}
	puts := []any{}
	if !p.Deleted {
		if err := systemscope.CheckBudget(map[string]int{p.Path: len(p.Content)}); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		digest := systemscope.Digest([]byte(p.Content))
		file.Digest, file.Size = digest, len(p.Content)
		if _, exists := platform.Get[Blob](c, blobID(digest)); !exists {
			puts = append(puts, Blob{Record: platform.Record{ID: blobID(digest)}, Digest: digest, Content: p.Content, Size: len(p.Content)})
		}
	}
	puts = append(puts, file) // the blob is written first so a failed put never leaves a file without bytes
	return func(r *pb.ChangeRecord) {
		for _, v := range puts {
			c.Put(r, v)
		}
	}, nil
}

func (s *System) propose(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct {
		Title, Rationale, Origin, Prompt, Agent string
		Base                                    int
		Files                                   []ChangeFile
	}
	if json.Unmarshal(submission.GetPayload(), &p) != nil || strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.Rationale) == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A change needs a title and a rationale")
	}
	if p.Origin == "" {
		p.Origin = "ui"
	}
	if p.Origin != "ui" && p.Origin != "prompt" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A change comes from the interface or a prompt")
	}
	if len(p.Files) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A change needs at least one file")
	}
	sizes := make(map[string]int, len(p.Files))
	byPath := make(map[string]string, len(p.Files))
	paths := make([]string, 0, len(p.Files))
	frozen := make([]ChangeFile, 0, len(p.Files))
	for _, f := range p.Files {
		if strings.TrimSpace(f.Path) == "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Every file needs a path")
		}
		paths = append(paths, f.Path)
		if f.Deleted {
			f.Content, f.Digest = "", ""
			frozen = append(frozen, f)
			continue
		}
		if f.Content == "" {
			// A change may name a saved file instead of carrying its bytes.
			if saved, ok := platform.Get[File](c, fileID(f.Path)); ok && !saved.Deleted {
				if blob, stored := platform.Get[Blob](c, blobID(saved.Digest)); stored {
					f.Content = blob.Content
				}
			}
		}
		f.Digest = systemscope.Digest([]byte(f.Content))
		sizes[f.Path] = len(f.Content)
		byPath[f.Path] = f.Digest
		frozen = append(frozen, f)
	}
	if err := systemscope.Check(paths); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The kernel gate refused {error}", err.Error())
	}
	if err := systemscope.CheckBudget(sizes); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	change := Change{Record: platform.Record{ID: submission.GetTarget().GetId()}, Title: p.Title, Rationale: p.Rationale,
		Origin: p.Origin, Prompt: p.Prompt, Agent: p.Agent, Base: p.Base, Files: frozen, State: StateDraft,
		Manifest: systemscope.Manifest(byPath)}
	return func(r *pb.ChangeRecord) { c.Put(r, change) }, nil
}

func (s *System) approve(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	change, known := platform.Get[Change](c, submission.GetTarget().GetId())
	if !known || change.State != StateDraft {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a draft change can be approved")
	}
	if change.Agent != "" && change.Agent == c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A proposal is not approved by the agent that proposed it")
	}
	change.State, change.ApprovedBy, change.ApprovedAt = StateApproved, c.ID, now
	return func(r *pb.ChangeRecord) { c.Put(r, change) }, nil
}

func (s *System) built(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct {
		OK                                  bool
		Binary, Web, Toolchain, Diagnostics string
	}
	if json.Unmarshal(submission.GetPayload(), &p) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The build report is unreadable")
	}
	change, known := platform.Get[Change](c, submission.GetTarget().GetId())
	if !known || change.State != StateApproved {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only an approved change is built")
	}
	if !p.OK {
		change.State, change.Diagnostics = StateFailed, p.Diagnostics
		return func(r *pb.ChangeRecord) { c.Put(r, change) }, nil
	}
	releases, _, _ := platform.Find[Release](c, platform.Query{Sort: []string{"-version"}, Limit: 1})
	previous, version := 0, 1
	if len(releases) > 0 {
		previous, version = releases[0].Version, releases[0].Version+1
	}
	release := Release{Record: platform.Record{ID: fmt.Sprintf("%04d", version)}, Version: version, Change: change.ID,
		Manifest: change.Manifest, ManifestDigest: systemscope.Manifest(map[string]string{"change": change.Manifest}),
		Binary: p.Binary, Web: p.Web, Toolchain: p.Toolchain, Previous: previous, State: StatePublished,
		Diagnostics: p.Diagnostics}
	change.State, change.Release = StatePublished, version
	return func(r *pb.ChangeRecord) { c.Put(r, change); c.Put(r, release) }, nil
}

func (s *System) activate(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct{ Window int }
	if body := submission.GetPayload(); len(body) > 0 && json.Unmarshal(body, &p) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The activation request is unreadable")
	}
	if p.Window == 0 {
		p.Window = 30
	}
	if p.Window < 0 || p.Window > 600 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The window is between 0 and 600 seconds")
	}
	// A request whose window has passed no longer blocks a decision: the
	// updater never performed it, and the person who asked is free to try
	// again or to activate something else.
	if pending, _, _ := platform.Find[Activation](c, platform.Query{Archived: true}); slices.ContainsFunc(pending, func(a Activation) bool {
		if a.Result != StateRequested {
			return false
		}
		window := time.Duration(a.Window) * time.Second
		if window == 0 {
			window = 30 * time.Second
		}
		return now.Sub(a.At) <= window
	}) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Another activation is still requested; cancel it or let the updater finish")
	}
	release, known := platform.Get[Release](c, submission.GetTarget().GetId())
	if !known || release.State != StatePublished {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a published release is activated")
	}
	activation := Activation{Record: platform.Record{ID: fmt.Sprintf("%04d", release.Version)}, Release: release.Version,
		From: release.Previous, Result: StateRequested, Window: p.Window, By: c.ID, At: now}
	release.State = StateRequested
	return func(r *pb.ChangeRecord) { c.Put(r, release); c.Put(r, activation) }, nil
}

// cancel closes a requested activation while its window is open. Nothing is
// swapped; the running release keeps serving.
func (s *System) cancel(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct{ Reason string }
	_ = json.Unmarshal(submission.GetPayload(), &p)
	release, known := platform.Get[Release](c, submission.GetTarget().GetId())
	if !known || release.State != StateRequested {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a requested activation can be cancelled")
	}
	pending, known := platform.Get[Activation](c, fmt.Sprintf("%04d", release.Version))
	if !known || pending.Result != StateRequested {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The release has no open activation")
	}
	pending.Result, pending.Failure = "aborted", p.Reason
	if p.Reason == "" {
		pending.Failure = "cancelled by " + c.ID
	}
	release.State = StatePublished
	return func(r *pb.ChangeRecord) { c.Put(r, release); c.Put(r, pending) }, nil
}

func (s *System) rollback(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	release, known := platform.Get[Release](c, submission.GetTarget().GetId())
	if !known || release.State != StateActive || release.Previous == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only an active release with a predecessor rolls back")
	}
	activation := Activation{Record: platform.Record{ID: fmt.Sprintf("%04d-r", release.Version)}, Release: release.Previous,
		From: release.Version, Result: StateRequested, By: c.ID, At: now}
	return func(r *pb.ChangeRecord) { c.Put(r, activation) }, nil
}

func (s *System) finished(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct{ Result, Evidence string }
	if json.Unmarshal(submission.GetPayload(), &p) != nil || p.Result == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An activation result needs its outcome")
	}
	activation, known := platform.Get[Activation](c, submission.GetTarget().GetId())
	if !known || activation.Result != StateRequested {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a requested activation reports an outcome")
	}
	switch p.Result {
	case StateActive:
		// The window bounds when a person may cancel and when a swap may
		// start; it does not expire the updater's report of what happened.
	case StateRolledBack, "aborted":
	default:
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An activation is active, rolled back or aborted")
	}
	activation.Result, activation.Evidence = p.Result, p.Evidence
	if p.Result != StateActive {
		activation.Failure = p.Evidence
	}
	puts := []any{activation}
	if releases, _, err := platform.Find[Release](c, platform.Query{Archived: true}); err == nil {
		for _, release := range releases {
			switch {
			case release.Version == activation.Release && p.Result == StateActive:
				release.State = StateActive
				puts = append(puts, release)
			case p.Result == StateActive && release.Version != activation.Release && (release.State == StateActive || release.State == StateRequested):
				// One release runs: the old one is superseded, and a request
				// that never swapped returns to published.
				if release.State == StateActive {
					release.State = StateSuperseded
				} else {
					release.State = StatePublished
				}
				puts = append(puts, release)
			case release.Version == activation.Release && p.Result != StateActive:
				// The swap did not happen: the release stays available.
				release.State = StatePublished
				puts = append(puts, release)
			}
		}
	}
	if p.Result == StateActive {
		if others, _, err := platform.Find[Activation](c, platform.Query{Archived: true}); err == nil {
			for _, other := range others {
				if other.ID == activation.ID {
					continue
				}
				switch {
				case other.Result == StateRequested:
					other.Result, other.Failure = "aborted", "superseded by release "+fmt.Sprint(activation.Release)
					puts = append(puts, other)
				case other.Result == StateActive:
					// One release runs: the older activation is history.
					other.Result = StateSuperseded
					puts = append(puts, other)
				}
			}
		}
	}
	return func(r *pb.ChangeRecord) {
		for _, v := range puts {
			c.Put(r, v)
		}
	}, nil
}

// blobID is one content's stable record identity.
func blobID(digest string) string { return "b" + digest[:16] }

// fileID is one path's stable record identity.
func fileID(path string) string {
	sum := systemscope.Digest([]byte(path))
	return "f" + sum[:16]
}
