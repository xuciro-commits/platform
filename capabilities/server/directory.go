package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// The platform app (ADR-0010): the tenant's directory of members, their role in
// each app and their attributes. Granting and revoking are its decisions, so the
// directory has history, survives restarts through the journal, and a change
// takes effect on the next request.
const (
	PlatformApp = "platform"
	MemberType  = "platform.member"
	SchemaGrant = "platform.member.grant"
	// SchemaRevoke removes a member's role in one app.
	SchemaRevoke = "platform.member.revoke"
	Admin        = "admin"
)

// Seat is a directory entry as configured: the subjects that sign in as the member.
type Seat struct {
	Subjects []string `json:"subjects"` // "user:<email>", "client:<id>", or a development token's subject
	Member
}

type Directory struct {
	mu       sync.Mutex
	members  map[string]*Member
	subjects map[string]string // subject → member ID
	ledger   *Ledger
}

func DirectoryActions() *Catalog {
	return NewCatalog(
		Action{Schema: SchemaGrant, Target: MemberType, Capability: "members", Title: "Grant role",
			Description: "Give a member a role in an app, replacing the role held there.",
			Payload: []Field{{Name: "app", Type: "string", Required: true, Description: "App ID"},
				{Name: "role", Type: "string", Required: true, Description: "A role the app defines"}}, Roles: []string{Admin}},
		Action{Schema: SchemaRevoke, Target: MemberType, Capability: "members", Title: "Revoke role",
			Description: "Remove a member's role in an app.",
			Payload:     []Field{{Name: "app", Type: "string", Required: true, Description: "App ID"}}, Roles: []string{Admin}},
	)
}

// NewDirectory seeds a tenant's directory; grants recorded later replay on top.
func NewDirectory(tenant string, seats ...Seat) *Directory {
	d := &Directory{members: map[string]*Member{}, subjects: map[string]string{},
		ledger: NewLedger(tenant, PlatformApp, DirectoryActions(), MemberType)}
	for _, s := range seats {
		m := s.Member
		m.Tenant, m.Roles = tenant, maps.Clone(m.Roles)
		if m.Roles == nil {
			m.Roles = map[string]string{}
		}
		d.members[m.ID] = &m
		for _, subject := range s.Subjects {
			d.subjects[subject] = m.ID
		}
	}
	return d
}

// Member is the member a subject signs in as, with its current roles.
func (d *Directory) Member(subject string) (Member, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[d.subjects[subject]]
	if m == nil {
		return Member{}, false
	}
	out := *m
	out.Roles = maps.Clone(m.Roles)
	return out, true
}

func (d *Directory) Manifest() Manifest {
	return Manifest{ID: PlatformApp, Version: "1", Actions: d.ledger.Catalog, Reads: []string{"members"}}
}

func (d *Directory) Declarations() []*pb.AuthorityDeclaration { return d.ledger.Declarations() }

func (d *Directory) Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var p struct{ App, Role string }
		m := d.members[s.GetTarget().GetId()]
		if json.Unmarshal(s.GetPayload(), &p) != nil || p.App == "" || s.GetSchema().GetName() == SchemaGrant && p.Role == "" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		if m == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return func(*pb.ChangeRecord) {
			if s.GetSchema().GetName() == SchemaGrant {
				m.Roles[p.App] = p.Role
			} else {
				delete(m.Roles, p.App)
			}
		}, nil
	})
}

// Read "members": every member with its roles, sorted by ID.
func (d *Directory) Read(Caller, string) any {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := []Member{}
	for _, m := range d.members {
		c := *m
		c.Roles = maps.Clone(m.Roles)
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Member) int { return strings.Compare(a.ID, b.ID) })
	return out
}

func (d *Directory) Input(Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
