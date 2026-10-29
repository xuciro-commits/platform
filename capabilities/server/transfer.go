package platformserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	controlowner "platformserver/kernel/control"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

// Import and export of records as CSV. A row is a decision:
// the type's generated create for a new ID, its generated edit for one the
// member may read, submitted as the member and keyed by the file's hash and
// the row, so a file sent twice repeats nothing. A preview probes each row
// and applies none. An export writes what a list shows the member.

// Import runs, or previews, a CSV of records of typ: a header of field names
// with an id column, then one record per row.
func (t *Tenant) Import(m platform.Member, typ string, data []byte, preview bool, now time.Time) ([]platform.ImportRow, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	if err := t.admits(m); err != nil {
		return nil, err
	}
	t.records.Lock()
	et := t.records.Type(typ)
	t.records.Unlock()
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
	currency := t.setting(t.automation(controlowner.PlatformApp, false), controlowner.SettingCurrency)
	out := []platform.ImportRow{}
	for i, row := range rows[1:] {
		r := platform.ImportRow{Row: i + 1}
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
			f, ok := view.Info().Field(name)
			if !ok {
				r.Outcome = "ERROR_CODE_INVALID_ARGUMENT: " + name
				break
			}
			// An export writes read-only columns (state, computed values) for
			// the reader; they are not inputs, so a file that comes back
			// round-trips instead of failing on its own header.
			if f.ReadOnly {
				continue
			}
			if value == "" {
				// A blank cell clears that field. Omitting it from an edit would
				// silently preserve an old value and bypass a required invariant.
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
			// Preserve the original file receipt before inspecting today's
			// record. A fresh partial edit must not run a create's required
			// input checks just to discover that its record already exists.
			raw, _ := json.Marshal(payload)
			try := func(verb, key string) *kernel.Error {
				r.Error = nil
				r.Action = typ + verb
				if !slices.Contains(view.Info().Standard, r.Action) {
					return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
				}
				s := &pb.Submission{TenantId: t.ID, PrincipalId: m.ID, Authority: view.Info().App, IdempotencyKey: key,
					Target: &pb.EntityRef{Type: typ, Id: r.ID}, Schema: &pb.SchemaRef{Name: r.Action, Version: 1}, Payload: raw}
				if preview {
					refusal, issues := t.probeDiagnosed(m, s, now)
					if refusal != nil {
						r.Error = &platform.ErrorBody{Code: refusal.Code.String(), Message: refusal.Message, Issues: issues}
					}
					return refusal
				}
				_, err, issues := t.submitDiagnosed(m, s, now)
				if err != nil {
					r.Error = &platform.ErrorBody{Code: err.Code.String(), Message: err.Message, Issues: issues}
				}
				return err
			}
			key := fmt.Sprintf("import:%s:%d", file, r.Row)
			verb := func() string {
				t.mu.Lock()
				defer t.mu.Unlock()
				ledger := t.app(view.Info().App).AcceptedLedger()
				if ledger.AcceptedFor(t.ID, key) != nil {
					return ".create"
				}
				if ledger.AcceptedFor(t.ID, key+":edit") != nil || t.readable(m, typ+"/"+r.ID, now) {
					return ".edit"
				}
				return ".create"
			}()
			requestKey := key
			if verb == ".edit" {
				requestKey += ":edit"
			}
			err := try(verb, requestKey)
			if verb == ".create" && err != nil && err.Code == pb.ErrorCode_ERROR_CODE_CONFLICT {
				err = try(".edit", key+":edit")
			}
			r.Outcome = outcomeOf(err)
			if r.Error != nil && len(r.Error.Issues) > 0 && r.Error.Issues[0].Constraint != "" {
				r.Outcome, r.Error.Code = "CONSTRAINT_VIOLATION", "CONSTRAINT_VIOLATION"
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (t *Tenant) probeDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*kernel.Error, []platform.FieldIssue) {
	a := t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return unknown(), nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	draft := t.newStagedDecision()
	draft.probing = true
	_, err := decideAcceptedAs(a, draft, m, s, now, false)
	return err, draft.inputIssues[err]
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
func (t *Tenant) exportRead(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	m = t.authorizationMember(m)
	if err := t.admits(m); err != nil {
		return nil, err
	}
	t.records.Lock()
	et := t.records.Type(typ)
	t.records.Unlock()
	if et == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	c := platform.NewCaller(runtime{t}, m, et.Info().App, false, false)
	if err := authorizationRefusal(t.authorizeFrom(t.records, c, platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "export", Type: typ}, now, false)); err != nil {
		return nil, err
	}
	view, _ := viewOf(m, et)
	var fields []platform.FieldInfo
	header := []string{"id"}
	for _, f := range view.Info().Fields {
		if f.Type != "lines" {
			fields, header = append(fields, f), append(header, f.Name)
		}
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Write(header)
	q.Offset, q.Limit = 0, 500
	for {
		page, err := t.recordsRead(m, typ, q, now)
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

// restricted says whether typ holds restricted data: a dataset or
// version marked so, or an object a restricted pipeline writes.
func (t *Tenant) restricted(typ string) bool {
	c := t.automation(build.ID, false)
	switch typ {
	case build.DatasetType, build.DatasetVersionType:
		sets, _, _ := platform.Find[build.Dataset](c, platform.Query{Limit: 500})
		return slices.ContainsFunc(sets, func(d build.Dataset) bool { return d.Marking == "restricted" })
	}
	pipelines, _, _ := platform.Find[build.Pipeline](c, platform.Query{Limit: 500})
	return slices.ContainsFunc(pipelines, func(p build.Pipeline) bool { return p.OutputObject == typ && p.Marking == "restricted" })
}
