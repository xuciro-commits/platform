package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

type object struct {
	keys   []string
	values map[string]any
}

func (o *object) set(key string, value any) {
	if o.values == nil {
		o.values = map[string]any{}
	}
	if _, ok := o.values[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *object) get(key string) (any, bool) {
	value, ok := o.values[key]
	return value, ok
}

func (o *object) remove(key string) {
	if _, ok := o.values[key]; !ok {
		return
	}
	delete(o.values, key)
	o.keys = slices.DeleteFunc(o.keys, func(k string) bool { return k == key })
}

func (o *object) order() []string {
	var index, named []string
	for _, key := range o.keys {
		if arrayIndex(key) {
			index = append(index, key)
		} else {
			named = append(named, key)
		}
	}
	slices.SortFunc(index, func(a, b string) int {
		x, _ := strconv.ParseUint(a, 10, 32)
		y, _ := strconv.ParseUint(b, 10, 32)
		return int(x) - int(y)
	})
	return append(index, named...)
}

func arrayIndex(key string) bool {
	if key == "0" {
		return true
	}
	if key == "" || key[0] < '1' || key[0] > '9' {
		return false
	}
	n, err := strconv.ParseUint(key, 10, 32)
	return err == nil && n < math.MaxUint32
}

func parseJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := parseValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return value, nil
}

func parseValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			out := &object{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				value, err := parseValue(decoder)
				if err != nil {
					return nil, err
				}
				out.set(key.(string), value)
			}
			_, err := decoder.Token()
			return out, err
		case '[':
			out := []any{}
			for decoder.More() {
				value, err := parseValue(decoder)
				if err != nil {
					return nil, err
				}
				out = append(out, value)
			}
			_, err := decoder.Token()
			return out, err
		}
		return nil, fmt.Errorf("unexpected %v", t)
	case json.Number:
		number, err := strconv.ParseFloat(string(t), 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return nil, err
		}
		return number, nil
	default:
		return t, nil
	}
}

func stringify(value any) string {
	var out strings.Builder
	write(&out, value, "")
	return out.String()
}

func write(out *strings.Builder, value any, indent string) {
	inner := indent + "  "
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case float64:
		out.WriteString(jsNumber(v))
	case int:
		out.WriteString(jsNumber(float64(v)))
	case string:
		quote(out, v)
	case []any:
		if len(v) == 0 {
			out.WriteString("[]")
			return
		}
		out.WriteString("[\n")
		for i, item := range v {
			if i > 0 {
				out.WriteString(",\n")
			}
			out.WriteString(inner)
			write(out, item, inner)
		}
		out.WriteString("\n" + indent + "]")
	case *object:
		keys := v.order()
		if len(keys) == 0 {
			out.WriteString("{}")
			return
		}
		out.WriteString("{\n")
		for i, key := range keys {
			if i > 0 {
				out.WriteString(",\n")
			}
			out.WriteString(inner)
			quote(out, key)
			out.WriteString(": ")
			write(out, v.values[key], inner)
		}
		out.WriteString("\n" + indent + "}")
	default:
		panic(fmt.Sprintf("cannot stringify %T", value))
	}
}

func jsNumber(n float64) string {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return "null"
	}
	if n == 0 {
		return "0"
	}
	if abs := math.Abs(n); abs < 1e21 && abs >= 1e-6 {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	mantissa, exponent, _ := strings.Cut(strconv.FormatFloat(n, 'e', -1, 64), "e")
	sign, digits := exponent[:1], strings.TrimLeft(exponent[1:], "0")
	return mantissa + "e" + sign + digits
}

func quote(out *strings.Builder, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
