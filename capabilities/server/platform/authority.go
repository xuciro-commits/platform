package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"time"
)

type ChangeArchive interface {
	Records(tenant string, through int) []*pb.ChangeRecord
	Accepted(tenant, key string, through int) *pb.ChangeRecord
	Contains(tenant, change string, through int) bool
}

type ChangeLog interface {
	Records(tenant string) []*pb.ChangeRecord
	Last(tenant string) *pb.ChangeRecord
	ApplyAccepted(*pb.ChangeRecord) (bool, error)
	SubmitChecked(*pb.Submission, time.Time, func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error)
	Fork() ChangeLog
}

type Authority interface {
	ID() string
	Catalog() *Catalog
	Declarations() []*pb.AuthorityDeclaration
	Extend(classes []string, actions []Action) error
	Receive(c Caller, s *pb.Submission, now time.Time, allowed func() bool, rules func() (func(*pb.ChangeRecord), *kernel.Error)) (*pb.ChangeRecord, *kernel.Error)
	Generated(c Caller, s *pb.Submission, now time.Time, allowed func() bool, entities ...Entity) (*pb.ChangeRecord, *kernel.Error, bool)
	ApplyTxnRecord(c Caller, r *pb.ChangeRecord, e Entity, verb, id string, payload json.RawMessage, now time.Time) *kernel.Error
	ApplyTxnRoute(c Caller, r *pb.ChangeRecord, e Entity, target TxnRef, now time.Time, run func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *kernel.Error) *kernel.Error
	RecordsFor(tenant string) []*pb.ChangeRecord
	LastAccepted(tenant string) *pb.ChangeRecord
	AcceptedFor(tenant, key string) *pb.ChangeRecord
	ApplyAcceptedChange(record *pb.ChangeRecord) (bool, error)
	UseChangeArchive(archive ChangeArchive)
	ForkAcceptedChanges() ChangeLog
	ForkRead() (Authority, error)
	Snapshot() (json.RawMessage, error)
	Restore(raw json.RawMessage) error
	SnapshotWith(own any) (json.RawMessage, error)
	RestoreWith(raw json.RawMessage, own any) error
}

type Authorities func(tenant, id string, catalog *Catalog, classes ...string) Authority

func (c Caller) Runtime() Runtime       { return c.rt }
func (c Caller) SourceProjection() bool { return c.sourceProjection }
func (c Caller) RestoreSource() bool    { return c.restoreSource }
func (c Caller) CreationDepth() int     { return c.creationDepth }

func (c Caller) Deeper() Caller {
	c.creationDepth++
	return c
}

func (a Action) EditsRecord() bool { return a.recordEdit }

type TransactionHost interface {
	CheckTxnCall(TxnCall) error
}
