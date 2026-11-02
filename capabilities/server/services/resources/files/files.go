package files

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

const (
	ID             = "files"
	FileType       = "files.file"
	SchemaAttach   = "files.file.attach"
	SchemaDetach   = "files.file.detach"
	SchemaAnnotate = "files.file.annotate"
	SettingMax     = "max-size-mb"
)

type File = platform.File

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Files struct {
	mu     sync.Mutex
	host   platform.FileHost
	ledger *platform.Ledger
}

func (f *Files) AttachFiles(h platform.FileHost) { f.host = h }

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
		platform.Action{Schema: SchemaAnnotate, Target: FileType, Capability: "files", Title: "Save image regions", Roles: any,
			Description: "Replace normalized regions on your readable original raster attachment at its current revision.", Payload: []platform.Field{{Name: "hash", Type: "string", Required: true, Description: "The confirmed original attachment hash"}, {Name: "regions", Type: "json", Required: true, Description: "Stable region IDs, labels and normalized rectangles"}}},
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
func (f *Files) AcceptedLedger() *platform.Ledger         { return f.ledger }
func (f *Files) SnapshotFormat() string                   { return "platform.ledger.v1" }

func (f *Files) Snapshot() (json.RawMessage, error) { return f.ledger.Snapshot() }
func (f *Files) Restore(raw json.RawMessage) error  { return f.ledger.Restore(raw) }
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
			if !c.Replaying {
				if !f.host.Readable(c.Member, p.Target, now) {
					return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
				}
				if p.Size > platform.MaxFileSize(c) || !f.host.Stored(c.Tenant, p.Hash) {
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
		case SchemaAnnotate:
			if !known || existing.Archived {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			if !c.Replaying && (!f.host.Readable(c.Member, existing.Target, now) || existing.By != c.ID) {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
			}
			p, err := imageRegionsPayload(s.GetPayload())
			if err != nil || p.Hash != existing.Hash || !rasterAttachment(existing.ContentType) || s.GetExpectedRevision() == 0 {
				return nil, invalid
			}
			if s.GetExpectedRevision() != existing.Revision {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			existing.Regions = p.Regions
			return func(r *pb.ChangeRecord) { c.Put(r, existing) }, nil
		}
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	})
}

func (f *Files) ForkDecision() (platform.App, error) {
	return &Files{host: f.host, ledger: f.ledger}, nil
}

var _ platform.DecisionForkApp = (*Files)(nil)
