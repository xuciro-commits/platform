package platformserver

import (
	"encoding/json"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/platform"
	"time"
)

// IdentitySessions is the authentication consumer of governance-owned state.
type IdentitySessions interface {
	platform.App
	MemberBySubject(string) (platform.Member, bool)
	MemberByToken(string, time.Time) (platform.Member, bool)
	NoticeSession(string, string, string, string, time.Time) bool
	SeenMember(string, time.Time)
	JoinsSubject(string) bool
	JoinableSubject(string) bool
	JoinID(string) string
	SecondFactorRequired() bool
	Minted(string, string, time.Time) (string, bool)
	EndOtherSessions(string, string, time.Time) int
	Sessions(string, string, time.Time) []Session
	Tokens(string, time.Time) []TokenView
}

// IdentityProfile exposes current profile projections without directory state.
type IdentityProfile interface {
	platform.App
	Account(string) Account
	TenantRecord() TenantRecord
	Location(string) *time.Location
	Language(string) string
	Reach(string) Reach
}
type HostIdentities interface {
	platform.App
	Identities() []Identity
}

var _ IdentitySessions = (*Console)(nil)
var _ IdentityProfile = (*Console)(nil)
var _ HostIdentities = (*Console)(nil)

// Recovery and package composition are separate from sign-in consumers.
type IdentityRecovery interface{ CloseTokenSecretWindows() }
type PackageCatalog interface{ BindPackageIndex(*PackageIndex) }
type ProtocolPackageCatalog interface {
	ProtocolPackages() []platform.DriverPackage
	ResolveDriver(id, version, digest, profile string) (platform.DriverRuntime, error)
}
type IdentityCounts interface{ MemberCount() int }
type ProjectAccess interface {
	EditsProjectAsset(string, ProjectAsset) bool
}

type HistoricalIdentity interface {
	LegacyCoreSeedPredecessor(resultformat.State, json.RawMessage) bool
	LegacyConsolePredecessor(resultformat.State, json.RawMessage) bool
}
