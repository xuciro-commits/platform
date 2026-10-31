package platform

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func IntegrationTypeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		if _, err := time.Parse(time.RFC3339, x); err == nil {
			return "date"
		}
		if len(x) == 10 {
			if _, err := time.Parse("2006-01-02", x); err == nil {
				return "date"
			}
		}
		return "string"
	default:
		return "json"
	}
}

func IntegrationLookupPath(in map[string]any, path string) (any, bool) {
	var cur any = in
	for _, part := range strings.Split(path, ".") {
		switch value := cur.(type) {
		case map[string]any:
			var ok bool
			if cur, ok = value[part]; !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			cur = value[index]
		default:
			return nil, false
		}
	}
	return cur, true
}

func IntegrationMatches(v any, op, want string) bool {
	text := IntegrationScalar(v)
	switch op {
	case "empty":
		return v == nil || text == ""
	case "notempty":
		return v != nil && text != ""
	case "contains":
		return strings.Contains(strings.ToLower(text), strings.ToLower(want))
	case "=":
		return text == want || (IntegrationTypeOf(v) == "number" && IntegrationNumeric(v) == IntegrationNumeric(want))
	case "!=":
		return !(text == want || (IntegrationTypeOf(v) == "number" && IntegrationNumeric(v) == IntegrationNumeric(want)))
	}
	if IntegrationTypeOf(v) == "number" {
		a, b := IntegrationNumeric(v), IntegrationNumeric(want)
		switch op {
		case "<":
			return a < b
		case "<=":
			return a <= b
		case ">":
			return a > b
		case ">=":
			return a >= b
		}
	}
	switch op {
	case "<":
		return text < want
	case "<=":
		return text <= want
	case ">":
		return text > want
	case ">=":
		return text >= want
	}
	return false
}

func IntegrationNumeric(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Float64()
		return n
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return n
	}
	return 0
}

func IntegrationKeyOf(r map[string]any, cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = IntegrationScalar(r[c])
	}
	return strings.Join(parts, "\x1f")
}

func IntegrationNormalize(v, how string) string {
	switch how {
	case "folded":
		return strings.Join(strings.Fields(strings.ToLower(v)), " ")
	case "digits":
		var b strings.Builder
		for _, r := range v {
			if unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		return strings.TrimLeft(b.String(), "0")
	}
	return v
}

func IntegrationCSVRows(body []byte) ([]string, []map[string]any, error) {
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("the answer is not CSV: %v", err)
	}
	if len(records) == 0 {
		return nil, nil, nil
	}
	head := make([]string, len(records[0]))
	for i, name := range records[0] {
		head[i] = strings.TrimSpace(name)
	}
	kinds := make([]string, len(head))
	for i := range head {
		kinds[i] = csvColumnKind(records[1:], i)
	}
	rows := make([]map[string]any, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := map[string]any{}
		for i, name := range head {
			if i >= len(rec) {
				continue
			}
			value := strings.TrimSpace(rec[i])
			switch {
			case kinds[i] == "string":
				row[name] = rec[i]
			case value == "":
				row[name] = nil
			case kinds[i] == "number":
				row[name], _ = strconv.ParseFloat(value, 64)
			default:
				row[name] = strings.EqualFold(value, "true")
			}
		}
		rows = append(rows, row)
	}
	return head, rows, nil
}

func csvColumnKind(records [][]string, column int) string {
	kind := ""
	for _, rec := range records {
		if column >= len(rec) {
			continue
		}
		value := strings.TrimSpace(rec[column])
		if value == "" {
			continue
		}
		next := "string"
		if strings.EqualFold(value, "true") || strings.EqualFold(value, "false") {
			next = "boolean"
		} else if n, err := strconv.ParseFloat(value, 64); err == nil && !math.IsInf(n, 0) && !math.IsNaN(n) && !(len(value) > 1 && value[0] == '0' && value[1] != '.') {
			next = "number"
		}
		if kind != "" && kind != next || next == "string" {
			return "string"
		}
		kind = next
	}
	if kind == "" {
		return "string"
	}
	return kind
}

func IntegrationRowsAt(body []byte, path string) ([]map[string]any, error) {
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

func IntegrationScalar(v any) string {
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

func IntegrationConvert(v any, to string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch to {
	case "":
		return v, nil
	case "string":
		return IntegrationScalar(v), nil
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
		text := IntegrationScalar(v)
		for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05", "2006/01/02", "02.01.2006"} {
			if t, err := time.Parse(layout, text); err == nil {
				return t.UTC().Format(time.RFC3339), nil
			}
		}
		return nil, fmt.Errorf("%q is not a date", text)
	}
	return nil, fmt.Errorf("%v cannot become %s", v, to)
}
