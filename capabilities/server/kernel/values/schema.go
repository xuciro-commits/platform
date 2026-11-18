package values

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"slices"
	"strconv"
	"unicode/utf8"
)

var Identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

var FieldName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

type ValueSchema struct {
	Type          string                 `json:"type"`
	Nullable      bool                   `json:"nullable,omitempty"`
	Discriminator string                 `json:"discriminator,omitempty"`
	Variants      map[string]ValueSchema `json:"variants,omitempty"`
	Properties    map[string]ValueSchema `json:"properties,omitempty"`
	Required      []string               `json:"required,omitempty"`
	Items         *ValueSchema           `json:"items,omitempty"`
	Enum          []string               `json:"enum,omitempty"`
	Description   string                 `json:"description,omitempty"`
	MaxLength     int                    `json:"maxLength,omitempty"`
	Minimum       *float64               `json:"minimum,omitempty"`
	Maximum       *float64               `json:"maximum,omitempty"`
	MinItems      int                    `json:"minItems,omitempty"`
	MaxItems      int                    `json:"maxItems,omitempty"`
}

func (s ValueSchema) Check() error { remaining := 512; return s.check(0, &remaining) }
func (s ValueSchema) check(depth int, remaining *int) error {
	*remaining--
	if *remaining < 0 {
		return fmt.Errorf("schema exceeds its node bound")
	}
	if depth > 12 || len(s.Description) > 1024 || len(s.Properties) > 64 || s.MaxLength < 0 || s.MaxLength > 1<<20 || s.MinItems < 0 || s.MaxItems < 0 || s.MinItems > 10000 || s.MaxItems > 10000 || s.MaxItems > 0 && s.MinItems > s.MaxItems {
		return fmt.Errorf("schema exceeds its structural bound")
	}
	if s.Minimum != nil && (math.IsNaN(*s.Minimum) || math.IsInf(*s.Minimum, 0)) || s.Maximum != nil && (math.IsNaN(*s.Maximum) || math.IsInf(*s.Maximum, 0)) || s.Minimum != nil && s.Maximum != nil && *s.Minimum > *s.Maximum {
		return fmt.Errorf("schema numeric bounds are invalid")
	}
	if !slices.Contains([]string{"object", "array", "string", "integer", "number", "boolean", "variant"}, s.Type) {
		return fmt.Errorf("unsupported schema type %q", s.Type)
	}
	if s.Type != "object" && (len(s.Properties) > 0 || len(s.Required) > 0) || s.Type != "array" && (s.Items != nil || s.MinItems != 0 || s.MaxItems != 0) || s.Type != "string" && (len(s.Enum) > 0 || s.MaxLength != 0) || s.Type != "number" && s.Type != "integer" && (s.Minimum != nil || s.Maximum != nil) {
		return fmt.Errorf("schema keywords do not match type %s", s.Type)
	}
	if s.Type != "variant" && (s.Discriminator != "" || len(s.Variants) > 0) {
		return fmt.Errorf("variant keywords need a variant schema")
	}
	if s.Type == "variant" {
		if !FieldName.MatchString(s.Discriminator) || len(s.Variants) < 1 || len(s.Variants) > 16 {
			return fmt.Errorf("variant needs a discriminator and 1 to 16 object cases")
		}
		for tag, child := range s.Variants {
			if !FieldName.MatchString(tag) || child.Type != "object" || child.Nullable {
				return fmt.Errorf("variant cases need named nonnullable objects")
			}
			if err := child.check(depth+1, remaining); err != nil {
				return fmt.Errorf("variant %s: %w", tag, err)
			}
			key, ok := child.Properties[s.Discriminator]
			if !ok || key.Type != "string" || key.Nullable || len(key.Enum) != 1 || key.Enum[0] != tag || !slices.Contains(child.Required, s.Discriminator) {
				return fmt.Errorf("variant %s needs its required literal discriminator %s", tag, s.Discriminator)
			}
		}
	}
	for name, child := range s.Properties {
		if !FieldName.MatchString(name) {
			return fmt.Errorf("invalid schema field %s", name)
		}
		if err := child.check(depth+1, remaining); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	seen := map[string]bool{}
	for _, name := range s.Required {
		if _, ok := s.Properties[name]; !ok || seen[name] {
			return fmt.Errorf("required schema fields must be unique declared properties")
		}
		seen[name] = true
	}
	if s.Type == "array" {
		if s.Items == nil {
			return fmt.Errorf("array schema needs items")
		}
		if err := s.Items.check(depth+1, remaining); err != nil {
			return err
		}
	}
	seen = map[string]bool{}
	if len(s.Enum) > 128 {
		return fmt.Errorf("schema has too many choices")
	}
	for _, v := range s.Enum {
		if seen[v] || len(v) > 4096 {
			return fmt.Errorf("schema choices must be unique")
		}
		seen[v] = true
	}
	return nil
}

func Decode(raw []byte, limit int) (any, error) {
	return decode(raw, limit, 16)
}

func ValidateRecord(raw []byte, maxDepth int) error {
	_, err := decode(raw, 64<<10, maxDepth)
	return err
}

func decode(raw []byte, limit, maxDepth int) (any, error) {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return nil, fmt.Errorf("JSON value exceeds its byte bound or is not UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := decodeValue(d, 0, maxDepth)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON value has trailing data")
	}
	return v, nil
}

func decodeValue(d *json.Decoder, depth, maxDepth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("JSON value exceeds its nesting bound")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch delim := t.(type) {
	case json.Delim:
		switch delim {
		case '{':
			m := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid JSON key")
				}
				if _, found := m[name]; found {
					return nil, fmt.Errorf("duplicate JSON field %s", name)
				}
				m[name], err = decodeValue(d, depth+1, maxDepth)
				if err != nil {
					return nil, err
				}
				if len(m) > 10000 {
					return nil, fmt.Errorf("JSON object is too large")
				}
			}
			if t, err := d.Token(); err != nil || t != json.Delim('}') {
				return nil, fmt.Errorf("incomplete JSON object")
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, err := decodeValue(d, depth+1, maxDepth)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
				if len(a) > 10000 {
					return nil, fmt.Errorf("JSON array is too large")
				}
			}
			if t, err := d.Token(); err != nil || t != json.Delim(']') {
				return nil, fmt.Errorf("incomplete JSON array")
			}
			return a, nil
		}
		return nil, fmt.Errorf("invalid JSON delimiter")
	default:
		return t, nil
	}
}

func (s ValueSchema) Validate(raw []byte, limit int) error {
	if err := s.Check(); err != nil {
		return err
	}
	v, err := Decode(raw, limit)
	if err != nil {
		return err
	}
	return s.validate(v, "$")
}

func (s ValueSchema) validate(v any, path string) error {
	if v == nil && s.Nullable {
		return nil
	}
	bad := func() error { return fmt.Errorf("%s must match %s", path, s.Type) }
	switch s.Type {
	case "variant":
		object, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		tag, ok := object[s.Discriminator].(string)
		child, found := s.Variants[tag]
		if !ok || !found {
			return fmt.Errorf("%s.%s must select a declared variant", path, s.Discriminator)
		}
		return child.validate(v, path)
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		for name, v := range m {
			child, ok := s.Properties[name]
			if !ok {
				return fmt.Errorf("%s.%s is unknown", path, name)
			}
			if err := child.validate(v, path+"."+name); err != nil {
				return err
			}
		}
		for _, name := range s.Required {
			if _, ok := m[name]; !ok {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
	case "array":
		a, ok := v.([]any)
		if !ok || len(a) < s.MinItems || s.MaxItems > 0 && len(a) > s.MaxItems {
			return bad()
		}
		for i, v := range a {
			if err := s.Items.validate(v, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok || s.MaxLength > 0 && len([]rune(str)) > s.MaxLength || len(s.Enum) > 0 && !slices.Contains(s.Enum, str) {
			return bad()
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return bad()
		}
	case "number", "integer":
		n, valid := jsonNumber(v)
		if !valid {
			return bad()
		}
		if len(n) > 128 {
			return bad()
		}
		if s.Type == "integer" {
			integer, err := strconv.ParseInt(string(n), 10, 64)
			if err != nil {
				return fmt.Errorf("%s requires a signed JSON integer", path)
			}
			value := new(big.Rat).SetInt64(integer)
			if s.Minimum != nil && value.Cmp(new(big.Rat).SetFloat64(*s.Minimum)) < 0 || s.Maximum != nil && value.Cmp(new(big.Rat).SetFloat64(*s.Maximum)) > 0 {
				return bad()
			}
		} else {
			number, err := strconv.ParseFloat(string(n), 64)
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || s.Minimum != nil && number < *s.Minimum || s.Maximum != nil && number > *s.Maximum {
				return bad()
			}
		}
	}
	return nil
}

func jsonNumber(v any) (json.Number, bool) {
	switch n := v.(type) {
	case json.Number:
		return n, true
	default:
		return "", false
	}
}
