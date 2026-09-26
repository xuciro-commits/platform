// Package files is the platform's files app (ADR-0028), on the app API and
// internal/host (ADR-0025 D4): a file is uploaded to the tenant's object
// store, then attached to a record by a decision that names its sha256, so the
// journal holds the hash and never the bytes. A file is readable exactly when
// the record it is attached to is.
package files

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

const (
	ID           = "files"
	FileType     = "files.file"
	SchemaAttach = "files.file.attach"
	SchemaDetach = "files.file.detach"
	SettingMax   = "max-size-mb"
)

// File is a file attached to a record.
type File struct {
	platform.Record
	Name        string `json:"name" field:"required,search" help:"The file's name as it was uploaded"`
	ContentType string `json:"contentType" field:"readonly" title:"Type"`
	Size        int    `json:"size" field:"readonly" help:"Bytes"`
	Hash        string `json:"hash" field:"readonly" help:"The SHA-256 of its bytes: where the store keeps them"`
	Target      string `json:"target" field:"readonly" title:"Attached to" help:"<type>/<id> of the record"`
	By          string `json:"by" field:"readonly" title:"Attached by"`
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Files struct {
	mu     sync.Mutex
	host   host.Host
	ledger *platform.Ledger
}

// Attach is called by the host when a tenant is composed.
func (f *Files) Attach(h host.Host) { f.host = h }

// New is a tenant's files app.
func New(tenant string) *Files {
	any := []string{platform.AnyMember}
	return &Files{ledger: platform.NewLedger(tenant, ID, platform.NewCatalog(
		platform.Action{Schema: SchemaAttach, Target: FileType, Capability: "files", Title: "Attach file", Roles: any,
			Description: "Attach an uploaded file to a record you may read; whoever may read the record may read the file.",
			Payload: []platform.Field{{Name: "hash", Type: "string", Required: true, Description: "The SHA-256 the upload answered with"},
				{Name: "name", Type: "string", Required: true, Description: "File name"},
				{Name: "contentType", Type: "string", Description: "Media type"},
				{Name: "size", Type: "integer", Required: true, Description: "Bytes"},
				{Name: "target", Type: "string", Required: true, Description: "<type>/<id> of the record"}}},
		platform.Action{Schema: SchemaDetach, Target: FileType, Capability: "files", Title: "Remove file", Roles: any,
			Description: "Remove a file you attached from its record; it stays in the record's history.", Payload: []platform.Field{}},
	), FileType)}
}

func entities() []platform.Entity {
	return []platform.Entity{{Type: FileType, Title: "File", Model: File{}, Display: "name", Synonyms: "attachment,document",
		Description: "A file attached to a record, readable exactly when the record is.",
		Scope:       platform.Scope{Through: func(record any) string { return record.(File).Target }}}}
}

func (f *Files) Manifest() platform.Manifest {
	return platform.Manifest{ID: ID, Title: "Files", Version: "1", Actions: f.ledger.Catalog, Entities: entities(),
		Settings: []platform.Setting{{Name: SettingMax, Title: "Largest file, MB", Type: "integer", Default: "25",
			Description: "Uploads larger than this are refused."}}}
}

func (f *Files) Declarations() []*pb.AuthorityDeclaration { return f.ledger.Declarations() }
func (f *Files) Snapshot() (json.RawMessage, error)       { return f.ledger.Snapshot() }
func (f *Files) Restore(raw json.RawMessage) error        { return f.ledger.Restore(raw) }
func (f *Files) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (f *Files) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Max is the largest file the tenant accepts, in bytes.
func Max(c platform.Caller) int {
	mb, err := strconv.Atoi(c.Setting(SettingMax))
	if err != nil || mb <= 0 {
		mb = 25
	}
	return mb << 20
}

func (f *Files) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		id := s.GetTarget().GetId()
		existing, known := platform.Get[File](c, id)
		switch s.GetSchema().GetName() {
		case SchemaAttach:
			var p struct {
				Hash, Name, ContentType, Target string
				Size                            int
			}
			if json.Unmarshal(s.GetPayload(), &p) != nil || !sha256Hex.MatchString(p.Hash) || strings.TrimSpace(p.Name) == "" || p.Size < 0 || !strings.Contains(p.Target, "/") {
				return nil, invalid
			}
			if known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			// What was checked when it was attached stands in a replay, which reads no bytes.
			if !c.Replaying {
				if !f.host.Readable(c.Member, p.Target, now) {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
				}
				if p.Size > Max(c) || !f.host.Stored(c.Tenant, p.Hash) {
					return nil, invalid
				}
			}
			file := File{Record: platform.Record{ID: id}, Name: strings.TrimSpace(p.Name), ContentType: p.ContentType, Size: p.Size, Hash: p.Hash, Target: p.Target, By: c.ID}
			return func(r *pb.ChangeRecord) { c.Put(r, file) }, nil
		case SchemaDetach:
			if !known || existing.Archived {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			if existing.By != c.ID && !c.Automation && !c.Replaying {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
			}
			existing.Archived = true
			return func(r *pb.ChangeRecord) { c.Put(r, existing) }, nil
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	})
}
