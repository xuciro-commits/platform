package host

import (
	"encoding/json"
	"platformkernel/kernel"
	"platformserver/platform"
	"time"
)

// BooksChecks validates accepted-effect preparations through the actual books
// owner; the host neither inspects its private chart nor recreates its policy.
type BooksChecks interface {
	CheckPosting(platform.Caller, platform.Journal) *kernel.Error
	CheckReversal(platform.Caller, string, json.RawMessage, time.Time) *kernel.Error
}
