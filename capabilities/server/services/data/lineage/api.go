package lineage

import (
	"time"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

type Host interface {
	Admits(platform.Member) *platform.Error
	RecordGeneration() uint64
	LineageState() (Input, []build.Resource, uint64)
	LineageResources() []build.Resource
	ReadBranch(m platform.Member, id string, now time.Time) (build.Branch, *platform.Error)
	BranchView(m platform.Member, id string, now time.Time) (Host, *platform.Error)
	ResourceRole(m platform.Member, project string) string
	ResourceReadable(m platform.Member, typ, id string) bool
	DataOwner() *dataowner.Owner
	LineageCache() *Cache[Host]
}

type Reader struct{ host Host }

func NewReader(host Host) Reader { return Reader{host: host} }
