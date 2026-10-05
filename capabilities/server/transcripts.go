package platformserver

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/platform"
)

// Transcript is one model call in full: what was sent and what came back
// (ADR-0022 D8, ADR-0021 D4). It may hold personal data, so it stays outside
// the journal, for as many days as the agent app's setting keeps it.
type Transcript struct {
	Tenant  string          `json:"-"`
	At      time.Time       `json:"at"`
	Member  string          `json:"member"`
	Model   string          `json:"model"`
	Run     string          `json:"run,omitempty"` // the agent run or evaluation it was for
	Request json.RawMessage `json:"request"`
	Answer  json.RawMessage `json:"answer"`
	Outcome string          `json:"outcome"`
}

const (
	SettingTranscriptDays = "transcript-days"
	transcriptsInMemory   = 500
)

var purgeAll = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// transcriptDays is how many days transcripts are kept, and whether any are
// kept at all: 0 or less keeps none, so the setting is a switch, not only a
// period (ADR-0050 D5, review AI-04).
func (t *Tenant) transcriptDays() int {
	days, err := strconv.Atoi(t.setting(t.automation(AgentApp, false), SettingTranscriptDays))
	if err != nil {
		return 0
	}
	return days
}

// transcribe keeps a call's transcript in the Store, or in memory without one.
// When the tenant keeps none, nothing is written — a transcript holds personal
// data, and a setting of 0 must not quietly keep it forever.
func (t *Tenant) transcribe(x Transcript) {
	if t.transcriptDays() <= 0 {
		return
	}
	x.Tenant = t.ID
	if t.Store != nil {
		t.Store.SaveTranscript(x)
		return
	}
	t.derivedMu.Lock()
	defer t.derivedMu.Unlock()
	t.transcripts = append(t.transcripts, x)
	if len(t.transcripts) > transcriptsInMemory {
		t.transcripts = t.transcripts[len(t.transcripts)-transcriptsInMemory:]
	}
}

// Transcripts are a run's model calls, newest first (every call's, for no run).
func (t *Tenant) Transcripts(run string, limit int) []Transcript {
	if t.Store != nil {
		return t.Store.Transcripts(t.ID, run, limit)
	}
	t.derivedMu.Lock()
	defer t.derivedMu.Unlock()
	out := []Transcript{}
	for _, x := range slices.Backward(t.transcripts) {
		if (run == "" || x.Run == run) && len(out) < limit {
			out = append(out, x)
		}
	}
	return out
}

// PurgeTranscripts forgets transcripts older than the setting keeps them, and
// every one of them when it keeps none: lowering the period, or setting it to
// zero, deletes what is over it on the next round (review AI-04).
func (t *Tenant) PurgeTranscripts(now time.Time) {
	if t.agents == nil {
		return
	}
	days := t.transcriptDays()
	if t.Store != nil {
		// A store's purge is "everything before this moment"; keeping none
		// means forgetting every one, so the moment is past every record.
		if days <= 0 {
			t.Store.PurgeTranscripts(t.ID, purgeAll)
			return
		}
		t.Store.PurgeTranscripts(t.ID, now.AddDate(0, 0, -days))
		return
	}
	t.derivedMu.Lock()
	defer t.derivedMu.Unlock()
	if days <= 0 {
		t.transcripts = nil
		return
	}
	before := now.AddDate(0, 0, -days)
	t.transcripts = slices.DeleteFunc(t.transcripts, func(x Transcript) bool { return x.At.Before(before) })
}

// purgeAll is a moment after any transcript: forgetting every one is a purge
// with this cutoff.

// TranscriptsFor are the model calls of run (every run's, for no run) as m may
// read them: an administrator of the agent or AI app reads a call only where
// they may also read the records that run read, because a prompt carries them
// (#130). It is how the host serves the transcripts; Transcripts itself is the
// store's own read, for the platform.
func (t *Tenant) TranscriptsFor(m platform.Member, run string, limit int, now time.Time) ([]Transcript, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if m.Roles[AgentApp] != AgentAdmin && m.Roles[ai.ID] != ai.Admin {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: "only an administrator of the agent or AI app reads transcripts"}
	}
	found := t.Transcripts(run, limit)
	if run == "" {
		// A person's own chat calls are not a run's trace: there are no records
		// to check them against, and the administrator gate above stands
		// (review AI-04).
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

// withheld reports whether a run's trace narrows for m: something the run read,
// cited or acted on is no longer theirs to read (#130).
func (t *Tenant) withheld(m platform.Member, run string, now time.Time) bool {
	if t.agents == nil || run == "" {
		return true
	}
	r, known := platform.Get[AgentRunRecord](t.automation(AgentApp, false), run)
	if !known {
		return true
	}
	return !t.narrower(m, now, false).readable(sourcesOf(r))
}
