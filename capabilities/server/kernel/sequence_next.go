package kernel

import (
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (s *Sequences) Next(rec *pb.ChangeRecord, appID string, app platform.App, sequence string, date time.Time) (string, *kernel.Error) {
	if rec.GetChangeId() == "" {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if app == nil {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	m := app.Manifest()
	i := slices.IndexFunc(m.Sequences, func(s platform.Sequence) bool { return s.Name == sequence })
	if i < 0 {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	seq, year := m.Sequences[i], 0
	if seq.Yearly {
		year = date.Year()
	}
	return seq.Format(date.Year(), s.Take(fmt.Sprintf("%s/%s/%d", appID, sequence, year))), nil
}
