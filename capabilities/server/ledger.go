package platformserver

import (
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Ledger is one business package's decisions in one tenant: the kernel's change
// log and authority for the package's data classes, received in the kernel's
// order with the package's action catalog as the role check (ADR-0008). The
// package supplies attribute conditions and its rules; it never wires the kernel.
type Ledger struct {
	mu           sync.Mutex
	tenant       string
	declarations []*pb.AuthorityDeclaration
	Changes      *kernel.ChangeLog
	authorities  *kernel.Authorities
	Catalog      *Catalog
}

// NewLedger declares authority as the tenant server for classes and accepts the
// catalog's actions (version 1 of each schema).
func NewLedger(tenant, authority string, catalog *Catalog, classes ...string) *Ledger {
	var schemas []*pb.SchemaRef
	for _, a := range catalog.actions {
		schemas = append(schemas, &pb.SchemaRef{Name: a.Schema, Version: 1})
	}
	l := &Ledger{tenant: tenant, Changes: kernel.NewChangeLog(kernel.NewSchemaRegistry(schemas, nil)),
		authorities: kernel.NewAuthorities(authority), Catalog: catalog}
	for _, class := range classes {
		d := &pb.AuthorityDeclaration{TenantId: tenant, DataClass: class, Kind: pb.AuthorityKind_AUTHORITY_KIND_TENANT_SERVER, AuthorityId: authority, Epoch: 1}
		l.authorities.Declare(d)
		l.declarations = append(l.declarations, d)
	}
	return l
}

// Declarations are the package's authority declarations, for edges (K5 A9).
func (l *Ledger) Declarations() []*pb.AuthorityDeclaration { return l.declarations }

// Receive accepts s from c or refuses it: an action outside the enabled catalog
// is UNKNOWN_SCHEMA; c's role must be granted and allowed (attribute conditions,
// may be nil) must hold, except in a replay (ADR-0008); rules (may be nil)
// returns how to apply the decision, which runs with the new record only when
// it is accepted (an idempotent replay applies nothing).
func (l *Ledger) Receive(c Caller, s *pb.Submission, now time.Time,
	allowed func() bool, rules func() (func(*pb.ChangeRecord), *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !c.Replaying && !l.Catalog.Enabled(s.GetSchema().GetName()) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
	}
	receiver := kernel.Receiver{Changes: l.Changes, Authorities: l.authorities,
		Policy: func(kernel.Caller, *pb.Submission) bool {
			return c.Replaying || l.Catalog.Permits(c.Role(), s.GetSchema().GetName()) && (allowed == nil || allowed())
		}}
	var apply func(*pb.ChangeRecord)
	record, err := receiver.Receive(kernel.Caller{Tenant: l.tenant, Principal: c.ID}, s, now, func() *kernel.Error {
		if rules == nil {
			return nil
		}
		var err *kernel.Error
		apply, err = rules()
		return err
	})
	if err == nil && apply != nil {
		apply(record)
	}
	return record, err
}
