package platformserver

import (
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

var purgeAll = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

func (t *Tenant) transcriptDays() int {
	days, err := strconv.Atoi(t.setting(t.automation(platform.AgentApp, false), platform.SettingTranscriptDays))
	if err != nil {
		return 0
	}
	return days
}

func (t *Tenant) transcribe(x Transcript) {
	if t.transcriptDays() <= 0 {
		return
	}
	x.Tenant = t.ID
	t.store().SaveTranscript(x)
}

func (t *Tenant) Transcripts(run string, limit int) []Transcript {
	return t.store().Transcripts(t.ID, run, limit)
}

func (t *Tenant) PurgeTranscripts(now time.Time) {
	if t.agents == nil {
		return
	}
	if days := t.transcriptDays(); days <= 0 {
		t.store().PurgeTranscripts(t.ID, purgeAll)
	} else {
		t.store().PurgeTranscripts(t.ID, now.AddDate(0, 0, -days))
	}
}

func (t *Tenant) store() Store {
	if t.Store != nil {
		return t.Store
	}
	return &t.memStore
}

func (t *Tenant) TranscriptsFor(m platform.Member, run string, limit int, now time.Time) ([]Transcript, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if m.Roles[platform.AgentApp] != platform.AgentAdmin && m.Roles[platform.AIApp] != platform.AIAdmin {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: "only an administrator of the agent or AI app reads transcripts"}
	}
	found := t.Transcripts(run, limit)
	if run == "" {
		return found, nil
	}
	out := []Transcript{}
	for _, x := range found {
		if !t.withheld(m, x.Run, now) {
			out = append(out, x)
		}
	}
	if run != "" && len(out) == 0 && len(found) > 0 {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: "this run read records you may not read"}
	}
	return out, nil
}

func (t *Tenant) withheld(m platform.Member, run string, now time.Time) bool {
	if t.agents == nil || run == "" {
		return true
	}
	r, known := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), run)
	if !known {
		return true
	}
	return !t.narrower(m, now, false).readable(r.SourceRefs())
}
