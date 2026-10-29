package platformserver

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/studio/build"
	"reflect"
	"slices"
	"strings"
	"time"
)

// RecordView is one record with its history, newest first, the records of
// the app's other types that refer to it, and the processes about it the
// member may read.
type RecordView struct {
	Effective   *bool                  `json:"effective,omitempty"`
	Values      map[string]any         `json:"values,omitempty"`
	ValueErrors map[string]string      `json:"valueErrors,omitempty"`
	Record      any                    `json:"record"`
	History     []records.RecordChange `json:"history"`
	Related     []Related              `json:"related"`
	Processes   []any                  `json:"processes"`
	Approvals   []any                  `json:"approvals"` // requests to move it, newest first (F-38)
	Tasks       []any                  `json:"tasks"`     // open tasks about it the member may take, to answer from its page
	Files       []any                  `json:"files"`     // attached to it
	Comments    []any                  `json:"comments"`  // on it, oldest first
	Following   bool                   `json:"following"` // the member follows it
	Linked      []Related              `json:"linked"`    // records of any app linked to it (the relations app), by type, that the member may read
	Activity    []any                  `json:"activity"`  // what apps told about it through protocols (platform.RelationNote), oldest first
	Deliveries  []RecordDelivery       `json:"deliveries,omitempty"`
}

// RecordDelivery exposes only the owner's public outcome, never a request,
// answer, endpoint or diagnostic. A financial receipt keeps its own read gate.
type RecordDelivery struct {
	ID      string    `json:"id"`
	Event   string    `json:"event"`
	State   string    `json:"state"`
	At      time.Time `json:"at"`
	Message string    `json:"message,omitempty"`
	Receipt string    `json:"receipt,omitempty"`
}

type Related struct {
	Type  string `json:"type"`
	Field string `json:"field"`
	// Relation is the reference field's declared inverse name, when it has one.
	Relation string `json:"relation,omitempty"`
	Title    string `json:"title"`
	Records  []any  `json:"records"`
	Total    int    `json:"total"`
}

func (t *Tenant) recordOf(m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	m = t.authorizationMember(m)
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	if err := t.admits(m); err != nil {
		return RecordView{}, err
	}
	s := t.records
	s.Lock()
	et := s.Type(typ)
	s.Unlock()
	if et == nil {
		return RecordView{}, notFound
	}
	visible, err := t.visible(m, et, now)
	if err != nil {
		return RecordView{}, err
	}
	var r *records.Row
	var readable bool
	func() {
		s.Lock()
		defer s.Unlock()
		r = et.Rows().Get(id)
		readable = r != nil && (visible == nil || visible(r.Value))
	}()
	if !readable {
		return RecordView{}, notFound
	}
	seen, hidden := viewOf(m, et)
	record := records.Mask(et.Info(), r.Value, hidden)
	if et.Info().HasReadSources() || len(et.Info().VersionPins) > 0 {
		narrowed, _ := t.narrower(m, now, false).derive(et, reflect.ValueOf(record))
		record = narrowed.Interface()
	}
	values, valueErrors := propertyValues(seen.Info(), record)
	view := RecordView{Values: values, ValueErrors: valueErrors, Record: record, History: []records.RecordChange{}, Related: []Related{}, Processes: []any{}, Approvals: []any{}, Tasks: []any{}, Files: []any{}, Comments: []any{}, Linked: []Related{}, Activity: []any{}}
	view.Deliveries = t.recordDeliveries(m, typ+"/"+id, now)
	if versioning := et.Info().Versioning; versioning != nil {
		field, _ := et.Info().Field(versioning.ValidFrom)
		clock := businessQuery(et.Info(), platform.Query{}, m, now).BusinessTime.At
		active, _ := platform.EffectiveAt(field.Type, records.BusinessField(et.Info(), r.Value, versioning.ValidFrom), records.BusinessField(et.Info(), r.Value, versioning.ValidUntil), clock)
		view.Effective = &active
	}
	if c := t.app(platform.RelationsApp); c != nil && typ != platform.CommentType && typ != platform.FollowType {
		about, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
		if page, err := t.recordsRead(m, platform.CommentType, platform.Query{Domain: about, Sort: []string{"created"}, Limit: 200}, now); err == nil {
			view.Comments = t.narrowed(m, page.Records, now, false).([]any)
		}
		if f, ok := platform.Get[platform.RecordFollow](t.automation(platform.RelationsApp, false), platform.FollowID(m.ID, typ+"/"+id)); ok && !f.Archived {
			view.Following = true
		}
		view.Linked, view.Activity = t.linked(m, c, typ+"/"+id, now)
	}
	t.readPersonal(m, seen, []string{id}, now)
	if typ != platform.FileType && t.app(platform.FileApp) != nil {
		domain, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
		if page, err := t.recordsRead(m, platform.FileType, platform.Query{Domain: domain, Sort: []string{"id"}, Limit: 100}, now); err == nil {
			view.Files = page.Records
		}
	}
	if t.procs != nil {
		domain, _ := json.Marshal([]any{[]any{"subject", "=", typ + "/" + id}})
		if page, err := t.recordsRead(m, platform.FlowInstanceType, platform.Query{Domain: domain, Sort: []string{"-id"}, Limit: 20, Archived: true}, now); err == nil {
			view.Processes = page.Records
		}
	}
	if typ != platform.WorkTaskType && t.app(platform.WorkApp) != nil {
		domain, _ := json.Marshal([]any{[]any{"ref", "=", typ + "/" + id}, []any{"state", "=", "open"}})
		if page, err := t.recordsRead(m, platform.WorkTaskType, platform.Query{Domain: domain, Sort: []string{"due"}, Limit: 20}, now); err == nil {
			view.Tasks = page.Records
		}
	}
	if typ != platform.WorkApprovalType && t.app(platform.WorkApp) != nil {
		domain, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
		if page, err := t.recordsRead(m, platform.WorkApprovalType, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 20}, now); err == nil {
			view.Approvals = page.Records
		}
	}
	history := records.MaskHistory(records.CopyHistory(r.RecordHistory()), hidden)
	for i := len(history) - 1; i >= 0; i-- {
		view.History = append(view.History, history[i])
	}
	s.Lock()
	var referring []*records.Type
	for _, other := range s.Types() {
		// The same app's referring types, and any type whose reference names an
		// inverse relation: those are declared to be seen from here (21b). The
		// member's own read of each type still decides what is listed.
		named := slices.ContainsFunc(other.Info().Fields, func(f platform.FieldInfo) bool { return f.Ref == typ && f.Inverse != "" })
		if other.Info().App == et.Info().App || named {
			referring = append(referring, other)
		}
	}
	s.Unlock()
	slices.SortFunc(referring, func(a, b *records.Type) int { return strings.Compare(a.Info().Type, b.Info().Type) })
	for _, other := range referring {
		for _, f := range other.Info().Fields {
			if f.Ref != typ {
				continue
			}
			domain, _ := json.Marshal([]any{[]any{f.Name, "=", id}})
			page, err := t.recordsRead(m, other.Info().Type, platform.Query{Domain: domain, Sort: []string{"-id"}, Limit: 20}, now)
			if err != nil {
				continue // the member may not read that type
			}
			if other.Info().App != et.Info().App && f.Inverse == "" {
				continue // another app's reference without a declared relation stays private to it
			}
			view.Related = append(view.Related, Related{Type: other.Info().Type, Field: f.Name, Relation: f.Inverse,
				Title: other.Info().Plural, Records: page.Records, Total: page.Total})
		}
	}
	return view, nil
}

func (t *Tenant) recordDeliveries(m platform.Member, target string, now time.Time) []RecordDelivery {
	t.opsMu.Lock()
	var effects []platform.Effect
	for i := len(t.outbound) - 1; i >= 0; i-- {
		if t.outbound[i].Target == target {
			effects = append(effects, t.outbound[i].Effect)
		}
	}
	t.opsMu.Unlock()
	out := make([]RecordDelivery, 0, len(effects))
	for _, effect := range effects {
		delivery := RecordDelivery{ID: effect.ID, Event: effect.Event, State: effect.State, At: effect.At, Message: effect.PublicMessage}
		if effect.Endpoint == build.BooksEndpoint && effect.State == "delivered" {
			var journal platform.Journal
			if json.Unmarshal([]byte(effect.Body), &journal) == nil && journal.ID != "" {
				domain, _ := json.Marshal([]any{[]any{"id", "=", journal.ID}})
				if page, err := t.recordsRead(m, platform.JournalType, platform.Query{Domain: domain, Limit: 1}, now); err == nil && len(page.Records) == 1 {
					delivery.Receipt = platform.JournalType + "/" + journal.ID
				}
			}
		}
		out = append(out, delivery)
	}
	return out
}

// linked are the records linked to entity that m may read, grouped by type in
// the order they were linked, and the protocol events told about it: what a
// record page shows of other apps without the app composing it (#129). A link
// to what is not a record of this tenant is left out.
func (t *Tenant) linked(m platform.Member, rel platform.App, entity string, now time.Time) ([]Related, []any) {
	c := t.caller(m, rel, false)
	out, activity := []Related{}, []any{}
	for _, e := range c.Links(entity) {
		typ, id, _ := strings.Cut(e, "/")
		domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
		page, err := t.recordsRead(m, typ, platform.Query{Domain: domain, Limit: 1, Archived: true}, now)
		if err != nil || len(page.Records) == 0 {
			continue
		}
		i := slices.IndexFunc(out, func(r Related) bool { return r.Type == typ })
		if i < 0 {
			t.records.Lock()
			plural := t.records.Type(typ).Info().Plural
			t.records.Unlock()
			out = append(out, Related{Type: typ, Field: "link", Title: plural, Records: []any{}})
			i = len(out) - 1
		}
		out[i].Records = append(out[i].Records, page.Records[0])
		out[i].Total++
	}
	if notes, err := platform.CallRead(rel, c, "timeline"); err == nil {
		for _, n := range notes.([]platform.RelationNote) {
			if n.Entity == entity {
				activity = append(activity, n)
			}
		}
	}
	return out, activity
}

// PersonalRead is one read of personal data: who read which
// records' personal fields, and when. Kept outside the journal, like transcripts.

// readPersonal notes that m read the personal fields view shows of records ids.
func (t *Tenant) readPersonal(m platform.Member, view *records.Type, ids []string, now time.Time) {
	var fields []string
	for _, f := range view.Info().Fields {
		if f.Personal != "" {
			fields = append(fields, f.Name)
		}
	}
	if len(fields) == 0 || len(ids) == 0 || strings.HasPrefix(m.ID, "app:") {
		return
	}
	t.audit.ReadPersonal(platform.PersonalRead{At: now, Member: m.ID, Type: view.Info().Type, IDs: ids, Fields: fields})
}

// PersonalReads are the latest reads of personal data, newest first.
func (t *Tenant) PersonalReads() []platform.PersonalRead { return t.audit.PersonalReads() }
