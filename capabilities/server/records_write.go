package platformserver

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/records"
	"platformserver/platform"
	"reflect"
	"time"
)

// declare registers an app's entity types; references resolve within the app.
func (s *recordStore) declare(a platform.App) error {
	m := a.Manifest()
	own := map[reflect.Type]string{}
	for _, e := range m.Entities {
		own[reflect.TypeOf(e.Model)] = e.Type
	}
	classes := map[string]bool{}
	for _, d := range a.Declarations() {
		classes[d.GetDataClass()] = true
	}
	for _, e := range m.Entities {
		info, err := platform.Describe(m.ID, e, func(t reflect.Type) string { return own[t] })
		if err != nil {
			return err
		}
		if !classes[e.Type] {
			return fmt.Errorf("entity %s is not a data class the app is authority for", e.Type)
		}
		if s.types[e.Type] != nil || s.byGo[info.Go] != nil {
			return fmt.Errorf("entity %s is declared twice", e.Type)
		}
		et := &entityType{store: s, info: info, rows: records.Rows{}, knowledge: records.KnowledgeBearing(info)}
		s.types[e.Type], s.byGo[info.Go] = et, et
		s.generation++
	}
	for _, e := range m.Entities { // seeds after every type is known, so references resolve
		seeder := platform.NewCaller(nil, platform.Member{ID: "seed"}, m.ID, false, false)
		seed := &pb.ChangeRecord{ChangeId: "seed", Submission: &pb.Submission{PrincipalId: "seed", Schema: &pb.SchemaRef{Name: "seed"}}}
		for _, v := range e.Seed {
			if reflect.TypeOf(v) != reflect.TypeOf(e.Model) {
				return fmt.Errorf("entity %s: a seed record is not a %s", e.Type, reflect.TypeOf(e.Model))
			}
			if err := s.check(seeder, v, time.Now()); err != nil {
				return fmt.Errorf("entity %s: a seed record does not hold: %v", e.Type, err)
			}
			if err := s.put(seeder, seed, v); err != nil {
				return fmt.Errorf("entity %s: seed record: %v", e.Type, err)
			}
		}
	}
	return nil
}

// of is c's app's entity type for a Go type; other apps' types are not reachable (D3).
func (s *recordStore) of(c platform.Caller, t reflect.Type) *entityType {
	et := s.byGo[t]
	if et == nil || et.info.App != c.App {
		return nil
	}
	return et
}

func (s *recordStore) put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealed {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	et := s.of(c, reflect.TypeOf(entity))
	if et == nil || r == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := s.migrationWriteLocked(et.info, r); err != nil {
		return err
	}
	v := records.Copy(et.info.Go, entity)
	platform.ApplyFieldAliases(et.info, v)
	records.EmptyLists(v)
	rec := records.Header(v)
	if rec.ID == "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := s.checkLinkWriteLocked(et, v); err != nil {
		return err
	}
	prev := et.rows.Get(rec.ID)
	next, refusal := records.Write(et.info, r, prev, v)
	if refusal != nil {
		return refusal
	}
	et.setRow(rec.ID, next)
	s.generation++
	s.liveTouched(et.info.Type)
	if s.writes != nil {
		s.writes[et.info.Type+"/"+rec.ID] = true
	}
	s.recentChanges.Remember(c.App+"/"+r.GetChangeId(), et.info.Type+"/"+rec.ID)
	if et.knowledge {
		s.dirty.Set(et.info.Type+"/"+rec.ID, true)
	}
	if s.touched != nil {
		s.touched(et.info.Type, rec.ID)
	}
	return nil
}

func (s *recordStore) get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, t)
	if et == nil {
		return nil, false
	}
	row := et.rows.Get(id)
	if row == nil {
		return nil, false
	}
	return records.Copy(t, row.Value.Interface()).Interface(), true
}

func (s *recordStore) check(c platform.Caller, entity any, now time.Time) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, reflect.TypeOf(entity))
	if et == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	v := records.Copy(et.info.Go, entity)
	platform.ApplyFieldAliases(et.info, v)
	issues, refusal := records.CheckFields(et.info, entity, v, c.Replaying, s)
	if len(issues) > 0 {
		return c.RefuseFields(issues)
	}
	if refusal != nil {
		return refusal
	}
	return s.checkLinkWriteLocked(et, v)
}

// HasRecord is called only under the owning store's lock during field checks.
func (s *recordStore) HasRecord(typ, id string) bool {
	et := s.types[typ]
	return et != nil && et.rows.Get(id) != nil
}

func (r runtime) Put(c platform.Caller, rec *pb.ChangeRecord, entity any) *kernel.Error {
	if !c.Replaying {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A new record write requires a private runtime")
	}
	return r.t.records.put(c, rec, entity)
}

func (r runtime) Get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	return r.t.records.get(c, t, id)
}

func (r runtime) Find(c platform.Caller, t reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	s := r.t.records
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, t)
	if et == nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if len(q.Sort) == 0 {
		q.Sort = []string{"id"}
	}
	q = businessQuery(et.info, q, c.Member, time.Now())
	page, total, err := s.find(et, q, nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = records.Copy(t, v.Interface()).Interface()
	}
	return out, total, err
}

func (r runtime) Check(c platform.Caller, entity any) *kernel.Error {
	return r.t.records.check(c, entity, time.Now())
}
