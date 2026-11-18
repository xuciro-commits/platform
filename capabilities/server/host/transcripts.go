package host

import (
	"platformserver/kernel/protocol"
	"time"

	"platformserver/platform"
	"platformserver/services/ai/gateway"
)

func (t *Tenant) transcripts() gateway.Transcripts {
	days := gateway.TranscriptDays(func(name string) string { return t.setting(t.automation(protocol.AgentApp, false), name) })
	return gateway.Transcripts{Tenant: t.ID, Days: days, Enabled: t.Agents != nil, Store: t.Log()}
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

func (t *Tenant) TranscriptsFor(m platform.Member, run string, limit int, now time.Time) ([]Transcript, *platform.Error) {
	transcripts := t.transcripts()
	transcripts.Admits, transcripts.Withheld = t.Admits, func(run string) bool { return t.withheld(m, run, now) }
	return transcripts.For(m, run, limit)
}

func (t *Tenant) withheld(m platform.Member, run string, now time.Time) bool {
	if t.Agents == nil || run == "" {
		return true
	}
	r, known := platform.Get[protocol.AgentRunRecord](t.automation(protocol.AgentApp, false), run)
	if !known {
		return true
	}
	return !t.narrower(m, now, false).Readable(r.SourceRefs())
}
