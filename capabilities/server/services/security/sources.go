package security

import (
	"reflect"
	"time"

	"platformserver/kernel/records"
	"platformserver/platform"
)

type DerivationSource interface {
	Sources(reflect.Value, platform.DerivationInfo) []string
}

type Sources interface {
	Tenant() string
	Store() *records.Store
	Now() time.Time
	Engine() platform.AuthorizationEngine
	LookupMember(string) (platform.Member, bool)
	DeclaredPrincipal(string) bool
	App(string) platform.Service
	ActionOwner(string) (platform.Service, bool)
	OwnerActive(string) bool
	AgentSuspended(string) bool
	NamedReadAllowed(platform.Member, string) bool
	InputGranted(platform.Member, string, string) bool
	KnowledgeReadable(*records.Store, platform.Member, string, string) bool
	Directory() platform.Directory
	DerivationSources(*records.Store, platform.Member, time.Time, bool) DerivationSource
	ProjectRole(platform.Member, string) string
	ProjectAccess(*records.Store, platform.Member, string, string, string, bool) (bool, bool)
	Markings(*records.Store, bool) RecordMarkings
}
