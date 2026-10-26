package host

import (
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
)

// CompositionReady runs only after every application and its declarations
// have been installed successfully. Attach supplies the host earlier; Ready
// must not publish access configuration while composition is incomplete.
type CompositionReady interface {
	Ready() error
}

// IdentityDirectory owns the distinction between an authenticated subject and
// a durable member identity. Consumers never inspect its maps or take its lock.
// The private accepted-state owner implements this same role for staged reads.
type IdentityDirectory interface {
	platform.App
	MemberByID(id string) (platform.Member, bool)
	MemberBySubject(subject string) (platform.Member, bool)
	Holding(app, role string) []string
	AgentMembers() []platform.Member
	UnitGrantUsage(unit, until string) []UnitGrantUsage
	Language(member string) string
}

// UnitGrantUsage names a member whose current or retained grants use a unit.
type UnitGrantUsage struct {
	Member  string
	Current bool
}

// AppCatalog is the compiled declaration set used by governance and agents.
// Reading it does not grant access to application records or change ownership.
type AppCatalog interface {
	Apps() []platform.App
	App(id string) platform.App
	AppIDs() []string                      // cached: governance reads it per request
	Manifest(app string) platform.Manifest // cached: governance reads it per request
}

// PackageCleanupOwner projects the installed generation and accepted technical
// receipts; transport cleanup never receives the owner's mutable maps.
type PackageCleanupOwner interface {
	PackageCleanupState(string) (state, generation string, proofs map[string]string)
	RetainedPackageRuntime(string) (bool, error)
}

// ConsoleHost supplies only tenant governance projections and artifact storage.
// Member/session/access decisions remain in the identity owner.
type ConsoleHost interface {
	AppCatalog
	CodeReleaseAssets() ([]platform.ReleaseAsset, error)
	ConsoleSetting(name string) string
	Languages() []string
	TenantSuspended() bool
	GovernanceRead(platform.Caller, string) (any, *kernel.Error)
	ConnectorHeartbeat(platform.Caller, time.Time) *kernel.Error
	PutPackageArtifact(key string, raw []byte) error
	GetPackageArtifact(key string) ([]byte, error)
	BaseModules() []platform.CapabilityModule
	PackageBlockers(string, map[string]string) ([]string, bool)
}

type ConsoleAttached interface{ AttachConsole(ConsoleHost) }
