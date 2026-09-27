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

// transcribe keeps a call's transcript in the Store, or in memory without one.
func (t *Tenant) transcribe(x Transcript) {
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

// PurgeTranscripts forgets transcripts older than the setting keeps them.
func (t *Tenant) PurgeTranscripts(now time.Time) {
	if t.agents == nil {
		return
	}
	days, err := strconv.Atoi(t.setting(t.automation(AgentApp, false), SettingTranscriptDays))
	if err != nil || days <= 0 {
		return
	}
	before := now.AddDate(0, 0, -days)
	if t.Store != nil {
		t.Store.PurgeTranscripts(t.ID, before)
		return
	}
	t.derivedMu.Lock()
	defer t.derivedMu.Unlock()
	t.transcripts = slices.DeleteFunc(t.transcripts, func(x Transcript) bool { return x.At.Before(before) })
}

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
