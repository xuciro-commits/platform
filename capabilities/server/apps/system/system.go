// Package system retains the platform's accepted source, release and activation
// history. New self-hosting work is frozen; static maintenance may reconcile
// outcomes already requested before the freeze.
package system

import (
	"embed"
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

//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

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
	Editable bool   `json:"editable"`
}

// SourceImage keeps a proposal's actual baseline for later review.
type SourceImage struct {
	Digest  string `json:"digest"`
	Content string `json:"content"`
}

// ChangeFile is the frozen content of one path in a change.
type ChangeFile struct {
	Path     string       `json:"path"`
	Content  string       `json:"content,omitempty"`
	Digest   string       `json:"digest"`
	Deleted  bool         `json:"deleted,omitempty"`
	Revision *int         `json:"revision,omitempty"`
	Before   *SourceImage `json:"before,omitempty"`
}

// Change is one proposed iteration of the system: where it came from (a person
// or a prompt), the base release, the frozen file set, who approved it, and
// what the builder reported.
type Change struct {
	platform.Record
	Title        string              `json:"title" field:"required,search" source:"user-input"`
	Rationale    string              `json:"rationale" field:"required" type:"longtext" source:"user-input"`
	Origin       string              `json:"origin" field:"readonly" choices:"ui,prompt"`
	Prompt       string              `json:"prompt,omitempty" field:"readonly" type:"longtext" source:"user-input"`
	Agent        string              `json:"agent,omitempty" field:"readonly" source:"record"`
	Run          string              `json:"run,omitempty" field:"readonly" source:"record"`
	Base         int                 `json:"base,omitempty" field:"readonly"`
	SourceDigest string              `json:"sourceDigest,omitempty" field:"readonly" source:"record"`
	Files        []ChangeFile        `json:"files" field:"readonly" type:"json" title:"Frozen file set"`
	Assets       []platform.AssetRef `json:"assets,omitempty" field:"readonly" type:"json" source:"record"`
	Candidate    json.RawMessage     `json:"candidate,omitempty" field:"readonly" type:"json" source:"record"`
	Manifest     string              `json:"manifest,omitempty" field:"readonly" source:"external-input"`
	State        string              `json:"state" field:"readonly" choices:"draft,approved,built,published,failed,rejected"`
	ApprovedBy   string              `json:"approvedBy,omitempty" field:"readonly" source:"user-input"`
	ApprovedAt   time.Time           `json:"approvedAt,omitzero" field:"readonly"`
	Diagnostics  string              `json:"diagnostics,omitempty" field:"readonly" type:"longtext" source:"external-input"`
	Release      int                 `json:"release,omitempty" field:"readonly"`
}

// Release is one immutable system version: what was built, with which
// toolchain, and which release it replaces.
type Release struct {
	platform.Record
	Version        int               `json:"version" field:"required,search"`
	Change         string            `json:"change" field:"required" source:"external-input"`
	Manifest       string            `json:"manifest" field:"required" type:"longtext" source:"external-input"`
	ManifestDigest string            `json:"manifestDigest" field:"readonly" source:"external-input"`
	Binary         string            `json:"binary,omitempty" field:"readonly" source:"external-input"`
	Web            string            `json:"web,omitempty" field:"readonly" source:"external-input"`
	Toolchain      string            `json:"toolchain,omitempty" field:"readonly" source:"external-input"`
	Dependencies   map[string]string `json:"deps,omitempty" field:"readonly" type:"json" source:"record"`
	Previous       int               `json:"previous,omitempty" field:"readonly"`
	SourceDigest   string            `json:"sourceDigest,omitempty" field:"readonly" source:"record"`
	Candidate      json.RawMessage   `json:"candidate,omitempty" field:"readonly" type:"json" source:"record"`
	Kind           string            `json:"kind,omitempty" field:"readonly" source:"record"`
	Installation   json.RawMessage   `json:"installation,omitempty" field:"readonly" type:"json" source:"record"`
	State          string            `json:"state" field:"readonly" choices:"published,requested,active,superseded,failed"`
	Diagnostics    string            `json:"diagnostics,omitempty" field:"readonly" type:"longtext" source:"external-input"`
}

// Activation is the evidence of making a release live, or of rolling back.
type Activation struct {
	platform.Record
	Release      int             `json:"release" field:"required"`
	From         int             `json:"from,omitempty" field:"readonly"`
	Result       string          `json:"result" field:"required" choices:"requested,active,rolled-back,aborted"`
	Window       int             `json:"window,omitempty" field:"readonly" help:"Seconds before the swap during which a person may cancel"`
	Evidence     string          `json:"evidence,omitempty" type:"longtext" source:"user-input"`
	Failure      string          `json:"failure,omitempty" type:"longtext" source:"external-input"`
	By           string          `json:"by,omitempty" field:"readonly" source:"user-input"`
	At           time.Time       `json:"at,omitzero" field:"readonly"`
	Installation json.RawMessage `json:"installation,omitempty" field:"readonly" type:"json" source:"record"`
}

// System retains the original system authority and its deployment history.
type System struct {
	mu             sync.Mutex
	ledger         *platform.Ledger
	source         *platform.SystemSource
	definitionHost platform.SystemDefinitionHost
}

// New retains the original system declarations for accepted history. Only
// reports about existing work remain writable through static maintenance.
func New(tenant string, sources ...*platform.SystemSource) *System {
	any := []string{Builder, Publisher}
	publish := []string{Publisher}
	actions := []platform.Action{
		{Schema: SchemaFileSave, Target: FileType, Title: "Save system file", Capability: "system",
			Description: "Write one system source file at its expected revision; the kernel gate refuses frozen paths.",
			Roles:       any, Payload: []platform.Field{{Name: "path", Type: "string", Required: true, Description: "Repository-relative path"}, {Name: "content", Type: "string", Description: "The file's new content"}, {Name: "revision", Type: "integer", Description: "The revision the writer expects; 0 creates a new path"}, {Name: "deleted", Type: "boolean", Description: "Append a tombstone instead of writing bytes"}}},
		{Schema: SchemaPropose, Target: ChangeType, Title: "Propose system change", Capability: "system",
			Description: "Freeze a title, a rationale, a prompt when one produced it, and the files this iteration touches.",
			Roles:       any, Payload: []platform.Field{{Name: "title", Type: "string", Required: true}, {Name: "rationale", Type: "string", Required: true}, {Name: "origin", Type: "string", Description: "ui (default) or prompt"}, {Name: "prompt", Type: "string"}, {Name: "base", Type: "integer"}, {Name: "files", Type: "json", Description: "[{path, content, revision}] or [{path, deleted:true}]"}, {Name: "assets", Type: "json", Description: "Build draft asset references frozen in this change"}}},
		{Schema: SchemaApprove, Target: ChangeType, Title: "Approve system change", Capability: "system",
			Description: "A publisher approves a proposed change; its file set freezes and the builder may run.",
			Roles:       publish, Payload: []platform.Field{}},
		{Schema: SchemaBuilt, Target: ChangeType, Title: "Record build", Capability: "system",
			Description: "The updater reports what the approved change built: a release, or why it did not build.",
			Roles:       publish, Payload: []platform.Field{{Name: "ok", Type: "boolean", Required: true}, {Name: "binary", Type: "string"}, {Name: "web", Type: "string"}, {Name: "toolchain", Type: "string"}, {Name: "diagnostics", Type: "string"}, {Name: "sourceDigest", Type: "string"}, {Name: "manifest", Type: "json"}}},
		{Schema: SchemaActivate, Target: ReleaseType, Title: "Activate release", Capability: "system",
			Description: "Ask the updater to make a published release live; a person may cancel inside the window.",
			Roles:       publish, Payload: []platform.Field{{Name: "window", Type: "integer", Description: "Seconds before the swap; default 30"}}},
		{Schema: SchemaDeactivate, Target: ReleaseType, Title: "Roll back", Capability: "system",
			Description: "Return to the release this one replaced.",
			Roles:       publish, Payload: []platform.Field{{Name: "window", Type: "integer", Description: "Cancellation seconds; default 30; recovery may use 0"}, {Name: "targetVersion", Type: "integer", Description: "Optional ancestor release for recovery; default direct predecessor"}}},
		{Schema: SchemaCancel, Target: ReleaseType, Title: "Cancel activation", Capability: "system",
			Description: "Cancel a requested activation while the window is still open; the running release stays.",
			Roles:       publish, Payload: []platform.Field{{Name: "reason", Type: "string"}, {Name: "activationId", Type: "string"}}},
		{Schema: SchemaFinished, Target: ActivationType, Title: "Record activation", Capability: "system",
			Description: "The updater reports the swap outcome: active, rolled-back or aborted.",
			Roles:       publish, Payload: []platform.Field{{Name: "result", Type: "string", Required: true, Choices: []string{StateActive, StateRolledBack, "aborted"}}, {Name: "evidence", Type: "string"}}},
	}
	actions = append(actions, platform.EntityActions(entity(FileType, "File", File{}, "path"))...)
	actions = append(actions, platform.EntityActions(entity(ChangeType, "System change", Change{}, "title"))...)
	actions = append(actions, platform.EntityActions(entity(ReleaseType, "System release", Release{}, "version"))...)
	actions = append(actions, platform.EntityActions(entity(ActivationType, "Activation", Activation{}, "result"))...)
	app := &System{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), BlobType, FileType, ChangeType, ReleaseType, ActivationType)}
	if len(sources) > 0 {
		app.source = sources[0]
	}
	return app
}

func entity(typ, title string, model any, display string) platform.Entity {
	return platform.Entity{Type: typ, Title: title, Model: model, Display: display, Description: "Self-hosting: the system's own " + strings.ToLower(title) + "."}
}

func (s *System) Manifest() platform.Manifest {
	return platform.Manifest{Languages: languages, ID: ID, Title: "Platform", Version: "1", Actions: s.ledger.Catalog,
		Entities: []platform.Entity{
			// Blobs hold file bytes keyed by digest; they have no actions of
			// their own and are written only as part of a file save.
			entity(BlobType, "Blob", Blob{}, "digest"),
			entity(FileType, "File", File{}, "path"), entity(ChangeType, "System change", Change{}, "title"),
			entity(ReleaseType, "System release", Release{}, "version"), entity(ActivationType, "Activation", Activation{}, "result"),
		},
		Reads:      []string{"system-files", "system-file", "system-changes", "system-releases", "system-activations"},
		ReadInputs: map[string][]platform.Field{"system-files": {{Name: "search", Type: "string"}, {Name: "offset", Type: "integer"}, {Name: "limit", Type: "integer"}}, "system-file": {{Name: "path", Type: "string", Required: true}}},
		ReadSources: map[string]string{"system-files": "record", "system-file": "record", "system-changes": "record",
			"system-releases": "record", "system-activations": "record"},
		Roles: []string{Builder, Publisher},
		Agents: []platform.Agent{{
			Name: "architect", Title: "System history assistant",
			Description:  "Reads retained system source and release history. System self-hosting writes are frozen.",
			Instructions: "Read the retained system history to explain the deployed version. Never edit, propose, approve, build or activate system changes. Direct maintenance to the static system entry.",
			Tools:        []string{"read:system-files", "read:system-file"},
			Budget:       platform.Budget{Steps: 20, Tokens: 60000, Cost: 2},
		}},
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
	return s.ReadInput(c, name, nil)
}

func (s *System) ReadInput(c platform.Caller, name string, input json.RawMessage) (any, *kernel.Error) {
	if c.Tenant != systemscope.Tenant {
		return []any{}, nil
	}
	switch name {
	case "system-files", "system-file":
		var query struct {
			Path, Search  string
			Offset, Limit int
		}
		if len(input) > 0 && json.Unmarshal(input, &query) != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unreadable source query")
		}
		if query.Limit == 0 {
			query.Limit = 200
		}
		if query.Offset < 0 || query.Limit < 1 || query.Limit > 1000 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a source window of 1 to 1000 files")
		}
		files, _, err := platform.Find[File](c, platform.Query{Sort: []string{"path"}})
		if err != nil {
			return nil, err
		}
		byPath := map[string]FileView{}
		if s.source != nil {
			for _, file := range s.source.Files {
				byPath[file.Path] = FileView{Path: file.Path, Digest: file.Digest, Size: file.Size, Editable: false}
			}
		}
		for _, file := range files {
			view := FileView{Path: file.Path, Digest: file.Digest, Size: file.Size, Revision: file.Revision, Deleted: file.Deleted, By: file.By, Editable: false}
			if name == "system-file" && query.Path == file.Path && !file.Deleted {
				if blob, ok := platform.Get[Blob](c, blobID(file.Digest)); ok {
					view.Content = blob.Content
				}
			}
			byPath[file.Path] = view
		}
		if name == "system-file" {
			view, ok := byPath[query.Path]
			if !ok {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Source file {path} is unavailable", query.Path)
			}
			if view.Revision == 0 && s.source != nil {
				body, err := s.source.Read(query.Path)
				if err != nil {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
				}
				view.Content = body
			}
			return view, nil
		}
		out := make([]FileView, 0, len(byPath))
		for _, view := range byPath {
			if query.Search == "" || strings.Contains(strings.ToLower(view.Path), strings.ToLower(query.Search)) {
				out = append(out, view)
			}
		}
		slices.SortFunc(out, func(a, b FileView) int { return strings.Compare(a.Path, b.Path) })
		from := min(query.Offset, len(out))
		until := min(from+query.Limit, len(out))
		out = out[from:until]
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
		if c.Agent {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System self-hosting is frozen; use static maintenance to reconcile retained work")
		}
		switch submission.GetSchema().GetName() {
		case SchemaFileSave, SchemaPropose, SchemaApprove, SchemaActivate, SchemaDeactivate:
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System self-hosting is frozen; use static maintenance to reconcile retained work")
		case SchemaBuilt:
			return s.built(c, submission, now)
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
	return &System{ledger: s.ledger, source: s.source, definitionHost: s.definitionHost}, nil
}

var _ platform.DecisionForkApp = (*System)(nil)

func (s *System) runningSourceBase(c platform.Caller) int {
	if releases, _, err := platform.Find[Release](c, platform.Query{Sort: []string{"-version"}}); err == nil {
		for _, release := range releases {
			if release.State == StateActive {
				return release.Version
			}
		}
	}
	if s.source != nil {
		return s.source.Version
	}
	return 0
}

func (s *System) built(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct {
		OK                                                bool
		Binary, Web, Toolchain, Diagnostics, SourceDigest string
		Manifest                                          map[string]string
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
	if change.Base != s.runningSourceBase(c) {
		change.State, change.Diagnostics = StateFailed, "The approved source baseline is no longer active; propose the change again"
		return func(r *pb.ChangeRecord) { c.Put(r, change) }, nil
	}
	if p.Binary == "" || p.Web == "" || p.SourceDigest == "" || len(p.Manifest) == 0 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A code release needs complete source and binary evidence")
	}
	releases, _, _ := platform.Find[Release](c, platform.Query{Sort: []string{"-version"}, Limit: 1})
	previous, version := change.Base, 1
	if len(releases) > 0 {
		version = releases[0].Version + 1
	}
	manifest, _ := json.Marshal(p.Manifest)
	deps := map[string]string{}
	for _, path := range []string{"capabilities/server/go.mod", "capabilities/server/go.sum", "web/pnpm-lock.yaml"} {
		if digest := p.Manifest[path]; digest != "" {
			deps[path] = digest
		}
	}
	release := Release{Record: platform.Record{ID: fmt.Sprintf("%04d", version)}, Version: version, Change: change.ID,
		Manifest: string(manifest), ManifestDigest: systemscope.Manifest(p.Manifest), Dependencies: deps,
		Binary: p.Binary, Web: p.Web, Toolchain: p.Toolchain, Previous: previous, State: StatePublished,
		Diagnostics: p.Diagnostics, SourceDigest: p.SourceDigest, Candidate: change.Candidate, Kind: "code"}
	change.State, change.Release = StatePublished, version
	return func(r *pb.ChangeRecord) { c.Put(r, change); c.Put(r, release) }, nil
}

func (s *System) cancel(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct{ Reason, ActivationID string }
	if json.Unmarshal(submission.GetPayload(), &p) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The cancellation request is unreadable")
	}
	release, known := platform.Get[Release](c, submission.GetTarget().GetId())
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The release is unavailable")
	}
	pending, _, _ := platform.Find[Activation](c, platform.Query{Archived: true})
	for _, activation := range pending {
		if activation.Release != release.Version || activation.Result != StateRequested || p.ActivationID != "" && activation.ID != p.ActivationID {
			continue
		}
		if !now.Before(activation.At.Add(time.Duration(activation.Window) * time.Second)) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The cancellation window has closed; wait for the updater outcome")
		}
		activation.Result, activation.Failure = "aborted", p.Reason
		if p.Reason == "" {
			activation.Failure = "cancelled by " + c.ID
		}
		if release.State == StateRequested {
			release.State = StatePublished
		}
		return func(r *pb.ChangeRecord) { c.Put(r, release); c.Put(r, activation) }, nil
	}
	return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The release has no matching open activation")
}

func (s *System) superseded(c platform.Caller, current Activation, handled ...int) []any {
	var puts []any
	releases, _, _ := platform.Find[Release](c, platform.Query{Archived: true})
	for _, release := range releases {
		if release.Version != current.Release && release.State == StateActive && !slices.Contains(handled, release.Version) {
			release.State = StateSuperseded
			puts = append(puts, release)
		}
	}
	activations, _, _ := platform.Find[Activation](c, platform.Query{Archived: true})
	for _, activation := range activations {
		if activation.ID != current.ID && activation.Result == StateActive {
			activation.Result = StateSuperseded
			puts = append(puts, activation)
		}
	}
	return puts
}

func (s *System) finished(c platform.Caller, submission *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	var p struct{ Result, Evidence string }
	if json.Unmarshal(submission.GetPayload(), &p) != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An activation result needs its outcome")
	}
	activation, known := platform.Get[Activation](c, submission.GetTarget().GetId())
	if !known || activation.Result != StateRequested {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only a requested activation reports an outcome")
	}
	if p.Result != StateActive && p.Result != StateRolledBack && p.Result != "aborted" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An activation is active, rolled back or aborted")
	}
	release, known := platform.Get[Release](c, fmt.Sprintf("%04d", activation.Release))
	if !known {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The activation release is unavailable")
	}
	activation.Result, activation.Evidence = p.Result, p.Evidence
	if p.Result == StateActive {
		if err := s.prepareDefinitionActivation(c, &activation, &release, now); err != nil {
			return nil, err
		}
		release.State = StateActive
	} else {
		activation.Failure = p.Evidence
		if release.State == StateRequested {
			release.State = StatePublished
		}
	}
	puts := []any{activation, release}
	if p.Result == StateActive {
		puts = append(puts, s.superseded(c, activation)...)
	}
	return func(r *pb.ChangeRecord) {
		for _, v := range puts {
			c.Put(r, v)
		}
	}, nil
}

// blobID is one content's stable record identity.
func blobID(digest string) string { return "b" + digest[:16] }
