package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"time"
)

type ChangeArchive interface {
	Records(tenant string, through int) []*pb.ChangeRecord
	Accepted(tenant, key string, through int) *pb.ChangeRecord
	Contains(tenant, change string, through int) bool
}

type ChangeLog interface {
	Records(tenant string) []*pb.ChangeRecord
	Accepted(tenant, key string) *pb.ChangeRecord
	Last(tenant string) *pb.ChangeRecord
	ApplyAccepted(*pb.ChangeRecord) (bool, error)
	SubmitChecked(*pb.Submission, time.Time, func() *Error, func() *Error) (*pb.ChangeRecord, *Error)
	Fork() ChangeLog
}

type Authority interface {
	ID() string
	Catalog() *Catalog
	Declarations() []*pb.AuthorityDeclaration
	Extend(classes []string, actions []Action) error
	Receive(c Caller, s *pb.Submission, now time.Time, allowed func() bool, rules func() (func(*pb.ChangeRecord), *Error)) (*pb.ChangeRecord, *Error)
	Generated(c Caller, s *pb.Submission, now time.Time, allowed func() bool, entities ...RecordType) (*pb.ChangeRecord, *Error, bool)
	ApplyTxnRecord(c Caller, r *pb.ChangeRecord, e RecordType, verb, id string, payload json.RawMessage, now time.Time) *Error
	ApplyTxnRoute(c Caller, r *pb.ChangeRecord, e RecordType, target TxnRef, now time.Time, run func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *Error) *Error
	RecordsFor(tenant string) []*pb.ChangeRecord
	LastAccepted(tenant string) *pb.ChangeRecord
	AcceptedFor(tenant, key string) *pb.ChangeRecord
	ApplyAcceptedChange(record *pb.ChangeRecord) (bool, error)
	UseChangeArchive(archive ChangeArchive)
	RetainAcceptedChanges(retain bool)
	ForkAcceptedChanges() ChangeLog
	ForkRead() (Authority, error)
	Snapshot() (json.RawMessage, error)
	Restore(raw json.RawMessage) error
	SnapshotWith(own any) (json.RawMessage, error)
	RestoreWith(raw json.RawMessage, own any) error
}

type Authorities func(tenant, id string, catalog *Catalog, classes ...string) Authority

func (c Caller) SourceProjection() bool { return c.sourceProjection }
func (c Caller) RestoreSource() bool    { return c.restoreSource }
func (c Caller) CreationDepth() int     { return c.creationDepth }
func (c Caller) HasRuntime() bool       { return c.rt != nil }

func (c Caller) Publish(record *pb.ChangeRecord) {
	if c.rt != nil {
		c.rt.Publish(c, record)
	}
}

func (c Caller) Deeper() Caller {
	c.creationDepth++
	return c
}

type TransactionHost interface {
	CheckTxnCall(TxnCall) error
}
