package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	pgx "github.com/jackc/pgx/v5"
	"log"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"platformserver/platform"
)

type Projection struct {
	pool    *pgxpool.Pool
	tenant  *Tenant
	schema  string
	mu      sync.Mutex
	dirty   map[string]map[string]bool
	history map[string]int
	shapes  map[string]reflect.Type
}

var unsafe = regexp.MustCompile(`[^a-z0-9_]+`)

func ident(parts ...string) string {
	return unsafe.ReplaceAllString(strings.ToLower(strings.Join(parts, "_")), "_")
}

func ProjectionSchema(tenant string) string { return ident("tenant", tenant) }
func ReaderRole(tenant string) string       { return ident("tenant", tenant, "reader") }

type column struct {
	name, sqlType string
	value         func(reflect.Value) any
}

func columns(info platform.EntityInfo) []column {
	stamp := func(created bool) func(reflect.Value) platform.Stamp {
		return func(v reflect.Value) platform.Stamp {
			if created {
				return records.Header(v).Created
			}
			return records.Header(v).Changed
		}
	}
	at := func(s func(reflect.Value) platform.Stamp) func(reflect.Value) any {
		return func(v reflect.Value) any {
			if t := s(v).At; !t.IsZero() {
				return t
			}
			return nil
		}
	}
	out := []column{
		{"id", "text primary key", func(v reflect.Value) any { return records.Header(v).ID }},
		{"revision", "bigint", func(v reflect.Value) any { return records.Header(v).Revision }},
		{"created_at", "timestamptz", at(stamp(true))},
		{"created_by", "text", func(v reflect.Value) any { return records.Header(v).Created.By }},
		{"changed_at", "timestamptz", at(stamp(false))},
		{"changed_by", "text", func(v reflect.Value) any { return records.Header(v).Changed.By }},
		{"archived", "boolean", func(v reflect.Value) any { return records.Header(v).Archived }},
	}
	out = slices.DeleteFunc(out, func(c column) bool {
		return slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return ident(f.Name) == c.name })
	})
	for _, f := range info.Fields {
		if len(f.Read) > 0 {
			continue
		}
		read := func(project func(reflect.Value) any) func(reflect.Value) any {
			return func(row reflect.Value) any {
				value := records.FieldValue(row, f.Index)
				if !value.IsValid() {
					return nil
				}
				return project(value)
			}
		}
		name := ident(f.Name)
		switch f.Type {
		case "money":
			out = append(out,
				column{name + "_amount", "bigint", read(func(v reflect.Value) any { return v.Interface().(platform.Money).Amount })},
				column{name + "_currency", "text", read(func(v reflect.Value) any { return v.Interface().(platform.Money).Currency })})
		case "integer":
			out = append(out, column{name, "bigint", read(func(v reflect.Value) any { x, _ := records.Comparable(v.Interface()).(float64); return int64(x) })})
		case "decimal":
			out = append(out, column{name, "double precision", read(func(v reflect.Value) any { return records.Comparable(v.Interface()) })})
		case "boolean":
			out = append(out, column{name, "boolean", read(func(v reflect.Value) any { return v.Bool() })})
		case "date":
			out = append(out, column{name, "date", read(func(v reflect.Value) any {
				if t, err := time.Parse(time.DateOnly, v.String()); err == nil {
					return t
				}
				return nil
			})})
		case "datetime":
			out = append(out, column{name, "timestamptz", read(func(v reflect.Value) any {
				if t, _ := v.Interface().(time.Time); !t.IsZero() {
					return t
				}
				return nil
			})})
		case "references", "tags":
			out = append(out, column{name, "text[]", read(func(v reflect.Value) any {
				list := []string{}
				for i := range v.Len() {
					list = append(list, v.Index(i).String())
				}
				return list
			})})
		case "lines":
			out = append(out, column{name, "jsonb", read(func(v reflect.Value) any { raw, _ := json.Marshal(v.Interface()); return raw })})
		default:
			out = append(out, column{name, "text", read(func(v reflect.Value) any { return fmt.Sprint(v.Interface()) })})
		}
	}
	return out
}

func tableOf(typ string) string { return ident(typ) }

func q(parts ...string) string { return pgx.Identifier(parts).Sanitize() }

func Project(ctx context.Context, pool *pgxpool.Pool, t *Tenant) (*Projection, error) {
	p := &Projection{pool: pool, tenant: t, schema: ProjectionSchema(t.ID), dirty: map[string]map[string]bool{}, history: map[string]int{}}
	if err := p.refresh(ctx); err != nil {
		return nil, err
	}
	t.records.Lock()
	t.records.SetTouched(p.touch)
	t.records.Unlock()
	return p, nil
}

func (p *Projection) refresh(ctx context.Context) error {
	s := p.tenant.records
	s.Lock()
	infos := []platform.EntityInfo{}
	for _, et := range s.Types() {
		infos = append(infos, et.Info())
	}
	s.Unlock()
	slices.SortFunc(infos, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, fmt.Sprintf(`create schema if not exists %s`, p.schema)); err != nil {
		return err
	}
	history := map[string]int{}
	for _, info := range infos {
		cols := columns(info)
		defs := make([]string, len(cols))
		for i, c := range cols {
			defs[i] = q(c.name) + " " + c.sqlType
		}
		table := tableOf(info.Type)
		if _, err := tx.Exec(ctx, fmt.Sprintf(`create table if not exists %s (%s);
			create table if not exists %s (record_id text not null, seq integer not null, change text not null, schema text not null, "by" text not null,
				at timestamptz not null, fields jsonb not null, primary key (record_id, seq))`,
			q(p.schema, table), strings.Join(defs, ", "), q(p.schema, table+"_changes"))); err != nil {
			return err
		}
		for _, c := range cols {
			if c.name == "id" {
				continue
			}
			if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s add column if not exists %s %s`, q(p.schema, table), q(c.name), c.sqlType)); err != nil {
				return err
			}
		}
		retained := map[string]bool{}
		for _, c := range cols {
			retained[c.name] = true
		}
		attributes, err := tx.Query(ctx, `select attname from pg_attribute where attrelid=$1::regclass and attnum>0 and not attisdropped`, q(p.schema, table))
		if err != nil {
			return err
		}
		var removed []string
		for attributes.Next() {
			var name string
			if err = attributes.Scan(&name); err != nil {
				attributes.Close()
				return err
			}
			if !retained[name] {
				removed = append(removed, name)
			}
		}
		err = attributes.Err()
		attributes.Close()
		if err != nil {
			return err
		}
		for _, name := range removed {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s drop column %s`, q(p.schema, table), q(name))); err != nil {
				return err
			}
		}
		existing, err := tx.Query(ctx, fmt.Sprintf(`select record_id, max(seq)+1 from %s group by record_id`, q(p.schema, table+"_changes")))
		if err != nil {
			return err
		}
		for existing.Next() {
			var id string
			var end int
			if err = existing.Scan(&id, &end); err != nil {
				existing.Close()
				return err
			}
			p.history[info.Type+"/"+id] = end
		}
		err = existing.Err()
		existing.Close()
		if err != nil {
			return err
		}
		rows, changes, ends, err := p.rows(ctx, info.Type, nil)
		if err != nil {
			return err
		}
		for ref, end := range ends {
			history[ref] = end
		}
		upsert, change := p.writeSQL(info)
		batch := &pgx.Batch{}
		for _, r := range rows {
			batch.Queue(upsert, r...)
		}
		for _, r := range changes {
			batch.Queue(change, r...)
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return err
		}

	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	p.recordHistory(history)
	p.grant(ctx)
	p.shapes = make(map[string]reflect.Type, len(infos))
	for _, info := range infos {
		p.shapes[info.Type] = info.Go
	}
	return nil
}

func (p *Projection) grant(ctx context.Context) {
	role := ReaderRole(p.tenant.ID)
	if _, err := p.pool.Exec(ctx, fmt.Sprintf(`do $$ begin
			if not exists (select from pg_roles where rolname = '%s') then create role %s nologin; end if;
		end $$; grant usage on schema %s to %s; grant select on all tables in schema %s to %s`, role, role, p.schema, role, p.schema, role)); err != nil {
		log.Printf("projection %s: reader role: %v", p.tenant.ID, err)
	}
}

func (p *Projection) touch(typ, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dirty[typ] == nil {
		p.dirty[typ] = map[string]bool{}
	}
	p.dirty[typ][id] = true
}

type projectionRows struct {
	rows, changes [][]any
	ends          map[string]int
}

func (p *Projection) rows(ctx context.Context, typ string, ids map[string]bool) ([][]any, [][]any, map[string]int, error) {
	out, refusal := recordRead(p.tenant, ctx, func() (projectionRows, *kernel.Error) {
		rows, changes, ends := p.readRows(typ, ids)
		return projectionRows{rows, changes, ends}, nil
	})
	if refusal != nil {
		return nil, nil, nil, refusal
	}
	return out.rows, out.changes, out.ends, nil
}

func (p *Projection) readRows(typ string, ids map[string]bool) (rows [][]any, changes [][]any, ends map[string]int) {
	p.mu.Lock()
	written := make(map[string]int, len(ids))
	if ids == nil {
		for ref, end := range p.history {
			prefix := typ + "/"
			if strings.HasPrefix(ref, prefix) {
				written[strings.TrimPrefix(ref, prefix)] = end
			}
		}
	}
	if ids != nil {
		for id := range ids {
			written[id] = p.history[typ+"/"+id]
		}
	}
	p.mu.Unlock()
	s := p.tenant.records
	s.Lock()
	defer s.Unlock()
	et := s.Type(typ)
	cols := columns(et.Info())
	ends = map[string]int{}
	take := func(id string, r *records.Row) {
		if r == nil {
			return
		}
		row := make([]any, len(cols))
		for i, c := range cols {
			row[i] = c.value(r.Value)
		}
		rows = append(rows, row)
		for offset, h := range r.HistoryAfter(written[id]) {
			seq := written[id] + offset
			fields, _ := json.Marshal(h.Fields)
			changes = append(changes, []any{id, seq, h.Change, h.Schema, h.By, h.At, fields})
		}
		ends[typ+"/"+id] = r.HistoryLength()
	}
	if ids == nil {
		for id, row := range et.Rows().All() {
			take(id, row)
		}
	} else {
		for id := range ids {
			take(id, et.Rows().Get(id))
		}
	}
	return rows, changes, ends
}

func (p *Projection) recordHistory(ends map[string]int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.history == nil {
		p.history = map[string]int{}
	}
	for ref, end := range ends {
		p.history[ref] = max(p.history[ref], end)
	}
}

func (p *Projection) Flush(ctx context.Context) (err error) {
	s := p.tenant.records
	s.Lock()
	changed := s.TypeCount() != len(p.shapes)
	for typ, et := range s.Types() {
		if p.shapes[typ] != et.Info().Go {
			changed = true
			break
		}
	}
	s.Unlock()
	if changed {
		if err := p.refresh(ctx); err != nil {
			return err
		}
	}
	p.mu.Lock()
	dirty := p.dirty
	p.dirty = map[string]map[string]bool{}
	p.mu.Unlock()
	if len(dirty) == 0 {
		return nil
	}
	defer func() {
		if err == nil {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		for typ, ids := range dirty {
			if p.dirty[typ] == nil {
				p.dirty[typ] = map[string]bool{}
			}
			for id := range ids {
				p.dirty[typ][id] = true
			}
		}
	}()
	batch := &pgx.Batch{}
	history := map[string]int{}
	for typ, ids := range dirty {
		s := p.tenant.records
		s.Lock()
		info := s.Type(typ).Info()
		s.Unlock()
		upsert, change := p.writeSQL(info)
		rows, changes, ends, readErr := p.rows(ctx, typ, ids)
		if readErr != nil {
			return readErr
		}
		for ref, end := range ends {
			history[ref] = end
		}
		for _, r := range rows {
			batch.Queue(upsert, r...)
		}
		for _, c := range changes {
			batch.Queue(change, c...)
		}
	}
	if err := p.pool.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	p.recordHistory(history)
	return nil
}

func (p *Projection) writeSQL(info platform.EntityInfo) (string, string) {
	cols := columns(info)
	names, params, updates := make([]string, len(cols)), make([]string, len(cols)), []string{}
	for i, c := range cols {
		names[i], params[i] = q(c.name), fmt.Sprintf("$%d", i+1)
		if c.name != "id" {
			updates = append(updates, q(c.name)+" = excluded."+q(c.name))
		}
	}
	table := tableOf(info.Type)
	upsert := fmt.Sprintf(`insert into %s (%s) values (%s) on conflict (id) do update set %s`, q(p.schema, table), strings.Join(names, ", "), strings.Join(params, ", "), strings.Join(updates, ", "))
	change := fmt.Sprintf(`insert into %s (record_id, seq, change, schema, "by", at, fields) values ($1,$2,$3,$4,$5,$6,$7) on conflict do nothing`, q(p.schema, table+"_changes"))
	return upsert, change
}
