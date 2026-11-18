package lineage

import (
	"platformserver/kernel/protocol"
	"time"

	"platformserver/platform"
	dataowner "platformserver/services/data"
)

type Host interface {
	Admits(platform.Member) *platform.Error
	RecordGeneration() uint64
	LineageState() (Input, []protocol.Resource, uint64)
	LineageResources() []protocol.Resource
	BranchRevision(m platform.Member, id string, now time.Time) (uint32, *platform.Error)
	BranchView(m platform.Member, id string, now time.Time) (Host, *platform.Error)
	ProjectDelegated(platform.Member) bool
	ResourceRole(m platform.Member, project string) string
	ResourceReadable(m platform.Member, typ, id string) bool
	DataOwner() *dataowner.Owner
	LineageCache() *Cache[Host]
}

type Reader struct{ host Host }

func NewReader(host Host) Reader { return Reader{host: host} }
