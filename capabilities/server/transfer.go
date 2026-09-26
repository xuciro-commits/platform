package platformserver

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

// Import and export of records as CSV (ADR-0028 D10). A row is a decision:
// the type's generated create for a new ID, its generated edit for one the
// member may read, submitted as the member and keyed by the file's hash and
// the row, so a file sent twice repeats nothing. A preview probes each row
// and applies none. An export writes what a list shows the member.

// ImportRow is what one row did, or would do.
type ImportRow struct {
	Row     int    `json:"row"` // from 1, after the header
	ID      string `json:"id"`
	Action  string `json:"action"`  // the generated action it is
	Outcome string `json:"outcome"` // ok, or the refusal's code
}

// Import runs, or previews, a CSV of records of typ: a header of field names
// with an id column, then one record per row.
func (t *Tenant) Import(m platform.Member, typ string, data []byte, preview bool, now time.Time) ([]ImportRow, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	t.records.mu.Lock()
	et := t.records.types[typ]
	t.records.mu.Unlock()
	if et == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	view, _ := viewOf(m, et)
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil || len(rows) < 1 || !slices.Contains(rows[0], "id") {
		return nil, invalid
	}
	header := rows[0]
	sum := sha256.Sum256(data)
	file := hex.EncodeToString(sum[:8])
	currency := t.setting(t.automation(PlatformApp, false), SettingCurrency)
	out := []ImportRow{}
	for i, row := range rows[1:] {
		r := ImportRow{Row: i + 1}
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
			f, ok := view.info.Field(name)
			if !ok || f.ReadOnly {
				r.Outcome = "ERROR_CODE_INVALID_ARGUMENT: " + name
				break
			}
			if value == "" {
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
			// The row's create first, under the file's key: a file sent again
			// answers with what its rows decided the first time. A record that
			// already existed is edited instead.
			raw, _ := json.Marshal(payload)
			try := func(verb, key string) *kernel.Error {
				r.Action = typ + verb
				if !slices.Contains(view.info.Standard, r.Action) {
					return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
				s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: view.info.App, IdempotencyKey: key,
					Target: &pb.EntityRef{Type: typ, Id: r.ID}, Schema: &pb.SchemaRef{Name: r.Action, Version: 1}, Payload: raw}
				if preview {
					return t.probe1(m, s, now)
				}
				_, err := t.Submit(m, s, now)
				return err
			}
			key := fmt.Sprintf("import:%s:%d", file, r.Row)
			err := try(".create", key)
			if err != nil && err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT {
				err = try(".edit", key+":edit")
			}
			r.Outcome = outcomeOf(err)
		}
		out = append(out, r)
	}
	return out, nil
}

// probe1 checks one submission's policy and rules as m, applying nothing.
func (t *Tenant) probe1(m platform.Member, s *pb.Submission, now time.Time) *kernel.Error {
	a := t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return unknown()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	was := t.probing
	t.probing = true
	defer func() { t.probing = was }()
	_, err := a.Submit(t.caller(m, a, false), s, now)
	return err
}

// fromCSV reads one cell as the field's type.
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

// Export writes the records of typ m's query finds as CSV: the ID and every
// field m may read, lines aside.
func (t *Tenant) Export(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	t.records.mu.Lock()
	et := t.records.types[typ]
	t.records.mu.Unlock()
	if et == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	view, _ := viewOf(m, et)
	var fields []platform.FieldInfo
	header := []string{"id"}
	for _, f := range view.info.Fields {
		if f.Type != "lines" {
			fields, header = append(fields, f), append(header, f.Name)
		}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write(header)
	q.Offset, q.Limit = 0, 500
	for {
		page, err := t.Records(m, typ, q, now)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Records {
			v := reflect.ValueOf(r)
			line := []string{v.FieldByName("Record").Interface().(platform.Record).ID}
			for _, f := range fields {
				line = append(line, toCSV(v.FieldByIndex(f.Index).Interface()))
			}
			w.Write(line)
		}
		q.Offset += len(page.Records)
		if len(page.Records) == 0 || q.Offset >= page.Total {
			break
		}
	}
	w.Flush()
	return buf.Bytes(), nil
}

// toCSV writes one value as a cell import reads back.
func toCSV(v any) string {
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
			parts = append(parts, fmt.Sprint(rv.Index(i).Interface()))
		}
		return strings.Join(parts, ";")
	}
	return fmt.Sprint(v)
}
