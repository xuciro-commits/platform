package host

import (
	"time"

	"platformserver/platform"
	"platformserver/services/objects"
)

func businessQuery(info platform.EntityInfo, q platform.Query, member platform.Member, at time.Time) platform.Query {
	return objects.BusinessQuery(info, q, member, at)
}
