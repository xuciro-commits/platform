package protocol

import "strings"

const ContinuationSuffix = "#next"

func Continues(idempotencyKey string) bool {
	return strings.HasSuffix(idempotencyKey, ContinuationSuffix)
}
