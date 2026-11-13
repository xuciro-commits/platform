package host

import (
	"platformserver/kernel/records"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) enterpriseHistoricalReferencePaths(store *records.Store) map[string][][]string {
	return records.EnterpriseHistoricalPaths(store, build.ObjectType, build.ID+".", build.TypeOf, records.CandidateRetainedBodies(t.releases.Candidates()))
}
