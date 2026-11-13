package aigateway

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type TranscriptReader[T any] struct {
	Admits   func(platform.Member) *kernel.Error
	Find     func(run string, limit int) []T
	Run      func(T) string
	Withheld func(run string) bool
}

func (r TranscriptReader[T]) Read(m platform.Member, run string, limit int) ([]T, *kernel.Error) {
	if err := r.Admits(m); err != nil {
		return nil, err
	}
	if m.Roles[platform.AgentApp] != platform.AgentAdmin && m.Roles[platform.AIApp] != platform.AIAdmin {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: "only an administrator of the agent or AI app reads transcripts"}
	}
	found := r.Find(run, limit)
	if run == "" {
		return found, nil
	}
	out := []T{}
	for _, x := range found {
		if !r.Withheld(r.Run(x)) {
			out = append(out, x)
		}
	}
	if len(out) == 0 && len(found) > 0 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: "this run read records you may not read"}
	}
	return out, nil
}
