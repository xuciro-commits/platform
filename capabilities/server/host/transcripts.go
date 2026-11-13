package host

import (
	"strconv"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/ai/gateway"
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
	t.Log().SaveTranscript(x)
}

func (t *Tenant) Transcripts(run string, limit int) []Transcript {
	return t.Log().Transcripts(t.ID, run, limit)
}

func (t *Tenant) PurgeTranscripts(now time.Time) {
	if t.agents == nil {
		return
	}
	if days := t.transcriptDays(); days <= 0 {
		t.Log().PurgeTranscripts(t.ID, purgeAll)
	} else {
		t.Log().PurgeTranscripts(t.ID, now.AddDate(0, 0, -days))
	}
}

func (t *Tenant) TranscriptsFor(m platform.Member, run string, limit int, now time.Time) ([]Transcript, *kernel.Error) {
	return aigateway.TranscriptReader[Transcript]{Admits: t.Admits, Find: t.Transcripts, Run: func(x Transcript) string { return x.Run },
		Withheld: func(run string) bool { return t.withheld(m, run, now) }}.Read(m, run, limit)
}

func (t *Tenant) withheld(m platform.Member, run string, now time.Time) bool {
	if t.agents == nil || run == "" {
		return true
	}
	r, known := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), run)
	if !known {
		return true
	}
	return !t.narrower(m, now, false).Readable(r.SourceRefs())
}
