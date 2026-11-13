package host

import (
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/ai/gateway"
)

func (t *Tenant) transcriptDays() int {
	return aigateway.TranscriptDays(func(name string) string { return t.setting(t.automation(platform.AgentApp, false), name) })
}

func (t *Tenant) transcripts() aigateway.Transcripts {
	return aigateway.Transcripts{Tenant: t.ID, Days: t.transcriptDays(), Enabled: t.agents != nil, Store: t.Log()}
}

func (t *Tenant) transcribe(x Transcript) {
	t.transcripts().Save(x)
}

func (t *Tenant) Transcripts(run string, limit int) []Transcript {
	return t.transcripts().List(run, limit)
}

func (t *Tenant) PurgeTranscripts(now time.Time) {
	t.transcripts().Purge(now)
}

func (t *Tenant) TranscriptsFor(m platform.Member, run string, limit int, now time.Time) ([]Transcript, *kernel.Error) {
	return aigateway.Transcripts{Tenant: t.ID, Days: t.transcriptDays(), Enabled: t.agents != nil,
		Store: t.Log(), Admits: t.Admits, Withheld: func(run string) bool { return t.withheld(m, run, now) }}.For(m, run, limit)
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
