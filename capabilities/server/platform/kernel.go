package platform

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"time"
)

// KernelFactory creates isolated contract state. Implementations keep the
// original v1 wire images; they do not own business rules or authorization.
type KernelFactory interface {
	Changes(SchemaRegistry) ChangeLog
	Schemas([]*pb.SchemaRef) SchemaRegistry
	Authorities(string) Authorities
	Works() Works
	Connectors() Connectors
	Identity([]*pb.EntityRef) Identity
	Receive(ChangeLog, Authorities, kernel.Policy, kernel.Caller, *pb.Submission, time.Time, func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error)
}

type SchemaRegistry interface {
	Accepts(*pb.SchemaRef) bool
	Learn(...*pb.SchemaRef) *kernel.Error
}
type Authorities interface {
	Declare(*pb.AuthorityDeclaration) *kernel.Error
	Authorize(*pb.Submission) *kernel.Error
}
type ChangeLog interface {
	ForkLog() ChangeLog
	SetFacts(func(string, string) bool)
	Records(string) []*pb.ChangeRecord
	Last(string) *pb.ChangeRecord
	Accepted(string, string) *pb.ChangeRecord
	Compact(kernel.ChangeArchive)
	ApplyAccepted(*pb.ChangeRecord) (bool, error)
	SubmitChecked(*pb.Submission, time.Time, func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error)
	Restore(string, []*pb.ChangeRecord)
	Adopt([]*pb.ChangeRecord) *kernel.Error
}
type Works interface {
	Start(string, string) (uint32, string, *kernel.Error)
	Checkpoint(string, uint32, uint32, string) *kernel.Error
	Finish(string, uint32, bool) *kernel.Error
	Cancel(string) *kernel.Error
	CloseOwner(string)
	Get(string) (*pb.Work, *kernel.Error)
	All() []*pb.Work
	Restore([]*pb.Work)
}
type Connectors interface {
	Register(*pb.ConnectorDescriptor) *kernel.Error
	Deliver(string, string, string, string, string, time.Time) *kernel.Error
	Heartbeat(string, string, time.Time) *kernel.Error
	Status(string, string, time.Time) (*pb.ConnectorStatus, *kernel.Error)
	Descriptors(string) []*pb.ConnectorDescriptor
	State() ([]*pb.ConnectorDescriptor, []kernel.ConnectorMark)
	Restore([]*pb.ConnectorDescriptor, []kernel.ConnectorMark)
}

type Identity interface {
	Create(*pb.EntityRef) *kernel.Error
	AddRedirect(*pb.Redirect) *kernel.Error
	Resolve(*pb.EntityRef) ([]kernel.Ref, *kernel.Error)
}

type KernelRuntime interface{ Kernel() KernelFactory }

func (c Caller) Kernel() KernelFactory {
	if runtime, ok := c.rt.(KernelRuntime); ok {
		return runtime.Kernel()
	}
	return NativeKernel{}
}

// NativeKernel delegates to the sole reference algorithm, including receive
// ordering. These adapters add no second implementation of contract semantics.
type NativeKernel struct{}
type nativeChanges struct{ *kernel.ChangeLog }

func (n nativeChanges) ForkLog() ChangeLog                   { return nativeChanges{n.Fork()} }
func (n nativeChanges) SetFacts(f func(string, string) bool) { n.Facts = f }
func (NativeKernel) Schemas(refs []*pb.SchemaRef) SchemaRegistry {
	return kernel.NewSchemaRegistry(refs, nil)
}
func (NativeKernel) Changes(s SchemaRegistry) ChangeLog         { return nativeChanges{kernel.NewChangeLog(s)} }
func (NativeKernel) Authorities(id string) Authorities          { return kernel.NewAuthorities(id) }
func (NativeKernel) Works() Works                               { return kernel.NewWorks() }
func (NativeKernel) Connectors() Connectors                     { return kernel.NewConnectors() }
func (NativeKernel) Identity(entities []*pb.EntityRef) Identity { return kernel.NewIdentity(entities) }
func (NativeKernel) Receive(log ChangeLog, authorities Authorities, policy kernel.Policy, caller kernel.Caller, s *pb.Submission, at time.Time, domain func() *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
	r := kernel.Receiver{Changes: log, Authorities: authorities, Policy: policy}
	return r.Receive(caller, s, at, domain)
}
