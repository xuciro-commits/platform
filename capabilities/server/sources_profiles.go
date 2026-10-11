package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// The OData and table profiles (ADR-0070) and connection checks. Both run on
// the outside loop like fetches: no lock held while a system is reached, and
// every outcome enters the journal as an ordinary input.

const odataPages = 20 // pages walked per pull; the row cap bounds the rest

// readOData walks an entity set: $filter past the cursor, ordered by the
// incremental property, following nextLink in v4 (@odata.nextLink, value) and
// v2 (d.__next, d.results) shapes.
func (t *Tenant) readOData(s build.Source, c build.Connection) ([]map[string]any, error) {
	q := url.Values{}
	q.Set("$format", "json")
	filter := s.Filter
	if s.Since != "" {
		if s.Cursor != "" {
			clause := fmt.Sprintf("%s gt %s", s.Since, odataLiteral(s.Cursor))
			if filter != "" {
				filter = "(" + filter + ") and " + clause
			} else {
				filter = clause
			}
		}
		q.Set("$orderby", s.Since)
	}
	if filter != "" {
		q.Set("$filter", filter)
	}
	next := c.Resolve(s.Entity) + "?" + q.Encode()
	var rows []map[string]any
	for page := 0; next != "" && page < odataPages && len(rows) <= 5000; page++ {
		body, err := t.fetchSource(next, t.authorization(c), c.AllowPrivate, false)
		if err != nil {
			return nil, err
		}
		var answer struct {
			Value    []map[string]any `json:"value"`
			NextLink string           `json:"@odata.nextLink"`
			D        json.RawMessage  `json:"d"`
		}
		if err := json.Unmarshal(body, &answer); err != nil {
			return nil, fmt.Errorf("the service did not answer JSON")
		}
		if len(answer.D) > 0 { // OData v2
			var d struct {
				Results []map[string]any `json:"results"`
				Next    string           `json:"__next"`
			}
			if json.Unmarshal(answer.D, &d) == nil && d.Results != nil {
				answer.Value, answer.NextLink = d.Results, d.Next
			} else {
				var list []map[string]any
				if json.Unmarshal(answer.D, &list) == nil {
					answer.Value = list
				}
			}
		}
		rows = append(rows, answer.Value...)
		next = answer.NextLink
		if next != "" {
			current, _ := url.Parse(c.Resolve(s.Entity))
			ref, err := url.Parse(next)
			if err != nil {
				return nil, fmt.Errorf("the service returned an invalid next link")
			}
			resolved := current.ResolveReference(ref)
			if resolved.Scheme != current.Scheme || resolved.Host != current.Host {
				return nil, fmt.Errorf("the service next link leaves the connection's origin")
			}
			next = resolved.String()
		}
	}
	if next != "" || len(rows) > 5000 {
		return nil, fmt.Errorf("the OData result exceeds 20 pages or 5000 rows; narrow the filter")
	}
	return rows, nil
}

// odataLiteral quotes a cursor unless it is a number or an ISO date-time.
func odataLiteral(v string) string {
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return v
	}
	if strings.Trim(v, "0123456789.-") == "" {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// readTable selects a database table past the cursor, read-only, bounded.
func (t *Tenant) latestTableCursor(s build.Source, c build.Connection) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := t.openPostgres(ctx, c)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	column := pgx.Identifier{s.Since}.Sanitize()
	query := "select " + column + " from " + pgx.Identifier(strings.Split(s.Entity, ".")).Sanitize()
	if s.Filter != "" {
		query += " where " + s.Filter
	}
	query += " order by " + column + " desc limit 1"
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return "", fmt.Errorf("the latest source cursor could not be read: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", fmt.Errorf("the latest source cursor could not be read: %v", err)
		}
		return "", nil
	}
	values, err := rows.Values()
	if err != nil || len(values) != 1 {
		return "", fmt.Errorf("the latest source cursor could not be decoded")
	}
	return s.Advance([]map[string]any{{s.Since: plain(values[0])}}), nil
}

func (t *Tenant) readTable(s build.Source, c build.Connection, columns ...string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := t.openPostgres(ctx, c)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	var where []string
	var args []any
	if s.Filter != "" {
		where = append(where, "("+s.Filter+")")
	}
	selection := "*"
	if len(columns) > 0 {
		columns = slices.Clone(columns)
		slices.Sort(columns)
		columns = slices.Compact(columns)
		for i, column := range columns {
			columns[i] = pgx.Identifier{column}.Sanitize()
		}
		selection = strings.Join(columns, ",")
	}
	query := "select " + selection + " from " + pgx.Identifier(strings.Split(s.Entity, ".")).Sanitize()
	if s.Since != "" && s.Cursor != "" {
		where = append(where, fmt.Sprintf("%s > $1", pgx.Identifier{s.Since}.Sanitize()))
		args = append(args, s.Cursor)
	}
	if len(where) > 0 {
		query += " where " + strings.Join(where, " and ")
	}
	if s.Since != "" {
		query += " order by " + pgx.Identifier{s.Since}.Sanitize()
	}
	query += " limit 5001"
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("the query failed: %v", err)
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	var out []map[string]any
	projectedBytes := 0
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("a row could not be read: %v", err)
		}
		row := map[string]any{}
		for i, f := range fields {
			row[f.Name] = plain(values[i])
		}
		if len(columns) > 0 {
			projectedBytes += len(platform.Raw(row))
			if projectedBytes > build.SourceBody {
				return nil, fmt.Errorf("the projected table rows exceed the source byte budget")
			}
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 5000 {
		if s.Since == "" || fmt.Sprint(out[4999][s.Since]) == fmt.Sprint(out[5000][s.Since]) {
			return nil, fmt.Errorf("the table result exceeds 5000 rows at one cursor; narrow the filter")
		}
		out = out[:5000]
	}
	return out, nil
}

// plain preserves database timestamps and full-width integer row identities.
func plain(v any) any {
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case int64:
		return json.Number(strconv.FormatInt(x, 10))
	case int32:
		return float64(x)
	case int16:
		return float64(x)
	case float32:
		return float64(x)
	case []byte:
		return string(x)
	}
	if raw, err := json.Marshal(v); err == nil {
		var back any
		if json.Unmarshal(raw, &back) == nil {
			return back
		}
	}
	return v
}

func (t *Tenant) openPostgres(ctx context.Context, c build.Connection) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(c.Address)
	if err != nil {
		return nil, fmt.Errorf("the address is not a postgres URL")
	}
	if c.Secret != "" {
		if v, ok := t.secret(c.Secret); ok {
			cfg.Password = strings.TrimSpace(string(v))
		}
	}
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.RuntimeParams["application_name"] = "platform-source"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("the database did not answer: %v", err)
	}
	return conn, nil
}

// CheckConnections reaches every connection whose check was asked for, once,
// and keeps the outcome on it. It runs beside PullSources.
func (t *Tenant) CheckConnections(now time.Time) {
	if t.quarantined() {
		return
	}
	t.mu.Lock()
	conns, _, _ := platform.Find[build.Connection](t.automation(build.ID, false), platform.Query{Domain: json.RawMessage(`[["requested","=",true]]`), Sort: []string{"id"}, Limit: 50})
	t.mu.Unlock()
	for _, c := range conns {
		check := build.ConnectionCheck{At: now}
		detail, err := t.checkConnection(c)
		check.OK, check.Detail = err == nil, detail
		if err != nil {
			check.Error = err.Error()
		}
		payload, _ := json.Marshal(map[string]any{"revision": c.Revision, "check": check})
		caller := t.automation(build.ID, false)
		sub := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: build.ID, IdempotencyKey: "checked:" + c.ID + ":" + now.UTC().Format(time.RFC3339Nano),
			Target: &pb.EntityRef{Type: build.ConnectionType, Id: c.ID}, Schema: &pb.SchemaRef{Name: build.SchemaConnectionChecked, Version: 1}, Payload: payload}
		func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if t.quarantined() {
				return
			}
			app := t.app(build.ID).(platform.ResultApp)
			if t.AcceptResult != nil {
				if _, err := t.submitAccepted(app, caller.Member, sub, now, true); err != nil {
					log.Printf("tenant %s: connection check result refused: %v", t.ID, err)
				}
			} else if _, err := app.Submit(caller, sub, now); err == nil {
				t.journal(app, caller.Member, sub, now)
			} else {
				log.Printf("tenant %s: connection check result refused: %v", t.ID, err)
			}
		}()
	}
}

func (t *Tenant) checkConnection(c build.Connection) (string, error) {
	if c.Secret != "" {
		if value, ok := t.secret(c.Secret); !ok || len(strings.TrimSpace(string(value))) == 0 {
			return "", fmt.Errorf("the named connection secret is unavailable")
		}
	}
	switch c.Kind {
	case "postgres":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		conn, err := t.openPostgres(ctx, c)
		if err != nil {
			return "", err
		}
		defer conn.Close(ctx)
		var version string
		if err := conn.QueryRow(ctx, "select version()").Scan(&version); err != nil {
			return "", fmt.Errorf("the database did not answer: %v", err)
		}
		return version, nil
	case "odata":
		body, err := t.fetchSource(c.Resolve("")+"?$format=json", t.authorization(c), c.AllowPrivate, false)
		if err != nil {
			return "", err
		}
		var doc struct {
			Context string `json:"@odata.context"`
			D       any    `json:"d"`
		}
		if json.Unmarshal(body, &doc) != nil {
			return "", fmt.Errorf("the service root did not answer JSON")
		}
		if doc.Context != "" {
			return "OData v4", nil
		}
		return "OData v2", nil
	default:
		if _, err := t.fetchSource(c.Resolve(""), t.authorization(c), c.AllowPrivate, false); err != nil {
			return "", err
		}
		return "answered", nil
	}
}
