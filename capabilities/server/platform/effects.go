package platform

import (
	"encoding/json"
	"time"

	"platformkernel/kernel"
)

// EffectKind declares an effect an app sends to whatever endpoint the tenant
// binds to it (ADR-0014).
type EffectKind struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Irreversible marks what cannot be recalled once received (a posting, a
	// payment, an email to people outside): caused by an agent, it is held until
	// a person approves it (D6).
	Irreversible bool `json:"irreversible,omitempty"`
}

// Effect is one intent for one endpoint and what became of it.
type Effect struct {
	ID       string    `json:"id"` // the idempotency key: <tenant>:<app>:<change id or key>:<endpoint>
	Endpoint string    `json:"endpoint"`
	Event    string    `json:"event"`         // the event or effect kind
	App      string    `json:"app,omitempty"` // the app that emitted it; empty for webhooks
	Key      string    `json:"key,omitempty"` // the app's key for it
	Target   string    `json:"target"`
	At       time.Time `json:"at"`
	State    string    `json:"state"`           // held, pending, retrying, delivered, rejected, failed, discarded
	Agent    string    `json:"agent,omitempty"` // the AI agent that caused a held effect
	Run      string    `json:"run,omitempty"`   // and its run
	Attempts int       `json:"attempts"`
	Last     time.Time `json:"last,omitzero"`
	Due      time.Time `json:"due,omitzero"`
	Error    string    `json:"error,omitempty"`
	Digest   string    `json:"digest,omitempty"` // sha256 of the body last sent
	Body     string    `json:"body,omitempty"`   // kept 30 days for support (D8)
}

// Outcome is what an attempt learned; it is the journal entry of the attempt.
type Outcome struct {
	Effect string          `json:"effect"`
	Result string          `json:"result"` // delivered, rejected, retry
	Detail string          `json:"detail,omitempty"`
	Digest string          `json:"digest,omitempty"`
	Answer json.RawMessage `json:"answer,omitempty"` // the receiver's body, for app effects (up to 64 KiB of JSON)
}

// Emit sends data as an effect of kind (declared in the app's manifest) to every
// endpoint bound to it; key names the effect within the app and kind, so the
// same fact of the business is sent once whatever retries or replays do. It is
// called inside an input, so replay rebuilds the intent and never sends it.
// It returns how many endpoints will receive it. An irreversible kind emitted
// for an agent is held until a person approves it (D6).
func (c Caller) Emit(kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	if c.rt == nil {
		return 0, notFound()
	}
	return c.rt.Emit(c, kind, key, entity, data, now)
}
