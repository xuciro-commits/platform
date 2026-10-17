package platformserver

import (
	"time"

	"platformkernel/kernel"
	enterpriseapi "platformserver/apps/enterprise"
)

// EnterpriseQueries is the typed member read capability, separate from the
// directory's private model mutation and metamodel storage.
type EnterpriseQueries interface {
	Query(stereotypes []string, kind, words, alive string, limit int, now time.Time) (enterpriseapi.EnterpriseQueryResult, *kernel.Error)
	Resolve(ids []string, now time.Time) (enterpriseapi.EnterpriseQueryResult, *kernel.Error)
}
