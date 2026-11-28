package objects

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type ImportLine struct {
	Row     int
	ID      string
	Payload json.RawMessage
	Outcome string
	Issue   *platform.FieldIssue
}

type ImportFile struct {
	File  string
	Lines []ImportLine
}

type choiceError struct{ field string }

func (e choiceError) Error() string { return "unknown or ambiguous choice for " + e.field }

func ParseImport(info platform.RecordTypeInfo, data []byte, currency string, refer func(platform.FieldInfo, string) (string, bool), translate ...func(string) []string) (ImportFile, *platform.Error) {
	if len(data) > 16<<20 {
		return ImportFile{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The import file exceeds 16 MiB")
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) < 1 {
		return ImportFile{}, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	var tr func(string) []string
	if len(translate) > 0 {
		tr = translate[0]
	}
	header := rows[0]
	sum := sha256.Sum256(data)
	out := []ImportLine{}
	for i, row := range rows[1:] {
		r := ImportLine{Row: i + 1}
		payload := map[string]any{}
		for j, name := range header {
			if j >= len(row) {
				continue
			}
			value := strings.TrimSpace(row[j])
			if name == "id" {
				r.ID = value
				continue
			}
			f, ok := info.Field(name)
			if !ok {
				r.Outcome = "ERROR_CODE_INVALID_ARGUMENT: " + name
				break
			}
			if f.ReadOnly {
				continue
			}
			if value == "" {
				if f.Type == "text" || f.Type == "longtext" || f.Type == "reference" || f.Type == "choice" || f.Type == "date" {
					payload[name] = ""
				} else {
					payload[name] = nil
				}
				continue
			}
			v, err := fromCSV(f, value, currency, refer, tr)
			if err != nil {
				var badChoice choiceError
				if errors.As(err, &badChoice) {
					r.Outcome = pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT.String()
					r.Issue = &platform.FieldIssue{Code: "choice", Path: []string{name}, Message: "Choose a listed value."}
				} else {
					r.Outcome = "ERROR_CODE_INVALID_ARGUMENT: " + name
				}
				break
			}
			payload[name] = v
		}
		if r.Outcome == "" {
			r.Payload, _ = json.Marshal(payload)
		}
		out = append(out, r)
	}
	return ImportFile{File: hex.EncodeToString(sum[:8]), Lines: out}, nil
}

func fromCSV(f platform.FieldInfo, value, currency string, refer func(platform.FieldInfo, string) (string, bool), translate ...func(string) []string) (any, error) {
	switch f.Type {
	case "choice":
		if slices.Contains(f.Choices, value) {
			return value, nil
		}
		var matches []string
		for i, key := range f.Choices {
			if strings.EqualFold(key, value) || (i < len(f.ChoiceTitles) && strings.EqualFold(f.ChoiceTitles[i], value)) {
				matches = append(matches, key)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) == 0 && len(translate) > 0 && translate[0] != nil {
			for i, key := range f.Choices {
				title := key
				if i < len(f.ChoiceTitles) && f.ChoiceTitles[i] != "" {
					title = f.ChoiceTitles[i]
				}
				if slices.ContainsFunc(translate[0](title), func(said string) bool { return strings.EqualFold(said, value) }) {
					matches = append(matches, key)
				}
			}
			if len(matches) == 1 {
				return matches[0], nil
			}
		}
		return nil, choiceError{field: f.Name}
	case "reference":
		id, ok := refer(f, value)
		if !ok {
			return nil, fmt.Errorf("no %s is named %s", f.Ref, value)
		}
		return id, nil
	case "date", "datetime":
		if days, err := strconv.ParseFloat(value, 64); err == nil {
			at := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).Add(time.Duration(days * float64(24*time.Hour)))
			if f.Type == "date" {
				return at.Format(time.DateOnly), nil
			}
			return at.Round(time.Second).Format(time.RFC3339), nil
		}
	case "integer":
		return strconv.Atoi(value)
	case "decimal":
		return strconv.ParseFloat(value, 64)
	case "boolean":
		return strconv.ParseBool(value)
	case "money":
		amount, cur, _ := strings.Cut(value, " ")
		x, err := strconv.ParseFloat(amount, 64)
		if cur == "" {
			cur = currency
		}
		return platform.Money{Amount: int64(x*100 + 0.5*sign(x)), Currency: cur}, err
	case "references":
		parts := strings.Split(value, ";")
		for i := range parts {
			id, ok := refer(f, strings.TrimSpace(parts[i]))
			if !ok {
				return nil, fmt.Errorf("no %s is named %s", f.Ref, parts[i])
			}
			parts[i] = id
		}
		return parts, nil
	case "tags":
		return strings.Split(value, ";"), nil
	case "lines":
		return nil, fmt.Errorf("lines are not imported")
	}
	return value, nil
}

func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

type Export struct {
	fields []platform.FieldInfo
	buf    bytes.Buffer
	w      *csv.Writer
}

func NewExport(info platform.RecordTypeInfo) *Export {
	e := &Export{}
	e.w = csv.NewWriter(&e.buf)
	header := []string{"id"}
	for _, f := range info.Fields {
		if f.Type != "lines" {
			e.fields, header = append(e.fields, f), append(header, f.Name)
		}
	}
	e.w.Write(header)
	return e
}

func (e *Export) Add(record any) {
	v := reflect.ValueOf(record)
	line := []string{v.FieldByName("Record").Interface().(platform.Record).ID}
	for _, f := range e.fields {
		line = append(line, toCSV(v.FieldByIndex(f.Index).Interface()))
	}
	e.w.Write(line)
}

func (e *Export) Bytes() []byte {
	e.w.Flush()
	return e.buf.Bytes()
}

func toCSV(v any) string {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return ""
		}
		return toCSV(rv.Elem().Interface())
	}
	switch x := v.(type) {
	case platform.Money:
		if x.Currency == "" {
			return ""
		}
		return fmt.Sprintf("%.2f %s", float64(x.Amount)/100, x.Currency)
	case []string:
		return strings.Join(x, ";")
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return x.Format(time.RFC3339)
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice {
		var parts []string
		for i := range rv.Len() {
			parts = append(parts, toCSV(rv.Index(i).Interface()))
		}
		return strings.Join(parts, ";")
	}
	return fmt.Sprint(v)
}

func (o *Owner) Referrer(m platform.Member, now time.Time, translate ...func(string) []string) func(platform.FieldInfo, string) (string, bool) {
	var tr func(string) []string
	if len(translate) > 0 {
		tr = translate[0]
	}
	type resolved struct {
		id string
		ok bool
	}
	cache := map[[2]string]resolved{}
	return func(f platform.FieldInfo, value string) (string, bool) {
		key := [2]string{f.Ref, value}
		if hit, found := cache[key]; found {
			return hit.id, hit.ok
		}
		id, ok := o.referred(m, f.Ref, value, now, tr)
		cache[key] = resolved{id: id, ok: ok}
		return id, ok
	}
}

func (o *Owner) referred(m platform.Member, typ, value string, now time.Time, translate func(string) []string) (string, bool) {
	o.Store.Lock()
	et := o.Store.Type(typ)
	o.Store.Unlock()
	if et == nil {
		return value, true
	}
	info := et.Info()
	display := info.Display
	equal := func(field string) platform.Expr {
		return platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", field), platform.ExprLiteralValue("text", value)}}
	}
	filter := equal("id")
	if display != "" && display != "id" {
		filter = platform.Expr{Op: "or", Kind: "boolean", Args: []platform.Expr{filter, equal(display)}}
	}
	page, err := o.Records(m, typ, platform.Query{Filter: &filter, Limit: 3}, now)
	if err != nil {
		return value, false
	}
	id := func(record any) string { return reflect.ValueOf(record).Field(0).Interface().(platform.Record).ID }
	for _, record := range page.Records {
		if id(record) == value {
			return value, true
		}
	}
	if len(page.Records) == 1 {
		return id(page.Records[0]), true
	}
	if len(page.Records) > 1 || display == "" || display == "id" {
		return value, false
	}
	all, err := o.Records(m, typ, platform.Query{Limit: 501}, now)
	if err != nil || len(all.Records) > 500 {
		return value, false
	}
	var matches []string
	for _, record := range all.Records {
		shown := records.BusinessField(info, reflect.ValueOf(record), display)
		if shown == "" {
			continue
		}
		if strings.EqualFold(shown, value) || (translate != nil && slices.ContainsFunc(translate(shown), func(said string) bool { return strings.EqualFold(said, value) })) {
			rid := id(record)
			if !slices.Contains(matches, rid) {
				matches = append(matches, rid)
			}
		}
	}
	if len(matches) != 1 {
		return value, false
	}
	return matches[0], true
}

func (o *Owner) ExportRead(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *platform.Error) {
	m = o.AuthorizationMember(m)
	if err := o.Admit(m); err != nil {
		return nil, err
	}
	o.Store.Lock()
	et := o.Store.Type(typ)
	o.Store.Unlock()
	if et == nil {
		return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	c := o.Caller(m, et.Info().App)
	if err := AuthorizationRefusal(o.Authorize(o.Store, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "export", Type: typ}, now, false)); err != nil {
		return nil, err
	}
	view, _ := ViewOf(m, et)
	export := NewExport(view.Info())
	q.Offset, q.Limit = 0, 500
	for {
		page, err := o.Records(m, typ, q, now)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Records {
			export.Add(r)
		}
		q.Offset += len(page.Records)
		if len(page.Records) == 0 || q.Offset >= page.Total {
			break
		}
	}
	return export.Bytes(), nil
}
