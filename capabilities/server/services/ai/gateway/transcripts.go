package aigateway

import (
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	persistence "platformserver/kernel/store"
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

var PurgeAll = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

func TranscriptDays(setting func(string) string) int {
	days, err := strconv.Atoi(setting(platform.SettingTranscriptDays))
	if err != nil {
		return 0
	}
	return days
}

type Transcripts struct {
	Tenant   string
	Days     int
	Enabled  bool
	Store    persistence.Store
	Admits   func(platform.Member) *kernel.Error
	Withheld func(string) bool
}

func (t Transcripts) Save(x persistence.Transcript) {
	if t.Days <= 0 {
		return
	}
	x.Tenant = t.Tenant
	t.Store.SaveTranscript(x)
}

func (t Transcripts) List(run string, limit int) []persistence.Transcript {
	return t.Store.Transcripts(t.Tenant, run, limit)
}

func (t Transcripts) Purge(now time.Time) {
	if !t.Enabled {
		return
	}
	if t.Days <= 0 {
		t.Store.PurgeTranscripts(t.Tenant, PurgeAll)
	} else {
		t.Store.PurgeTranscripts(t.Tenant, now.AddDate(0, 0, -t.Days))
	}
}

func (t Transcripts) For(m platform.Member, run string, limit int) ([]persistence.Transcript, *kernel.Error) {
	return TranscriptReader[persistence.Transcript]{Admits: t.Admits, Find: t.List,
		Run: func(x persistence.Transcript) string { return x.Run }, Withheld: t.Withheld}.Read(m, run, limit)
}
