package organization

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func noModel() *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This tenant has no enterprise model")
}

func QueryRead(app platform.EnterpriseQueries, values url.Values, now time.Time) (platform.EnterpriseQueryResult, *kernel.Error) {
	limit := 200
	if raw := values.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000 {
			return platform.EnterpriseQueryResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "limit is a number between 1 and 1000")
		}
		limit = n
	}
	if app == nil {
		return platform.EnterpriseQueryResult{}, noModel()
	}
	return app.Query(strings.Split(values.Get("stereotype"), ","), values.Get("kind"), values.Get("q"), values.Get("alive"), limit, now)
}

func ResolveRead(app platform.EnterpriseQueries, values url.Values, now time.Time) (platform.EnterpriseQueryResult, *kernel.Error) {
	if app == nil {
		return platform.EnterpriseQueryResult{}, noModel()
	}
	return app.Resolve(strings.Split(values.Get("ids"), ","), now)
}
