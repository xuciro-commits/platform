package aigateway

import (
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

type TranscriptReader[T any] struct {
	Admits   func(platform.Member) *platform.Error
	Find     func(run string, limit int) []T
	Run      func(T) string
	Withheld func(run string) bool
}

func (r TranscriptReader[T]) Read(m platform.Member, run string, limit int) ([]T, *platform.Error) {
	if err := r.Admits(m); err != nil {
		return nil, err
	}
	if m.Roles[runtimeprotocol.AgentApp] != runtimeprotocol.AgentAdmin && m.Roles[runtimeprotocol.AIApp] != runtimeprotocol.AIAdmin {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: "only an administrator of the agent or AI app reads transcripts"}
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
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: "this run read records you may not read"}
	}
	return out, nil
}

var PurgeAll = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

func TranscriptDays(setting func(string) string) int {
	days, err := strconv.Atoi(setting(runtimeprotocol.SettingTranscriptDays))
	if err != nil {
		return 0
	}
	return days
}

type Transcripts struct {
	Tenant   string
	Days     int
	Enabled  bool
	Store    TranscriptStore
	Admits   func(platform.Member) *platform.Error
	Withheld func(string) bool
}

type TranscriptStore interface {
	SaveTranscript(runtimeprotocol.Transcript)
	Transcripts(string, string, int) []runtimeprotocol.Transcript
	PurgeTranscripts(string, time.Time)
}

func (t Transcripts) Save(x runtimeprotocol.Transcript) {
	if t.Days <= 0 {
		return
	}
	x.Tenant = t.Tenant
	t.Store.SaveTranscript(x)
}

func (t Transcripts) List(run string, limit int) []runtimeprotocol.Transcript {
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

func (t Transcripts) For(m platform.Member, run string, limit int) ([]runtimeprotocol.Transcript, *platform.Error) {
	return TranscriptReader[runtimeprotocol.Transcript]{Admits: t.Admits, Find: t.List,
		Run: func(x runtimeprotocol.Transcript) string { return x.Run }, Withheld: t.Withheld}.Read(m, run, limit)
}
