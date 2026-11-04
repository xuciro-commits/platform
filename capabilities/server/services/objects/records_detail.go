package objects

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

type RecordView struct {
	Effective   *bool                           `json:"effective,omitempty"`
	Values      map[string]any                  `json:"values,omitempty"`
	Formats     map[string]platform.FieldFormat `json:"formats,omitempty"`
	ValueErrors map[string]string               `json:"valueErrors,omitempty"`
	Restricted  []string                        `json:"restricted,omitempty"`
	Record      any                             `json:"record"`
	History     []records.RecordChange          `json:"history,omitempty"`
	Related     []Related                       `json:"related,omitempty"`
	Processes   []any                           `json:"processes,omitempty"`
	Approvals   []any                           `json:"approvals,omitempty"`
	Tasks       []any                           `json:"tasks,omitempty"`
	Files       []any                           `json:"files,omitempty"`
	Comments    []any                           `json:"comments,omitempty"`
	Following   bool                            `json:"following,omitempty"`
	Linked      []Related                       `json:"linked,omitempty"`
	Activity    []any                           `json:"activity,omitempty"`
	Deliveries  []RecordDelivery                `json:"deliveries,omitempty"`
}

const (
	RecordSectionLinks    = "links"
	RecordSectionActivity = "activity"
	RecordSectionFiles    = "files"
	RecordSectionWork     = "work"
	RecordSectionHistory  = "history"
	RecordSectionComments = "comments"
)

type RecordSectionView struct {
	Section    string                 `json:"section"`
	Partial    bool                   `json:"partial,omitempty"`
	History    []records.RecordChange `json:"history,omitempty"`
	Related    []Related              `json:"related,omitempty"`
	Processes  []any                  `json:"processes,omitempty"`
	Approvals  []any                  `json:"approvals,omitempty"`
	Tasks      []any                  `json:"tasks,omitempty"`
	Files      []any                  `json:"files,omitempty"`
	Comments   []any                  `json:"comments,omitempty"`
	Following  bool                   `json:"following,omitempty"`
	Linked     []Related              `json:"linked,omitempty"`
	Activity   []any                  `json:"activity,omitempty"`
	Deliveries []RecordDelivery       `json:"deliveries,omitempty"`
}

type RecordDelivery struct {
	ID      string    `json:"id"`
	Event   string    `json:"event"`
	State   string    `json:"state"`
	At      time.Time `json:"at"`
	Message string    `json:"message,omitempty"`
	Receipt string    `json:"receipt,omitempty"`
}

type Related struct {
	Type     string `json:"type"`
	Field    string `json:"field"`
	Relation string `json:"relation,omitempty"`
	Title    string `json:"title"`
	Records  []any  `json:"records"`
	Total    int    `json:"total"`
}

type recordBase struct {
	view   RecordView
	entity *records.Type
	row    *records.Row
	hidden []platform.FieldInfo
}

func (t *Owner) readRecordBase(m platform.Member, typ, id string, now time.Time) (recordBase, *kernel.Error) {
	m = t.authorizationMember(m)
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	if err := t.admits(m); err != nil {
		return recordBase{}, err
	}
	s := t.Store
	s.Lock()
	et := s.Type(typ)
	s.Unlock()
	if et == nil {
		return recordBase{}, notFound
	}
	visible, err := t.visible(m, et, now)
	if err != nil {
		return recordBase{}, err
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
		return recordBase{}, notFound
	}
	seen, hidden := ViewOf(m, et)
	record := records.Mask(et.Info(), r.Value, hidden)
	if et.Info().HasReadSources() || len(et.Info().VersionPins) > 0 {
		narrowed, _ := t.narrower(m, now, false).Derive(et, reflect.ValueOf(record))
		record = narrowed.Interface()
	}
	values, valueErrors := propertyValues(seen.Info(), record)
	view := RecordView{Values: values, ValueErrors: valueErrors, Formats: et.Formats().Evaluate(record, hidden), Record: record}
	caller := t.caller(m, et.Info().App)
	for _, field := range et.Info().Fields {
		if field.Aside {
			continue
		}
		decision := caller.Authorize(platform.AuthzRequest{Principal: m.ID, Tenant: t.TenantID, Kind: "read", Type: typ, Record: id, Fields: []string{field.Name}}, now)
		if !decision.Allow || len(decision.Withheld) > 0 {
			view.Restricted = append(view.Restricted, field.Name)
		}
	}
	slices.Sort(view.Restricted)
	if versioning := et.Info().Versioning; versioning != nil {
		field, _ := et.Info().Field(versioning.ValidFrom)
		clock := businessQuery(et.Info(), platform.Query{}, m, now).BusinessTime.At
		active, _ := platform.EffectiveAt(field.Type, records.BusinessField(et.Info(), r.Value, versioning.ValidFrom), records.BusinessField(et.Info(), r.Value, versioning.ValidUntil), clock)
		view.Effective = &active
	}
	t.readPersonal(m, seen, []string{id}, now)
	return recordBase{view: view, entity: et, row: r, hidden: hidden}, nil
}

func (t *Owner) recordOf(m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	base, err := t.readRecordBase(m, typ, id, now)
	if err != nil {
		return RecordView{}, err
	}
	return base.view, nil
}

func (t *Owner) recordSection(m platform.Member, typ, id, section string, now time.Time) (RecordSectionView, *kernel.Error) {
	base, err := t.readRecordBase(m, typ, id, now)
	if err != nil {
		return RecordSectionView{}, err
	}
	out := RecordSectionView{Section: section}
	switch section {
	case RecordSectionLinks:
		out.Related, out.Partial = t.recordRelated(m, base, now)
		out.Linked, out.Partial = t.recordLinked(m, typ, id, now, out.Partial)
	case RecordSectionActivity:
		out.Activity, out.Partial = t.recordActivity(m, typ, id, now)
		out.Deliveries = t.recordDeliveries(typ+"/"+id, now)
	case RecordSectionFiles:
		out.Files, out.Partial = t.recordFiles(m, typ, id, now)
	case RecordSectionWork:
		out.Processes, out.Tasks, out.Approvals, out.Partial = t.recordWork(m, typ, id, now)
	case RecordSectionHistory:
		out.History = t.recordHistory(base)
	case RecordSectionComments:
		out.Comments, out.Following, out.Partial = t.recordComments(m, typ, id, now)
	default:
		return RecordSectionView{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return out, nil
}

func (t *Owner) recordDetail(m platform.Member, typ, id string, now time.Time, sections ...string) (RecordView, *kernel.Error) {
	base, err := t.readRecordBase(m, typ, id, now)
	if err != nil {
		return RecordView{}, err
	}
	for _, section := range sections {
		switch section {
		case RecordSectionLinks:
			base.view.Related, _ = t.recordRelated(m, base, now)
		case RecordSectionHistory:
			base.view.History = t.recordHistory(base)
		}
	}
	return base.view, nil
}

func (t *Owner) recordHistory(base recordBase) []records.RecordChange {
	history := records.MaskHistory(records.CopyHistory(base.row.RecordHistory()), base.hidden)
	out := make([]records.RecordChange, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		out = append(out, history[i])
	}
	return out
}

func (t *Owner) recordRelated(m platform.Member, base recordBase, now time.Time) ([]Related, bool) {
	s := t.Store
	et := base.entity
	s.Lock()
	var referring []*records.Type
	for _, other := range s.Types() {
		named := slices.ContainsFunc(other.Info().Fields, func(f platform.FieldInfo) bool { return f.Ref == et.Info().Type && f.Inverse != "" })
		if other.Info().App == et.Info().App || named {
			referring = append(referring, other)
		}
	}
	s.Unlock()
	slices.SortFunc(referring, func(a, b *records.Type) int { return strings.Compare(a.Info().Type, b.Info().Type) })
	out, partial := []Related{}, false
	for _, other := range referring {
		for _, f := range other.Info().Fields {
			if f.Ref != et.Info().Type {
				continue
			}
			domain, _ := json.Marshal([]any{[]any{f.Name, "=", records.Header(base.row.Value).ID}})
			page, err := t.recordsRead(m, other.Info().Type, platform.Query{Domain: domain, Sort: []string{"-id"}, Limit: 20}, now)
			if err != nil {
				partial = true
				continue
			}
			if other.Info().App != et.Info().App && f.Inverse == "" {
				continue
			}
			partial = partial || page.Partial
			out = append(out, Related{Type: other.Info().Type, Field: f.Name, Relation: f.Inverse,
				Title: other.Info().Plural, Records: page.Records, Total: page.Total})
		}
	}
	return out, partial
}

func (t *Owner) recordLinked(m platform.Member, typ, id string, now time.Time, partial bool) ([]Related, bool) {
	rel := t.app(platform.RelationsApp)
	if rel == nil || typ == platform.CommentType || typ == platform.FollowType {
		return nil, partial
	}
	out := t.linkRecords(m, rel.Manifest().ID, typ+"/"+id, now)
	return out, partial
}

func (t *Owner) recordActivity(m platform.Member, typ, id string, now time.Time) ([]any, bool) {
	rel := t.app(platform.RelationsApp)
	if rel == nil || typ == platform.CommentType || typ == platform.FollowType {
		return nil, false
	}
	return t.timeline(m, rel.Manifest().ID, typ+"/"+id), false
}

func (t *Owner) recordFiles(m platform.Member, typ, id string, now time.Time) ([]any, bool) {
	if typ == platform.FileType || t.app(platform.FileApp) == nil {
		return nil, false
	}
	domain, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
	page, err := t.recordsRead(m, platform.FileType, platform.Query{Domain: domain, Sort: []string{"id"}, Limit: 100}, now)
	if err != nil {
		return nil, true
	}
	return page.Records, page.Partial
}

func (t *Owner) recordWork(m platform.Member, typ, id string, now time.Time) ([]any, []any, []any, bool) {
	var processes, tasks, approvals []any
	partial := false
	if t.HasProcesses() {
		domain, _ := json.Marshal([]any{[]any{"subject", "=", typ + "/" + id}})
		page, err := t.recordsRead(m, platform.FlowInstanceType, platform.Query{Domain: domain, Sort: []string{"-id"}, Limit: 20, Archived: true}, now)
		if err != nil {
			partial = true
		} else {
			processes, partial = page.Records, page.Partial
		}
	}
	work := t.app(platform.WorkApp)
	if typ != platform.WorkTaskType && work != nil {
		domain, _ := json.Marshal([]any{[]any{"ref", "=", typ + "/" + id}, []any{"state", "=", "open"}})
		page, err := t.recordsRead(m, platform.WorkTaskType, platform.Query{Domain: domain, Sort: []string{"due"}, Limit: 20}, now)
		if err != nil {
			partial = true
		} else {
			tasks, partial = page.Records, partial || page.Partial
		}
	}
	if typ != platform.WorkApprovalType && work != nil {
		domain, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
		page, err := t.recordsRead(m, platform.WorkApprovalType, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 20}, now)
		if err != nil {
			partial = true
		} else {
			approvals, partial = page.Records, partial || page.Partial
		}
	}
	return processes, tasks, approvals, partial
}

func (t *Owner) recordComments(m platform.Member, typ, id string, now time.Time) ([]any, bool, bool) {
	rel := t.app(platform.RelationsApp)
	if rel == nil || typ == platform.CommentType || typ == platform.FollowType {
		return nil, false, false
	}
	about, _ := json.Marshal([]any{[]any{"target", "=", typ + "/" + id}})
	page, err := t.recordsRead(m, platform.CommentType, platform.Query{Domain: about, Sort: []string{"created"}, Limit: 200}, now)
	if err != nil {
		return nil, false, true
	}
	following := t.Follows(m, typ+"/"+id)
	return t.narrowed(m, page.Records, now, false).([]any), following, page.Partial
}

func (t *Owner) recordDeliveries(target string, now time.Time) []RecordDelivery {
	return t.Deliveries(target, now)
}

func (t *Owner) linkRecords(m platform.Member, app, entity string, now time.Time) []Related {
	c := t.caller(m, app)
	out := []Related{}
	for _, e := range c.Links(entity) {
		typ, id, _ := strings.Cut(e, "/")
		domain, _ := json.Marshal([]any{[]any{"id", "=", id}})
		page, err := t.recordsRead(m, typ, platform.Query{Domain: domain, Limit: 1, Archived: true}, now)
		if err != nil || len(page.Records) == 0 {
			continue
		}
		i := slices.IndexFunc(out, func(r Related) bool { return r.Type == typ })
		if i < 0 {
			t.Store.Lock()
			plural := t.Store.Type(typ).Info().Plural
			t.Store.Unlock()
			out = append(out, Related{Type: typ, Field: "link", Title: plural, Records: []any{}})
			i = len(out) - 1
		}
		out[i].Records = append(out[i].Records, page.Records[0])
		out[i].Total++
	}
	return out
}

func (t *Owner) timeline(m platform.Member, app, entity string) []any {
	c := t.caller(m, app)
	activity := []any{}
	if owner := t.app(app); owner != nil {
		if notes, err := platform.CallRead(owner, c, "timeline"); err == nil {
			for _, n := range notes.([]platform.RelationNote) {
				if n.Entity == entity {
					activity = append(activity, n)
				}
			}
		}
	}
	return activity
}

func (t *Owner) readPersonal(m platform.Member, view *records.Type, ids []string, now time.Time) {
	var fields []string
	for _, f := range view.Info().Fields {
		if f.Personal != "" {
			fields = append(fields, f.Name)
		}
	}
	if len(fields) == 0 || len(ids) == 0 || strings.HasPrefix(m.ID, "app:") {
		return
	}
	t.AuditPersonal(platform.PersonalRead{At: now, Member: m.ID, Type: view.Info().Type, IDs: ids, Fields: fields})
}

func (t *Owner) RecordOf(m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	return t.recordOf(m, typ, id, now)
}

func (t *Owner) RecordSection(m platform.Member, typ, id, section string, now time.Time) (RecordSectionView, *kernel.Error) {
	return t.recordSection(m, typ, id, section, now)
}

func (t *Owner) RecordDetail(m platform.Member, typ, id string, now time.Time, sections ...string) (RecordView, *kernel.Error) {
	return t.recordDetail(m, typ, id, now, sections...)
}
