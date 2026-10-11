package buildapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"strconv"
	"strings"
	"time"

	"platformserver/platform"
)

// Data sources (ADR-0061, ADR-0057 block C): a builder names an external JSON
// endpoint, the array inside its answer, the field that identifies a row and
// how fields map onto one of the tenant's objects. The host pulls it when due
// or asked, outside any decision - like a CSV import: every row becomes the
// object's own generated create or edit as the publisher, under a key made of
// the source, the row and its content, so a page pulled twice decides nothing
// twice; then the pull's summary is decided on the source (SchemaSourcePulled).
const (
	SourceType              = "build.source"
	SchemaSourcePulled      = "build.source.pulled"
	SourceBody              = 16 << 20 // the most bytes an answer may hold
	ValueSourceRows         = 5000
	ValueSourceFailuresKept = 20
)

type Source struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// Connection is the system the source reads through (ADR-0070); empty: a bare JSON URL as in ADR-0061.
	Connection string `json:"connection,omitempty" ref:"build.connection"`
	// Profile is how rows are read: json (default), csv, odata (an entity set), table (a database table).
	Profile string `json:"profile,omitempty" choices:"json,csv,odata,table"`
	// URL answers JSON or CSV over https (http only to a private address when allowed); relative to the connection's address when one is set.
	URL string `json:"url,omitempty" title:"URL"`
	// Entity is the OData entity set or the schema.table a table profile reads; Filter narrows it ($filter, or a WHERE fragment).
	Entity string `json:"entity,omitempty" title:"Entity or table"`
	Filter string `json:"filter,omitempty" title:"Filter" help:"OData $filter, or a WHERE fragment of simple comparisons"`
	// Since is the timestamp or sequence property an incremental pull orders by; Cursor the last value pulled.
	Since        string `json:"since,omitempty" title:"Incremental column" help:"A timestamp or sequence property; each pull reads rows past the cursor"`
	Cursor       string `json:"cursor,omitempty" field:"readonly"`
	AllowPrivate bool   `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	// Header is one request header, "Name: value", typically Authorization.
	Header string `json:"header,omitempty" title:"Request header" help:"One header such as Authorization: Bearer …"`
	// Path is the dotted path to the array of rows inside the answer; empty: the answer is the array.
	Path string `json:"path,omitempty" title:"Rows at" help:"Dotted path to the array, such as data.items; empty when the answer is the array"`
	// Dataset keeps the rows as they came (ADR-0071); a pipeline maps them later. Otherwise Object is the
	// tenant's type the rows become directly, Key the row field that is the record's id.
	Dataset string        `json:"dataset,omitempty" ref:"build.dataset" title:"Target dataset"`
	Object  string        `json:"object,omitempty" title:"Target object"`
	Key     string        `json:"key,omitempty" title:"Row id field"`
	Mapping []SourceField `json:"mapping,omitempty" title:"Field mapping"`
	// Stream sources are read by the original continuous instance. They never
	// materialize machine-rate rows into ordinary object or dataset records.
	Stream bool `json:"stream,omitempty" title:"Continuous Flow source"`
	// Every is a period such as 15m or 24h; empty: pulled only when asked.
	Every string `json:"every,omitempty" title:"Pull every" help:"A period such as 15m, 1h or 24h; empty: only when asked"`
	State string `json:"state" field:"readonly"`
	// Puller is the member who published the source; rows are decided as them.
	Puller    string      `json:"puller,omitempty" field:"readonly" title:"Pulls as"`
	Requested bool        `json:"requested,omitempty" field:"readonly" title:"Pull requested"`
	Last      *SourcePull `json:"last,omitempty" field:"readonly" type:"json" title:"Last pull"`
	// Definition publication is independent of data/runtime versions.
	DefinitionVersion int      `json:"definitionVersion,omitempty" field:"readonly" title:"Version"`
	Published         string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions          []string `json:"definitionVersions,omitempty" field:"readonly" type:"json" title:"Published versions"`
}

// SourceField maps one row field onto one object field, optionally converted.
type SourceField struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Convert string `json:"convert,omitempty"` // "", string, number, boolean, date
}

// SourcePull is what one pull did: counted, with the first failures kept.
type SourcePull struct {
	DefinitionVersion string          `json:"definitionVersion,omitempty"`
	At                time.Time       `json:"at"`
	Rows              int             `json:"rows"`
	Applied           int             `json:"applied"`
	Failed            int             `json:"failed"`
	Merged            int             `json:"merged,omitempty"` // rows that became an edit of a matched record (ADR-0074)
	Error             string          `json:"error,omitempty"`
	Failures          []SourceFailure `json:"failures,omitempty"`
	// Cursor is the last incremental value the pull reached; kept on the source.
	Cursor string `json:"cursor,omitempty"`
}

type SourceFailure struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
}

// Due is whether a published source should be pulled at now: asked for, or
// its period has passed since the last pull.
func (s Source) Due(now time.Time) bool {
	if s.State != "published" || s.Stream {
		return false
	}
	if s.Requested {
		return true
	}
	every, err := time.ParseDuration(s.Every)
	if s.Every == "" || err != nil {
		return false
	}
	return s.Last == nil || !now.Before(s.Last.At.Add(every))
}

// SourceRow is one mapped row: the record's id, its payload and the key that
// makes the same content decide once.
type SourceRow struct {
	ID      string
	Payload json.RawMessage
	Key     string
	Error   string
}

// Decode turns an answer's body into rows for the json and csv profiles.
func (s Source) Decode(body []byte) ([]map[string]any, error) {
	if s.Profile == "csv" {
		return ValueCsvRows(body)
	}
	return ValueRowsAt(body, s.Path)
}

// MapRows turns decoded rows into the target's rows; the host decides each as
// the publisher, outside any decision of the builder.
func (s Source) MapRows(rows []map[string]any) ([]SourceRow, error) {
	if len(rows) > ValueSourceRows {
		return nil, fmt.Errorf("the answer holds %d rows; a pull takes at most %d", len(rows), ValueSourceRows)
	}
	out := make([]SourceRow, 0, len(rows))
	for _, row := range rows {
		r := SourceRow{ID: ValueScalar(row[s.Key])}
		payload := map[string]any{}
		for _, m := range s.Mapping {
			v, convErr := ValueConvert(row[m.From], m.Convert)
			if convErr != nil {
				r.Error = m.To + ": " + convErr.Error()
				break
			}
			if v != nil {
				payload[m.To] = v
			}
		}
		if r.ID == "" && r.Error == "" {
			r.Error = "row has no id"
		}
		r.Payload, _ = json.Marshal(payload)
		sum := sha256.Sum256(r.Payload)
		r.Key = fmt.Sprintf("source:%s:%s:%s", s.Name, r.ID, hex.EncodeToString(sum[:8]))
		out = append(out, r)
	}
	return out, nil
}

// Fail counts one failed row, keeping the first few.
func (p *SourcePull) Fail(id, outcome string) {
	p.Failed++
	if len(p.Failures) < ValueSourceFailuresKept {
		p.Failures = append(p.Failures, SourceFailure{ID: id, Outcome: outcome})
	}
}

// Advance is the cursor after rows: the greatest Since value seen, as text.
func (s Source) Advance(rows []map[string]any) string {
	if s.Since == "" {
		return ""
	}
	best := s.Cursor
	for _, row := range rows {
		if v := ValueScalar(row[s.Since]); v != "" {
			a, anum := new(big.Rat).SetString(v)
			b, bnum := new(big.Rat).SetString(best)
			at, aerr := time.Parse(time.RFC3339Nano, v)
			bt, berr := time.Parse(time.RFC3339Nano, best)
			if best == "" || anum && bnum && a.Cmp(b) > 0 || aerr == nil && berr == nil && at.After(bt) || (!anum || !bnum) && (aerr != nil || berr != nil) && v > best {
				best = v
			}
		}
	}
	return best
}

// csvRows reads a header row and the rows beneath it.
func ValueCsvRows(body []byte) ([]map[string]any, error) {
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("the answer is not CSV: %v", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	head := records[0]
	rows := make([]map[string]any, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := map[string]any{}
		for i, name := range head {
			if i < len(rec) {
				row[strings.TrimSpace(name)] = rec[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// rowsAt is the array of objects at the dotted path inside a JSON answer.
func ValueRowsAt(body []byte, path string) ([]map[string]any, error) {
	var node any
	if err := json.Unmarshal(body, &node); err != nil {
		return nil, fmt.Errorf("the answer is not JSON")
	}
	if path != "" {
		for _, part := range strings.Split(path, ".") {
			object, ok := node.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("the answer has no %s", path)
			}
			node = object[part]
		}
	}
	items, ok := node.([]any)
	if !ok {
		return nil, fmt.Errorf("the rows at %q are not an array", path)
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("a row is not an object")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// scalar is a row value as the string a record id or a text field takes.
func ValueScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		raw, _ := json.Marshal(x)
		return string(raw)
	}
}

// convert applies one declared conversion; an absent value stays absent.
func ValueConvert(v any, to string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch to {
	case "":
		return v, nil
	case "string":
		return ValueScalar(v), nil
	case "number":
		switch x := v.(type) {
		case json.Number:
			if _, err := x.Float64(); err == nil {
				return x, nil
			}
		case float64:
			return x, nil
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", x)
			}
			return n, nil
		}
	case "boolean":
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "yes", "1", "y":
				return true, nil
			case "false", "no", "0", "n", "":
				return false, nil
			}
		case float64:
			return x != 0, nil
		}
	case "date":
		text := ValueScalar(v)
		for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05", "2006/01/02", "02.01.2006"} {
			if t, err := time.Parse(layout, text); err == nil {
				return t.UTC().Format(time.RFC3339), nil
			}
		}
		return nil, fmt.Errorf("%q is not a date", text)
	}
	return nil, fmt.Errorf("%v cannot become %s", v, to)
}
