package platformserver

import (
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/studio/build"

	"platformserver/platform"
)

func (t *Tenant) legacyJournalEffects(e platform.Event) []platform.Effect {
	if e.App != build.ID || t.app(platform.BooksApp) == nil {
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
			if held, ok := t.held(typ + "/" + s.GetTarget().GetId()); ok {
				raw, _ := json.Marshal(held)
				json.Unmarshal(raw, &record)
			}
			inputs := map[string]any{}
			json.Unmarshal(s.GetPayload(), &inputs)
			at := e.Record.GetRecordedTime().AsTime()
			today := at.UTC().Format(time.DateOnly)
			if who, ok := t.member(s.GetPrincipalId()); ok {
				today = who.Today(at)
			}
			entry, err := journal.Entry(e.Record.GetChangeId(), record, inputs, s.GetPrincipalId(), at, today)
			if err != nil {
				entry["error"] = err.Error()
			}
			return []platform.Effect{{ID: fmt.Sprintf("%s:%s:%s:books", t.ID, build.ID, e.Record.GetChangeId()), Endpoint: build.BooksEndpoint, Event: "journal/" + o.Name + "." + a.Name,
				App: build.ID, Key: e.Record.GetChangeId(), Target: target(s), At: at, State: "pending", Due: at, Body: t.bookBody(entry, typ, s.GetTarget().GetId(), a.Reverses)}}
		}
	}
	return nil
}

func (t *Tenant) sendBooks(x platform.Effect, now time.Time) platform.Outcome {
	out := platform.Outcome{Effect: x.ID}
	app := t.app(platform.BooksApp)
	if app == nil {
		out.Result, out.Detail = "retry", "the books are not installed"
		return out
	}
	var reverse struct {
		ID    string `json:"reverse"`
		Error string `json:"error"`
	}
	json.Unmarshal([]byte(x.Body), &reverse)
	if reverse.Error != "" {
		out.Result, out.Detail = "rejected", reverse.Error
		return out
	}
	var entry platform.Journal
	if json.Unmarshal([]byte(x.Body), &entry) != nil || len(entry.Lines) == 0 && reverse.ID == "" {
		out.Result, out.Detail = "rejected", "nothing to book"
		return out
	}
	values := map[string]any{"date": entry.Date, "text": entry.Text, "currency": entry.Currency, "lines": entry.Lines, "source": entry.Source}
	if entry.ClosedPeriod != "" {
		values["closedPeriod"] = entry.ClosedPeriod
	}
	payload, _ := json.Marshal(values)
	sub := &pb.Submission{TenantId: t.ID, Authority: platform.BooksApp, IdempotencyKey: "books:" + x.Key,
		Target: &pb.EntityRef{Type: platform.JournalType, Id: entry.ID}, Schema: &pb.SchemaRef{Name: platform.JournalType + ".create", Version: 1}, Payload: payload}
	if reverse.ID != "" {
		sub.Target.Id = reverse.ID
		sub.Schema.Name = platform.JournalType + ".reverse"
		sub.Payload, _ = json.Marshal(map[string]any{"date": entry.Date})
	}
	out, err := t.submitBooks(app, sub, now)
	out.Effect = x.ID
	if err != nil {
		out.Detail = err.Code.String() + ": " + err.Message
	}
	return out
}

func booksEndpoint() *Endpoint { return &Endpoint{ID: build.BooksEndpoint, Kind: "books"} }

func (t *Tenant) submitBooks(app platform.App, sub *pb.Submission, now time.Time) (platform.Outcome, *kernel.Error) {
	checks, ok := app.(platform.BooksChecks)
	if !ok {
		return platform.Outcome{Result: "rejected"}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Books owner does not provide posting checks")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return platform.Outcome{Result: "rejected"}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	caller := t.automation(platform.BooksApp, false)
	sub.PrincipalId = caller.ID
	if sub.GetSchema().GetName() == platform.JournalType+".create" {
		if _, exists := platform.Get[platform.Journal](caller, sub.GetTarget().GetId()); !exists {
			var entry platform.Journal
			json.Unmarshal(sub.GetPayload(), &entry)
			if outcome, err := checks.CheckPosting(caller, entry); err != nil {
				return outcome, err
			}
		}
	} else {
		original, exists := platform.Get[platform.Journal](caller, sub.GetTarget().GetId())
		if !exists || original.State != "reversed" {
			if outcome, err := checks.CheckReversal(caller, sub.GetTarget().GetId(), sub.GetPayload(), now); err != nil {
				return outcome, err
			}
		}
	}
	if err := t.submitAutomated(app, caller, sub, now); err != nil {
		return platform.Outcome{Result: "rejected"}, err
	}
	return platform.Outcome{Result: "delivered"}, nil
}

func (t *Tenant) submitAutomated(app platform.App, caller platform.Caller, sub *pb.Submission, now time.Time) *kernel.Error {
	sub.PrincipalId = caller.ID
	_, err := t.submitAccepted(app, caller.Member, sub, now, true)
	return err
}

func (t *Tenant) bookBody(entry map[string]any, typ, id, reverses string) string {
	if reverses == "" {
		return build.EntryBody(entry)
	}
	change := ""
	func() {
		t.records.Lock()
		defer t.records.Unlock()
		if et := t.records.Type(typ); et != nil {
			if row := et.Rows().Get(id); row != nil {
				history := row.RecordHistory()
				for i := len(history) - 1; i >= 0; i-- {
					h := history[i]
					if h.Schema == typ+"."+reverses {
						change = h.Change
						break
					}
				}
			}
		}
	}()
	if change == "" {
		return `{}`
	}
	original := ""
	t.opsMu.Lock()
	for _, intent := range t.outbound {
		if intent.Endpoint != build.BooksEndpoint || intent.Target != typ+"/"+id || intent.Key != change && !strings.HasPrefix(intent.Key, change+":") {
			continue
		}
		var journal platform.Journal
		if json.Unmarshal([]byte(intent.Body), &journal) != nil || journal.ID == "" || len(journal.Lines) == 0 {
			continue
		}
		if original != "" {
			t.opsMu.Unlock()
			return `{"error":"The original action produced multiple journal entries; its reversal is ambiguous"}`
		}
		original = journal.ID
	}
	t.opsMu.Unlock()
	if original == "" {
		original = "jnl-" + change
	}
	raw, _ := json.Marshal(map[string]any{"reverse": original, "date": entry["date"]})
	return string(raw)
}
