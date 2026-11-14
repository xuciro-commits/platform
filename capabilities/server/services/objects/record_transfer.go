package objects

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ImportLine struct {
	Row     int
	ID      string
	Payload json.RawMessage
	Outcome string
}

func ParseImport(info platform.EntityInfo, data []byte, currency string) (string, []ImportLine, bool) {
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) < 1 || !slices.Contains(rows[0], "id") {
		return "", nil, false
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
			v, err := fromCSV(f, value, currency)
			if err != nil {
				r.Outcome = "ERROR_CODE_INVALID_ARGUMENT: " + name
				break
			}
			payload[name] = v
		}
		if r.Outcome == "" && r.ID == "" {
			r.Outcome = "ERROR_CODE_INVALID_ARGUMENT"
		}
		if r.Outcome == "" {
			r.Payload, _ = json.Marshal(payload)
		}
		out = append(out, r)
	}
	return hex.EncodeToString(sum[:8]), out, true
}

func fromCSV(f platform.FieldInfo, value, currency string) (any, error) {
	switch f.Type {
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
	case "references", "tags":
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

func NewExport(info platform.EntityInfo) *Export {
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

func (o *Owner) ExportRead(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	m = o.AuthorizationMember(m)
	if err := o.Admit(m); err != nil {
		return nil, err
	}
	o.Store.Lock()
	et := o.Store.Type(typ)
	o.Store.Unlock()
	if et == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
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
