package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"platformserver/platform"
)

// Projection copies a tenant's records into PostgreSQL for tools outside the
// host (ADR-0019 D3): one schema per tenant, one table per entity type with
// columns from its declaration, and one table of its changes. It is only a
// copy: rebuilt from the records at start-up, so a changed declaration needs
// no migration, then kept current after each commit. The journal stays the
// truth. Record security does not reach outside tools: reading the schema is
// an administrator's grant of its reader role.
type Projection struct {
	pool   *pgxpool.Pool
	tenant *Tenant
	schema string
	mu     sync.Mutex
	dirty  map[string]map[string]bool // type → ids changed since the last flush
}

var unsafe = regexp.MustCompile(`[^a-z0-9_]+`)

// ident is a lower-case SQL identifier made of letters, digits and underscores.
func ident(parts ...string) string {
	return unsafe.ReplaceAllString(strings.ToLower(strings.Join(parts, "_")), "_")
}

// ProjectionSchema and ReaderRole name a tenant's schema and the role that may read it.
func ProjectionSchema(tenant string) string { return ident("tenant", tenant) }
func ReaderRole(tenant string) string       { return ident("tenant", tenant, "reader") }

type column struct {
	name, sqlType string
	value         func(reflect.Value) any
}

// columns are a type's table columns: the record's own, then one per field
// (money as amount and currency; lists as arrays; lines as JSON).
func columns(info platform.EntityInfo) []column {
	stamp := func(created bool) func(reflect.Value) platform.Stamp {
		return func(v reflect.Value) platform.Stamp {
			if created {
				return recordOf(v).Created
			}
			return recordOf(v).Changed
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
		{"id", "text primary key", func(v reflect.Value) any { return recordOf(v).ID }},
		{"revision", "bigint", func(v reflect.Value) any { return recordOf(v).Revision }},
		{"created_at", "timestamptz", at(stamp(true))},
		{"created_by", "text", func(v reflect.Value) any { return recordOf(v).Created.By }},
		{"changed_at", "timestamptz", at(stamp(false))},
		{"changed_by", "text", func(v reflect.Value) any { return recordOf(v).Changed.By }},
		{"archived", "boolean", func(v reflect.Value) any { return recordOf(v).Archived }},
	}
	for _, f := range info.Fields {
		if len(f.Read) > 0 { // a field some roles may not read is not copied where no role applies (ADR-0028 D3)
			continue
		}
		read := func(v reflect.Value) reflect.Value { return v.FieldByIndex(f.Index) }
		name := ident(f.Name)
		switch f.Type {
		case "money":
			out = append(out,
				column{name + "_amount", "bigint", func(v reflect.Value) any { return read(v).Interface().(platform.Money).Amount }},
				column{name + "_currency", "text", func(v reflect.Value) any { return read(v).Interface().(platform.Money).Currency }})
		case "integer":
			out = append(out, column{name, "bigint", func(v reflect.Value) any { x, _ := comparable(f, read(v).Interface()).(float64); return int64(x) }})
		case "decimal":
			out = append(out, column{name, "double precision", func(v reflect.Value) any { return comparable(f, read(v).Interface()) }})
		case "boolean":
			out = append(out, column{name, "boolean", func(v reflect.Value) any { return read(v).Bool() }})
		case "date":
			out = append(out, column{name, "date", func(v reflect.Value) any {
				if t, err := time.Parse(time.DateOnly, read(v).String()); err == nil {
					return t
				}
				return nil
			}})
		case "datetime":
			out = append(out, column{name, "timestamptz", func(v reflect.Value) any {
				if t, _ := read(v).Interface().(time.Time); !t.IsZero() {
					return t
				}
				return nil
			}})
		case "references", "tags":
			out = append(out, column{name, "text[]", func(v reflect.Value) any {
				list := []string{}
				for i := range read(v).Len() {
					list = append(list, read(v).Index(i).String())
				}
				return list
			}})
		case "lines":
			out = append(out, column{name, "jsonb", func(v reflect.Value) any { raw, _ := json.Marshal(read(v).Interface()); return raw }})
		default: // text, long text, choices, references: their text
			out = append(out, column{name, "text", func(v reflect.Value) any { return fmt.Sprint(read(v).Interface()) }})
		}
	}
	return out
}

func tableOf(typ string) string { return ident(typ) }

// q quotes an identifier: fields may be named like SQL words ("from", "order").
func q(parts ...string) string { return pgx.Identifier(parts).Sanitize() }

// Project creates the tenant's schema and reader role, rebuilds every type's
// tables from the records, and marks each later change for Flush.
func Project(ctx context.Context, pool *pgxpool.Pool, t *Tenant) (*Projection, error) {
	p := &Projection{pool: pool, tenant: t, schema: ProjectionSchema(t.ID), dirty: map[string]map[string]bool{}}
	s := t.records
	s.mu.Lock()
	infos := []platform.EntityInfo{}
	for _, et := range s.types {
		infos = append(infos, et.info)
	}
	s.mu.Unlock()
	slices.SortFunc(infos, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// A rebuilt schema: tables of types an app no longer declares go with it.
	if _, err := tx.Exec(ctx, fmt.Sprintf(`drop schema if exists %s cascade; create schema %s`, p.schema, p.schema)); err != nil {
		return nil, err
	}
	for _, info := range infos {
		cols := columns(info)
		defs := make([]string, len(cols))
		names := make([]string, len(cols))
		for i, c := range cols {
			defs[i], names[i] = q(c.name)+" "+c.sqlType, c.name
		}
		table := tableOf(info.Type)
		if _, err := tx.Exec(ctx, fmt.Sprintf(`create table %s (%s);
			create table %s (record_id text not null, seq integer not null, change text not null, schema text not null, "by" text not null,
				at timestamptz not null, fields jsonb not null, primary key (record_id, seq))`,
			q(p.schema, table), strings.Join(defs, ", "), q(p.schema, table+"_changes"))); err != nil {
			return nil, err
		}
		rows, changes := p.rows(info.Type, nil)
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{p.schema, table}, names, pgx.CopyFromRows(rows)); err != nil {
			return nil, err
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{p.schema, table + "_changes"}, []string{"record_id", "seq", "change", "schema", "by", "at", "fields"},
			pgx.CopyFromRows(changes)); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	p.grant(ctx)
	s.mu.Lock()
	s.touched = p.touch
	s.mu.Unlock()
	return p, nil
}

// grant lets the tenant's reader role read the schema. Creating the role needs
// a database user that may create roles; without it the projection still runs.
func (p *Projection) grant(ctx context.Context) {
	role := ReaderRole(p.tenant.ID)
	if _, err := p.pool.Exec(ctx, fmt.Sprintf(`do $$ begin
			if not exists (select from pg_roles where rolname = '%s') then create role %s nologin; end if;
		end $$; grant usage on schema %s to %s; grant select on all tables in schema %s to %s`, role, role, p.schema, role, p.schema, role)); err != nil {
		log.Printf("projection %s: reader role: %v", p.tenant.ID, err)
	}
}

// touch marks a record for the next flush; the record store calls it on each put.
func (p *Projection) touch(typ, id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dirty[typ] == nil {
		p.dirty[typ] = map[string]bool{}
	}
	p.dirty[typ][id] = true
}

// rows reads a type's records (all, or the ids given) and their changes as table rows.
func (p *Projection) rows(typ string, ids map[string]bool) (rows [][]any, changes [][]any) {
	s := p.tenant.records
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.types[typ]
	cols := columns(et.info)
	for id, r := range et.rows {
		if ids != nil && !ids[id] {
			continue
		}
		row := make([]any, len(cols))
		for i, c := range cols {
			row[i] = c.value(r.value)
		}
		rows = append(rows, row)
		for seq, h := range r.history { // one decision may change a record twice: its position names a change
			fields, _ := json.Marshal(h.Fields)
			changes = append(changes, []any{id, seq, h.Change, h.Schema, h.By, h.At, fields})
		}
	}
	return rows, changes
}

// Flush writes the records changed since the last flush; the host calls it
// every second, after the decisions that changed them are committed.
func (p *Projection) Flush(ctx context.Context) error {
	p.mu.Lock()
	dirty := p.dirty
	p.dirty = map[string]map[string]bool{}
	p.mu.Unlock()
	if len(dirty) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for typ, ids := range dirty {
		s := p.tenant.records
		s.mu.Lock()
		info := s.types[typ].info
		s.mu.Unlock()
		cols := columns(info)
		names, params, updates := make([]string, len(cols)), make([]string, len(cols)), []string{}
		for i, c := range cols {
			names[i], params[i] = q(c.name), fmt.Sprintf("$%d", i+1)
			if c.name != "id" {
				updates = append(updates, q(c.name)+" = excluded."+q(c.name))
			}
		}
		table := tableOf(typ)
		upsert := fmt.Sprintf(`insert into %s (%s) values (%s) on conflict (id) do update set %s`,
			q(p.schema, table), strings.Join(names, ", "), strings.Join(params, ", "), strings.Join(updates, ", "))
		change := fmt.Sprintf(`insert into %s (record_id, seq, change, schema, "by", at, fields) values ($1, $2, $3, $4, $5, $6, $7) on conflict do nothing`, q(p.schema, table+"_changes"))
		rows, changes := p.rows(typ, ids)
		for _, r := range rows {
			batch.Queue(upsert, r...)
		}
		for _, c := range changes {
			batch.Queue(change, c...)
		}
	}
	if err := p.pool.SendBatch(ctx, batch).Close(); err != nil {
		p.mu.Lock() // try these again with the next flush
		for typ, ids := range dirty {
			for id := range ids {
				if p.dirty[typ] == nil {
					p.dirty[typ] = map[string]bool{}
				}
				p.dirty[typ][id] = true
			}
		}
		p.mu.Unlock()
		return err
	}
	return nil
}
