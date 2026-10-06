package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
		if next != "" && !strings.Contains(next, "://") {
			next = c.Resolve(next)
		}
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
func (t *Tenant) readTable(s build.Source, c build.Connection) ([]map[string]any, error) {
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
	query := "select * from " + s.Entity
	if s.Since != "" && s.Cursor != "" {
		where = append(where, fmt.Sprintf("%s > $1", s.Since))
		args = append(args, s.Cursor)
	}
	if len(where) > 0 {
		query += " where " + strings.Join(where, " and ")
	}
	if s.Since != "" {
		query += " order by " + s.Since
	}
	query += " limit 5000"
	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("the query failed: %v", err)
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	var out []map[string]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("a row could not be read: %v", err)
		}
		row := map[string]any{}
		for i, f := range fields {
			row[f.Name] = plain(values[i])
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// plain turns driver values into what JSON rows hold: times as RFC 3339, numbers as float64.
func plain(v any) any {
	switch x := v.(type) {
	case time.Time:
		return x.UTC().Format(time.RFC3339)
	case int64:
		return float64(x)
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
		payload, _ := json.Marshal(map[string]any{"check": check})
		member := t.automation(build.ID, false).Member
		t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "checked:" + c.ID + ":" + now.UTC().Format(time.RFC3339Nano),
			Target: &pb.EntityRef{Type: build.ConnectionType, Id: c.ID}, Schema: &pb.SchemaRef{Name: build.SchemaConnectionChecked, Version: 1}, Payload: payload}, now)
	}
}

func (t *Tenant) checkConnection(c build.Connection) (string, error) {
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
