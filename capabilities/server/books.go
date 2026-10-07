package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

// The books (ADR-0076): a builder action that declares a Journal makes the
// host land one balanced entry in core after the decision is accepted. It is
// an ADR-0014 effect on the "core:books" endpoint - in order, retried while
// no period is open for it, delivered once per decision - so the general
// ledger lags the business by a tick and never disagrees with it.

// journalEffects plans the entry of one accepted decision.
func (t *Tenant) journalEffects(e platform.Event) []platform.Effect {
	if e.App != build.ID || t.app(core.ID) == nil {
		return nil
	}
	s := e.Record.GetSubmission()
	typ, verb := s.GetTarget().GetType(), strings.TrimPrefix(s.GetSchema().GetName(), s.GetTarget().GetType()+".")
	c := t.automation(build.ID, false)
	objects, _, _ := platform.Find[build.Object](c, platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Limit: 500})
	for _, o := range objects {
		if build.TypeOf(o.Name) != typ {
			continue
		}
		for _, a := range o.Actions {
			journal := o.EffectiveJournal(a)
			if a.Name != verb || journal == nil {
				continue
			}
			record := map[string]any{}
			if held, ok := t.Held(typ + "/" + s.GetTarget().GetId()); ok {
				raw, _ := json.Marshal(held)
				json.Unmarshal(raw, &record)
			}
			inputs := map[string]any{}
			json.Unmarshal(s.GetPayload(), &inputs)
			at := e.Record.GetRecordedTime().AsTime()
			today := at.UTC().Format(time.DateOnly)
			if who, ok := t.member(s.GetPrincipalId()); ok {
				today = who.Today(at) // the posting date is the decider's day (ADR-0079 §3)
			}
			entry, err := journal.Entry(e.Record.GetChangeId(), record, inputs, s.GetPrincipalId(), at, today)
			if err != nil {
				entry.Text = "not booked: " + err.Error()
			}
			return []platform.Effect{{ID: fmt.Sprintf("%s:%s:%s:books", t.ID, build.ID, e.Record.GetChangeId()), Endpoint: build.BooksEndpoint, Event: "journal/" + o.Name + "." + a.Name,
				App: build.ID, Key: e.Record.GetChangeId(), Target: target(s), At: at, State: "pending", Due: at, Body: build.EntryBody(entry)}}
		}
	}
	return nil
}

// sendBooks lands one entry. A period that is not open yet is a retry - the
// accountant opens it and the queue drains in order; anything else about the
// entry itself is rejected and stays visible in the effect log.
func (t *Tenant) sendBooks(x platform.Effect, now time.Time) platform.Outcome {
	out := platform.Outcome{Effect: x.ID}
	books, ok := t.app(core.ID).(*core.Core)
	if !ok {
		out.Result, out.Detail = "retry", "the books are not installed"
		return out
	}
	var entry core.Journal
	if json.Unmarshal([]byte(x.Body), &entry) != nil || len(entry.Lines) == 0 {
		out.Result, out.Detail = "rejected", "nothing to book"
		return out
	}
	err := books.Post(t.automation(core.ID, false), entry, "books:"+x.Key, now)
	if err != nil {
		out.Detail = err.Code.String() + ": " + err.Message
	}
	switch {
	case err == nil, err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT, err.Code == pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT:
		out.Result = "delivered"
	case strings.Contains(err.Message, "closed") || strings.Contains(err.Message, "No fiscal period"):
		out.Result = "retry"
	default:
		out.Result = "rejected"
	}
	return out
}

// booksEndpoint is the single ordered lane entries go through.
func booksEndpoint() *Endpoint { return &Endpoint{ID: build.BooksEndpoint, Kind: "books"} }
