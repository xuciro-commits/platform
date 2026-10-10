package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

const WasmCommandABI = "platform-wasip1-json/v1"
const WasmDataABI = "platform-wasip1-data/v2"

// ValueSchema is the bounded JSON Schema profile shared by ordinary compute
// inputs, outputs and Logic Studio ports. Objects are closed; recursion, remote
// references and tenant-defined validators are deliberately absent.
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
	MaxItems      int                    `json:"maxItems,omitempty"`
}

func (s ValueSchema) Check() error { remaining := 512; return s.check(0, &remaining) }
func (s ValueSchema) check(depth int, remaining *int) error {
	*remaining--
	if *remaining < 0 {
		return fmt.Errorf("schema exceeds its node bound")
	}
	if depth > 12 || len(s.Description) > 1024 || len(s.Properties) > 64 || s.MaxLength < 0 || s.MaxLength > 1<<20 || s.MaxItems < 0 || s.MaxItems > 10000 {
		return fmt.Errorf("schema exceeds its structural bound")
	}
	if !slices.Contains([]string{"object", "array", "string", "integer", "number", "boolean", "variant"}, s.Type) {
		return fmt.Errorf("unsupported schema type %q", s.Type)
	}
	if s.Type != "object" && (len(s.Properties) > 0 || len(s.Required) > 0) || s.Type != "array" && (s.Items != nil || s.MaxItems != 0) || s.Type != "string" && (len(s.Enum) > 0 || s.MaxLength != 0) {
		return fmt.Errorf("schema keywords do not match type %s", s.Type)
	}
	if s.Type != "variant" && (s.Discriminator != "" || len(s.Variants) > 0) {
		return fmt.Errorf("variant keywords need a variant schema")
	}
	if s.Type == "variant" {
		if !functionFieldName.MatchString(s.Discriminator) || len(s.Variants) < 1 || len(s.Variants) > 16 {
			return fmt.Errorf("variant needs a discriminator and 1 to 16 object cases")
		}
		for tag, child := range s.Variants {
			if !functionFieldName.MatchString(tag) || child.Type != "object" || child.Nullable {
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
		if !functionFieldName.MatchString(name) {
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

// DecodeValue rejects duplicate keys before unmarshalling, retains integer
// precision, and accepts exactly one bounded UTF-8 JSON value.
func DecodeValue(raw []byte, limit int) (any, error) {
	if len(raw) == 0 || len(raw) > limit || !utf8.Valid(raw) {
		return nil, fmt.Errorf("JSON value exceeds its byte bound or is not UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := decodeValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON value has trailing data")
	}
	return v, nil
}
func decodeValue(d *json.Decoder, depth int) (any, error) {
	if depth > 16 {
		return nil, fmt.Errorf("JSON value exceeds its nesting bound")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
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
			m[name], err = decodeValue(d, depth+1)
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
			v, err := decodeValue(d, depth+1)
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
}
func (s ValueSchema) Validate(raw []byte, limit int) error {
	if err := s.Check(); err != nil {
		return err
	}
	v, err := DecodeValue(raw, limit)
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
		if !ok || s.MaxItems > 0 && len(a) > s.MaxItems {
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
		n, ok := v.(json.Number)
		if !ok {
			return bad()
		}
		if len(n) > 128 {
			return bad()
		}
		if s.Type == "integer" {
			if _, err := strconv.ParseInt(string(n), 10, 64); err != nil {
				return fmt.Errorf("%s requires a signed JSON integer", path)
			}
		} else {
			number, err := strconv.ParseFloat(string(n), 64)
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
				return bad()
			}
		}
	}
	return nil
}

type OperationBinding struct {
	Kind   string `json:"kind"`
	Module string `json:"module,omitempty"`
	ABI    string `json:"abi,omitempty"`
}
type OperationLimits struct {
	TimeoutMillis  int    `json:"timeoutMillis"`
	MemoryPages    uint32 `json:"memoryPages"`
	MaxInputBytes  int    `json:"maxInputBytes"`
	MaxOutputBytes int    `json:"maxOutputBytes"`
	// StagedOutputBytes, when set, allows a result above MaxOutputBytes to be
	// sealed into the per-call result channel the host owns (ADR-0047 §13.3).
	// 0 keeps the inline-only profile; the worker still never writes anywhere.
	StagedOutputBytes int `json:"stagedOutputBytes,omitempty"`
	// DataInputBytes bounds the call-owned read channel; it never raises stdin.
	DataInputBytes int `json:"dataInputBytes,omitempty"`
}
type Operation struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Input       ValueSchema      `json:"input"`
	Output      ValueSchema      `json:"output"`
	Roles       []string         `json:"roles"`
	Binding     OperationBinding `json:"binding"`
	Limits      OperationLimits  `json:"limits"`
}

func (o Operation) Check() error {
	if !functionName.MatchString(o.Name) || strings.TrimSpace(o.Title) == "" || len(o.Title) > 256 || len(o.Description) > 1024 || len(o.Roles) == 0 {
		return fmt.Errorf("operation needs a name, title and callable roles")
	}
	if err := o.Input.Check(); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if err := o.Output.Check(); err != nil {
		return fmt.Errorf("output: %w", err)
	}
	if o.Binding.Kind != "native" && o.Binding.Kind != "wasm" || o.Binding.Kind == "wasm" && (len(o.Binding.Module) != 64 || (o.Binding.ABI != WasmCommandABI && o.Binding.ABI != WasmDataABI)) || o.Binding.Kind == "native" && (o.Binding.Module != "" || o.Binding.ABI != "") {
		return fmt.Errorf("operation needs a native binding or pinned WASIp1 command")
	}
	if o.Binding.Kind == "wasm" {
		for _, c := range o.Binding.Module {
			if !strings.ContainsRune("0123456789abcdef", c) {
				return fmt.Errorf("invalid module digest")
			}
		}
	}
	if o.Limits.TimeoutMillis < 1 || o.Limits.TimeoutMillis > 30000 || o.Limits.MemoryPages < 1 || o.Limits.MemoryPages > 4096 || o.Limits.MaxInputBytes < 1 || o.Limits.MaxInputBytes > 1<<20 || o.Limits.MaxOutputBytes < 1 || o.Limits.MaxOutputBytes > 48<<10 {
		return fmt.Errorf("operation needs bounded time, memory and JSON bytes")
	}
	if o.Limits.StagedOutputBytes < 0 || o.Limits.StagedOutputBytes > 16<<20 ||
		o.Limits.StagedOutputBytes > 0 && o.Limits.StagedOutputBytes <= o.Limits.MaxOutputBytes {
		return fmt.Errorf("operation staged output must exceed the inline budget and stay within 16 MiB")
	}
	if o.Binding.ABI == WasmDataABI && o.Limits.StagedOutputBytes == 0 {
		return fmt.Errorf("the v2 data ABI needs a bounded call output channel")
	}
	if o.Limits.DataInputBytes < 0 || o.Limits.DataInputBytes > 64<<20 || o.Limits.DataInputBytes > 0 && (o.Binding.Kind != "wasm" || o.Binding.ABI != WasmDataABI || o.Limits.DataInputBytes <= o.Limits.MaxInputBytes) {
		return fmt.Errorf("operation data input requires the v2 read channel, above stdin and within 64 MiB")
	}
	seen := map[string]bool{}
	for _, r := range o.Roles {
		if r == "" || seen[r] {
			return fmt.Errorf("operation roles must be unique")
		}
		seen[r] = true
	}
	return nil
}

// OperationExecutor is implemented only by trusted code applications. It has
// no Caller and cannot use compute to obtain business submission privileges.
type OperationExecutor interface {
	Compute(context.Context, string, json.RawMessage) (json.RawMessage, error)
}
type OperationRequest struct {
	App      string          `json:"app,omitempty"`
	Name     string          `json:"name"`
	Version  int             `json:"version,omitempty"`
	Key      string          `json:"key"`
	Inputs   json.RawMessage `json:"inputs"`
	Sources  []string        `json:"sources,omitempty"`
	Target   string          `json:"target,omitempty"`
	OnBehalf string          `json:"onBehalf,omitempty"`
	Release  *string         `json:"release,omitempty"`
}
type OperationCall struct {
	OwnerVersion string   `json:"ownerVersion,omitempty"`
	ID           string   `json:"id"`
	Definition   string   `json:"definition"`
	InputHash    string   `json:"inputHash"`
	Version      int      `json:"version,omitempty"`
	Module       string   `json:"module,omitempty"`
	Sources      []string `json:"sources,omitempty"`
	Dependencies string   `json:"dependencies"`
	Release      string   `json:"release,omitempty"`
}
type OperationResult struct {
	OwnerVersion string          `json:"ownerVersion,omitempty"`
	Ref          *AssetRef       `json:"ref,omitempty"`
	Definition   string          `json:"definition,omitempty"`
	Version      int             `json:"version,omitempty"`
	Module       string          `json:"module,omitempty"`
	Dependencies string          `json:"dependencies,omitempty"`
	Release      string          `json:"release,omitempty"`
	ID           string          `json:"id"`
	State        string          `json:"state"`
	Output       json.RawMessage `json:"output,omitempty"`
	Error        string          `json:"error,omitempty"`
	Generation   uint32          `json:"generation"`
	Millis       int64           `json:"millis,omitempty"`
}

func (c Caller) RequestOperation(r *pb.ChangeRecord, q OperationRequest) (OperationCall, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		RequestOperation(Caller, *pb.ChangeRecord, OperationRequest) (OperationCall, *kernel.Error)
	}); ok {
		return rt.RequestOperation(c, r, q)
	}
	return OperationCall{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Operations require an accepted decision")
}
func (c Caller) OperationResult(id string) (OperationResult, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		OperationResult(Caller, string) (OperationResult, *kernel.Error)
	}); ok {
		return rt.OperationResult(c, id)
	}
	return OperationResult{}, notFound()
}

func (c Caller) CancelOperation(r *pb.ChangeRecord, id string) *kernel.Error {
	if rt, ok := c.rt.(interface {
		CancelOperation(Caller, *pb.ChangeRecord, string) *kernel.Error
	}); ok {
		return rt.CancelOperation(c, r, id)
	}
	return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Cancellation requires an accepted decision")
}

type CodeBuildRequest struct {
	ABI      string      `json:"abi,omitempty"`
	Language string      `json:"language"`
	Source   string      `json:"source"`
	Input    ValueSchema `json:"input"`
	Output   ValueSchema `json:"output"`
}
type CodeBuildResult struct {
	Module      string `json:"module,omitempty"`
	SourceHash  string `json:"sourceHash"`
	BuildHash   string `json:"buildHash"`
	Toolchain   string `json:"toolchain"`
	Diagnostics string `json:"diagnostics,omitempty"`
}

func (c Caller) RequestCompilation(r *pb.ChangeRecord, key, target string, q CodeBuildRequest) (OperationCall, *kernel.Error) {
	if rt, ok := c.rt.(interface {
		RequestCompilation(Caller, *pb.ChangeRecord, string, string, CodeBuildRequest) (OperationCall, *kernel.Error)
	}); ok {
		return rt.RequestCompilation(c, r, key, target, q)
	}
	return OperationCall{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Compilation requires an accepted decision")
}
