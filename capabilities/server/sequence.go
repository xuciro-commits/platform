package platformserver

import (
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Number sequences (ADR-0024 D3): the counters are the host's, keyed by app,
// sequence and year, taken only in accepted decisions and rebuilt by replay;
// snapshots keep them.

// Next is the next number of c's app's sequence for the accepted decision r.
func (r runtime) Next(c platform.Caller, rec *pb.ChangeRecord, sequence string, date time.Time) (string, *kernel.Error) {
	t := r.t
	if rec.GetChangeId() == "" || t.probing {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	app := t.app(c.App)
	if app == nil {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	m := app.Manifest()
	i := slices.IndexFunc(m.Sequences, func(s platform.Sequence) bool { return s.Name == sequence })
	if i < 0 {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	s, year := m.Sequences[i], 0
	if s.Yearly {
		year = date.Year()
	}
	key := fmt.Sprintf("%s/%s/%d", c.App, sequence, year)
	t.seqMu.Lock()
	defer t.seqMu.Unlock()
	t.sequences[key]++
	return s.Format(date.Year(), t.sequences[key]), nil
}
