package data

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"time"

	pgx "github.com/jackc/pgx/v5"
)

const odataPages = 20

type Credentials interface {
	CredentialValue(name, consumer, address string) ([]byte, bool)
	CredentialHeader(name, consumer, address string) ([]byte, bool)
}

type Client struct {
	Credentials Credentials
	Send        func(req *http.Request, allowPrivate bool) (*http.Response, error)
	A2A         func(id, url, secret string, allowPrivate bool, x platform.Effect) platform.Outcome
	Private     error
}

func (r Client) Read(s Source, conn *Connection) ([]map[string]any, error) {
	switch s.Profile {
	case "odata":
		return r.readOData(s, *conn)
	case "table":
		return r.ReadTable(s, *conn)
	}
	url, header, private := s.URL, s.Header, s.AllowPrivate
	if conn != nil {
		url, private = conn.Resolve(s.URL), conn.AllowPrivate
		header = r.Authorization(*conn, url)
		if conn.Secret != "" && header == "" {
			return nil, fmt.Errorf("The connection credential is not authorized for this target")
		}
	}
	accept := "application/json"
	if s.Profile == "csv" {
		accept = "text/csv, text/plain"
	}
	body, err := r.Fetch(url, header, private, accept)
	if err != nil {
		return nil, err
	}
	return s.Decode(body)
}

func (r Client) Authorization(c Connection, target ...string) string {
	address := c.Address
	if len(target) > 0 {
		address = target[0]
	}
	if c.Secret == "" {
		return ""
	}
	if v, ok := r.Credentials.CredentialHeader(c.Secret, "connection/"+c.ID, address); ok && len(strings.TrimSpace(string(v))) > 0 {
		return "Authorization: " + strings.TrimSpace(string(v))
	}
	return ""
}

func (r Client) Fetch(url, header string, allowPrivate bool, accept string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("the URL is not valid")
	}
	req.Header.Set("Accept", accept)
	if name, value, ok := strings.Cut(header, ":"); ok && strings.TrimSpace(name) != "" {
		req.Header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	resp, err := r.Send(req, allowPrivate)
	if err != nil {
		return nil, fmt.Errorf("the endpoint did not answer: %v", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, SourceBody+1))
	if readErr != nil {
		return nil, fmt.Errorf("the response body could not be read")
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("the endpoint answered %d", resp.StatusCode)
	}
	if len(body) > SourceBody {
		return nil, fmt.Errorf("the answer exceeds %d bytes", SourceBody)
	}
	return body, nil
}

func (r Client) readOData(s Source, c Connection) ([]map[string]any, error) {
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
		body, err := r.Fetch(next, r.Authorization(c, next), c.AllowPrivate, "application/json")
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
		if len(answer.D) > 0 {
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

func odataLiteral(v string) string {
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return v
	}
	if strings.Trim(v, "0123456789.-") == "" {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func (r Client) LatestTableCursor(s Source, c Connection) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := r.openPostgres(ctx, c)
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

func (r Client) ReadTable(s Source, c Connection, columns ...string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	conn, err := r.openPostgres(ctx, c)
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
			if projectedBytes > SourceBody {
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

func (r Client) openPostgres(ctx context.Context, c Connection) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig(c.Address)
	if err != nil {
		return nil, fmt.Errorf("the address is not a postgres URL")
	}
	if c.Secret != "" {
		if v, ok := r.Credentials.CredentialValue(c.Secret, "connection/"+c.ID, c.Address); ok {
			cfg.Password = strings.TrimSpace(string(v))
		} else {
			return nil, fmt.Errorf("The database credential is unavailable or not authorized for this target")
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

func (r Client) Check(c Connection) (string, error) {
	if c.Secret != "" {
		if value, ok := r.Credentials.CredentialValue(c.Secret, "connection/"+c.ID, c.Address); !ok || len(strings.TrimSpace(string(value))) == 0 {
			return "", fmt.Errorf("the named connection secret is unavailable")
		}
	}
	switch c.Kind {
	case "postgres":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		conn, err := r.openPostgres(ctx, c)
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
		body, err := r.Fetch(c.Resolve("")+"?$format=json", r.Authorization(c), c.AllowPrivate, "application/json")
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
		if _, err := r.Fetch(c.Resolve(""), r.Authorization(c), c.AllowPrivate, "*/*"); err != nil {
			return "", err
		}
		return "answered", nil
	}
}

func (c Client) Writeback(x platform.Effect) (out platform.Outcome) {
	sum := sha256.Sum256([]byte(x.Body))
	out = platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	var req WritebackRequest
	if json.Unmarshal([]byte(x.Body), &req) != nil {
		out.Result, out.Detail = "rejected", "the request is unreadable"
		return out
	}
	if err := CheckWritebackTransport(req.Transport, req.Method); err != nil {
		out.Result, out.Detail = "rejected", err.Error()
		return out
	}
	defer func() {
		if req.Dispatch != "" && !req.SafeRetry && out.Result == "retry" {
			out.Result, out.Detail = "unknown", "This command cannot be resent automatically: "+out.Detail
		}
	}()
	u, err := url.Parse(req.URL)
	if err != nil || u.Scheme != "https" && !(u.Scheme == "http" && req.AllowPrivate) {
		out.Result, out.Detail = "rejected", "only https addresses (http when private addresses are allowed)"
		return out
	}
	if req.Transport == "a2a" {
		message := x
		message.Body = string(platform.Raw(map[string]any{"data": req.Body}))
		answer := c.A2A("connection/"+req.Connection, req.URL, req.Secret, req.AllowPrivate, message)
		answer.Digest = out.Digest
		return answer
	}
	var requestBody any = req.Body
	body, _ := json.Marshal(requestBody)
	r, err := http.NewRequest(req.Method, req.URL, strings.NewReader(string(body)))
	if err != nil {
		out.Result, out.Detail = "rejected", "the request method or address is invalid"
		return out
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Idempotency-Key", x.ID)
	if req.Dispatch != "" {
		r.Header.Set("Idempotency-Key", req.DispatchKey)
	}
	if req.Secret != "" {
		v, ok := c.Credentials.CredentialHeader(req.Secret, "connection/"+req.Connection, req.URL)
		if !ok {
			out.Result, out.Detail = "retry", "secret "+req.Secret+" missing"
			return out
		}
		r.Header.Set("Authorization", strings.TrimSpace(string(v)))
	}
	resp, err := c.Send(r, req.AllowPrivate)
	switch {
	case err != nil && strings.Contains(err.Error(), c.Private.Error()):
		out.Result, out.Detail = "rejected", c.Private.Error()
	case err != nil:
		out.Result, out.Detail = "unknown", "No delivery answer; reconcile or record a human decision: "+err.Error()
	default:
		answer, readErr := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		resp.Body.Close()
		if readErr != nil || len(answer) > 64<<10 {
			out.Result, out.Detail = "unknown", "The delivery answer is incomplete or exceeds its retained evidence budget"
			return out
		}
		if len(answer) > 0 && !json.Valid(answer) {
			answer = platform.Raw(clipDetail(answer))
		}
		out.Answer = answer
		code := resp.StatusCode
		switch {
		case code >= 200 && code < 300:
			out.Result = "delivered"
		case code >= 400 && code < 500 && code != 408 && code != 429:
			out.Result, out.Detail = "rejected", resp.Status+": "+clipDetail(answer)
		default:
			out.Result, out.Detail = "retry", resp.Status
		}
	}
	return out
}

func clipDetail(raw []byte) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
