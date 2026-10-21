package host

import (
	"time"

	"platformkernel/kernel"
	enterprisemodel "platformserver/internal/enterprise"
	"platformserver/platform"
)

// BlueprintCatalog exposes only the installed Package owner's sealed,
// licensed definition set. Consumers cannot change installation or grants.
type BlueprintCatalog interface {
	InstalledBlueprint(id, version string) ([]platform.AssetRef, []platform.ReleaseAsset, error)
}

// The caller provides the private transport; the attached owner also reads
// current reviewer membership. There is no independent decision or scheduler.
type ConfirmationHost interface {
	platform.ConfirmationTransport
	Holding(string, string) []string
}

type DispatchHost interface {
	platform.DispatchTransport
	Holding(string, string) []string
}

// DriverPackages reads the original Package installation state. It grants no device access.
type DriverPackages interface {
	CheckDriverBinding(platform.DriverBinding) error
}

// EnterpriseQueries is the typed member read capability, separate from the
// directory's private model mutation and metamodel storage.
type EnterpriseQueries interface {
	Query(stereotypes []string, kind, words, alive string, limit int, now time.Time) (enterprisemodel.EnterpriseQueryResult, *kernel.Error)
	Resolve(ids []string, now time.Time) (enterprisemodel.EnterpriseQueryResult, *kernel.Error)
}

type TransactionHost interface {
	CheckTxnCall(platform.TxnCall) error
}
