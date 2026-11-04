package objects

import (
	"platformserver/platform"
	"time"
)

func businessQuery(info platform.EntityInfo, q platform.Query, member platform.Member, at time.Time) platform.Query {
	if info.Versioning == nil || q.BusinessTime != nil && (q.BusinessTime.All || q.BusinessTime.At != "") {
		return q
	}
	field, _ := info.Field(info.Versioning.ValidFrom)
	point := member.Today(at)
	if field.Type == "datetime" {
		point = at.UTC().Format(time.RFC3339Nano)
	}
	q.BusinessTime = &platform.BusinessTime{At: point}
	return q
}

func BusinessQuery(info platform.EntityInfo, q platform.Query, member platform.Member, at time.Time) platform.Query {
	return businessQuery(info, q, member, at)
}
