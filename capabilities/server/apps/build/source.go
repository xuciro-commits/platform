package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	SourceType         = "build.source"
	SchemaSourcePulled = "build.source.pulled"
	SourceBody         = 16 << 20 // the most bytes an answer may hold
	sourceRows         = 5000
	sourceFailuresKept = 20
)

type Source struct {
	platform.Record
	Name  string `json:"name" field:"required,search"`
	Title string `json:"title" field:"required,search"`
	// URL answers JSON over https (http only to a private address when allowed).
	URL          string `json:"url" field:"required" title:"URL"`
	AllowPrivate bool   `json:"allowPrivate,omitempty" title:"Allow private address" help:"Also accept http and private networks, for on-premise systems"`
	// Header is one request header, "Name: value", typically Authorization.
	Header string `json:"header,omitempty" title:"Request header" help:"One header such as Authorization: Bearer …"`
	// Path is the dotted path to the array of rows inside the answer; empty: the answer is the array.
	Path string `json:"path,omitempty" title:"Rows at" help:"Dotted path to the array, such as data.items; empty when the answer is the array"`
	// Object is the tenant's type the rows become; Key the row field that is the record's id.
	Object  string        `json:"object" field:"required" title:"Target object"`
	Key     string        `json:"key" field:"required" title:"Row id field"`
	Mapping []SourceField `json:"mapping" title:"Field mapping"`
	// Every is a period such as 15m or 24h; empty: pulled only when asked.
	Every string `json:"every,omitempty" title:"Pull every" help:"A period such as 15m, 1h or 24h; empty: only when asked"`
	State string `json:"state" field:"readonly"`
	// Puller is the member who published the source; rows are decided as them.
	Puller    string      `json:"puller,omitempty" field:"readonly" title:"Pulls as"`
	Requested bool        `json:"requested,omitempty" field:"readonly" title:"Pull requested"`
	Last      *SourcePull `json:"last,omitempty" field:"readonly" type:"json" title:"Last pull"`
}

// SourceField maps one row field onto one object field, optionally converted.
type SourceField struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Convert string `json:"convert,omitempty"` // "", string, number, boolean, date
}

// SourcePull is what one pull did: counted, with the first failures kept.
type SourcePull struct {
	At       time.Time       `json:"at"`
	Rows     int             `json:"rows"`
	Applied  int             `json:"applied"`
	Failed   int             `json:"failed"`
	Error    string          `json:"error,omitempty"`
	Failures []SourceFailure `json:"failures,omitempty"`
}

type SourceFailure struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
}

func (b *Build) sourceEntity() platform.Entity {
	return platform.Entity{Type: SourceType, Title: "Data source", Plural: "Data sources", Model: Source{}, Display: "title", Description: "An external JSON endpoint whose rows become records of one object, pulled on a period or on request.",
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the source and let the builder's job pull it.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishSource},
				{Name: "pull", Title: "Pull now", Description: "Ask for one pull at the next tick.", From: []string{"published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}, Do: requestPull},
				{Name: "pause", Title: "Pause", Description: "Stop pulling; the mapping stays.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func sourceActions() []platform.Action {
	return []platform.Action{{Schema: SchemaSourcePulled, Target: SourceType, Capability: "integrations", Title: "Keep pull result", Description: "Retain what one pull of a data source decided.", Roles: []string{Builder},
		Payload: []platform.Field{{Name: "pull", Type: "json", Required: true, Description: "The pull's counts and first failures"}}}}
}

func requestPull(_ platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	s.Requested = true
	return nil
}

func (b *Build) publishSource(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	s, ok := record.(*Source)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkSource(*s); err != nil {
		return err
	}
	s.Puller, s.Requested = c.ID, true
	return nil
}

func (b *Build) checkSource(s Source) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(s.Name, s.ID); err != nil {
		return refuse(err.Error())
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.Host == "" || u.Scheme != "https" && !(u.Scheme == "http" && s.AllowPrivate) {
		return refuse("A data source answers over https; http only to a private address when allowed")
	}
	if s.Header != "" && !strings.Contains(s.Header, ":") {
		return refuse("A request header reads Name: value")
	}
	info, ok := b.lookupEntity(s.Object)
	if !ok {
		return refuse("The target object {object} is not installed", s.Object)
	}
	if s.Key == "" || len(s.Mapping) == 0 {
		return refuse("A data source names the row field that identifies a record and maps at least one field")
	}
	seen := map[string]bool{}
	for _, m := range s.Mapping {
		f, known := info.Field(m.To)
		if m.From == "" || !known || f.ReadOnly || seen[m.To] {
			return refuse("Field mapping {to} must name a writable field of {object} once", m.To, s.Object)
		}
		if !slices.Contains([]string{"", "string", "number", "boolean", "date"}, m.Convert) {
			return refuse("Conversion {convert} is not one of string, number, boolean, date", m.Convert)
		}
		seen[m.To] = true
	}
	if s.Every != "" {
		every, parseErr := time.ParseDuration(s.Every)
		if parseErr != nil || every < time.Minute || every > 366*24*time.Hour {
			return refuse("A source pulls every period between 1m and a year, such as 15m, 1h or 24h")
		}
	}
	return nil
}

// Due is whether a published source should be pulled at now: asked for, or
// its period has passed since the last pull.
func (s Source) Due(now time.Time) bool {
	if s.State != "published" {
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

// MapRows turns an endpoint's answer into the target's rows; the host decides
// each as the publisher, outside any decision of the builder.
func (s Source) MapRows(body []byte) ([]SourceRow, error) {
	rows, err := rowsAt(body, s.Path)
	if err != nil {
		return nil, err
	}
	if len(rows) > sourceRows {
		return nil, fmt.Errorf("the answer holds %d rows; a pull takes at most %d", len(rows), sourceRows)
	}
	out := make([]SourceRow, 0, len(rows))
	for _, row := range rows {
		r := SourceRow{ID: scalar(row[s.Key])}
		payload := map[string]any{}
		for _, m := range s.Mapping {
			v, convErr := convert(row[m.From], m.Convert)
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
	if len(p.Failures) < sourceFailuresKept {
		p.Failures = append(p.Failures, SourceFailure{ID: id, Outcome: outcome})
	}
}

func (b *Build) submitSourcePulled(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Pull SourcePull `json:"pull"`
		}
		src, ok := platform.Get[Source](c, s.GetTarget().GetId())
		if !ok || json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Pull.At.IsZero() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Pull result has no source")
		}
		return func(r *pb.ChangeRecord) {
			pull := payload.Pull
			src.Last, src.Requested = &pull, false
			c.Put(r, src)
		}, nil
	})
}

// rowsAt is the array of objects at the dotted path inside a JSON answer.
func rowsAt(body []byte, path string) ([]map[string]any, error) {
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
func scalar(v any) string {
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
func convert(v any, to string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch to {
	case "":
		return v, nil
	case "string":
		return scalar(v), nil
	case "number":
		switch x := v.(type) {
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
		text := scalar(v)
		for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05", "2006/01/02", "02.01.2006"} {
			if t, err := time.Parse(layout, text); err == nil {
				return t.UTC().Format(time.RFC3339), nil
			}
		}
		return nil, fmt.Errorf("%q is not a date", text)
	}
	return nil, fmt.Errorf("%v cannot become %s", v, to)
}
