package platformserver

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// The host API contract (ADR-0023 D7). The Go types are the source: each
// route is declared once with its handler, the body it takes and the answer
// it gives, and the host describes them as OpenAPI 3.1 at /v1/openapi.json.
// The TypeScript types of the web edge are generated from the same document
// (cmd/api-types), never written by hand. Per tenant, the document also
// describes each entity type the member may read and each action's payload.

// Route is one route of the host's HTTP API.
type Route struct {
	Pattern string // "GET /v1/records/{type}"
	Summary string
	Query   []Param
	Body    any  // the request body's Go type as a zero value; nil: none
	Answer  any  // the answer's Go type as a zero value; nil: any JSON
	Public  bool // no bearer token
}

// Param is a query parameter.
type Param struct{ Name, Description string }

// namedReads document the platform apps' reads, all served by GET /v1/{read}:
// the contract names each with the Go type it answers with.
var namedReads = []Route{
	{Pattern: "GET /v1/notifications", Summary: "The caller's notifications, newest first, in their language", Answer: []platform.Notification{}},
	{Pattern: "GET /v1/members", Summary: "The tenant's members with their roles (administrators)", Answer: []MemberView{}},
	{Pattern: "GET /v1/audit", Summary: "Accepted inputs, newest first, rebuilt from the journal (administrators)", Answer: []AuditEntry{}},
	{Pattern: "GET /v1/deliveries", Summary: "Delivery attempts of events to subscribers (administrators)", Answer: []Delivery{}},
	{Pattern: "GET /v1/work", Summary: "Owned work: deliveries and jobs (administrators)", Answer: []Task{}},
	{Pattern: "GET /v1/connectors", Summary: "Connectors with their health and cursor (administrators)", Answer: []ConnectorView{}},
	{Pattern: "GET /v1/settings", Summary: "Every app's settings with their values, in the caller's language (administrators)", Answer: []AppSettings{}},
	{Pattern: "GET /v1/endpoints", Summary: "Outbound endpoints (administrators)", Answer: []EndpointView{}},
	{Pattern: "GET /v1/effects", Summary: "Outbound effects and their state (administrators)", Answer: []platform.Effect{}},
	{Pattern: "GET /v1/inbox", Summary: "Tasks offered to the caller, overdue first, in their language", Answer: []InboxTask{}},
	{Pattern: "GET /v1/requests", Summary: "The caller's approval requests", Answer: []ApprovalRequest{}},
	{Pattern: "GET /v1/views", Summary: "The caller's saved views", Answer: []SavedView{}},
	{Pattern: "GET /v1/agents", Summary: "The agents the apps declare", Answer: []AgentInfo{}},
	{Pattern: "GET /v1/runs", Summary: "Agent runs on the caller's behalf", Answer: []AgentRunRecord{}},
	{Pattern: "GET /v1/memories", Summary: "What agents remember about the caller", Answer: []Memory{}},
	{Pattern: "GET /v1/ai-providers", Summary: "AI providers (AI administrators)", Answer: []Provider{}},
	{Pattern: "GET /v1/ai-models", Summary: "Models the caller may call, or all for AI administrators", Answer: []Model{}},
	{Pattern: "GET /v1/ai-usage", Summary: "Model calls and their totals", Answer: AIUsage{}},
	{Pattern: "GET /v1/organization", Summary: "The organisation's structures, units, edges and memberships", Answer: platform.OrgSeed{}},
	{Pattern: "GET /v1/flows", Summary: "The flows the apps declare, in the caller's language", Answer: []FlowDefinition{}},
	{Pattern: "GET /v1/links", Summary: "Links between entities", Answer: []Link{}},
	{Pattern: "GET /v1/timeline", Summary: "Notes on entities' timelines", Answer: []Note{}},
}

// InboxTask is a task as the inbox serves it: with its answers in the
// reader's language, beside the values submitted.
type InboxTask struct {
	WorkTask
	AnswerTitles []string `json:"answerTitles,omitempty"`
}

// SubmissionAnswer is what a decision's route answers: the accepted change
// record, or an error with the code the kernel's errors name (contract/spec/errors.md).
type SubmissionAnswer struct {
	Record *pb.ChangeRecord `json:"record,omitempty"`
	Error  *ErrorBody       `json:"error,omitempty"`
}

type ErrorBody struct {
	Code   string `json:"code"`
	Status int    `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// ProtocolCall is the body of a call to a protocol's action.
type ProtocolCall struct {
	Target         string          `json:"target"`
	IdempotencyKey string          `json:"idempotencyKey"`
	Payload        json.RawMessage `json:"payload"`
}

// MeView is who the caller is on this host, and what they may open.
type MeView struct {
	TenantID    string          `json:"tenantId"`
	PrincipalID string          `json:"principalId"`
	Profile     platform.Member `json:"profile"`
	Apps        []AppEntry      `json:"apps"`
	Tenants     []string        `json:"tenants"`
	Language    string          `json:"language"`            // the language the host serves this member in; "" is English
	Languages   []string        `json:"languages"`           // the languages the tenant has dictionaries for
	Preferred   string          `json:"preferred,omitempty"` // the member's own choice
}

// SignIn tells the workspace how to sign in: an OpenID issuer and client, or
// development identities.
type SignIn struct {
	Issuer     string     `json:"issuer,omitempty"`
	Client     string     `json:"client,omitempty"`
	Identities []Identity `json:"identities,omitempty"`
}

// OpenAPI describes the host's routes; with a tenant and a member, it also
// describes the entity types and action payloads that member sees.
func (h *Host) OpenAPI(t *Tenant, m *platform.Member) map[string]any {
	s := newSchemas()
	paths := map[string]any{}
	for _, r := range h.routes {
		method, path, _ := strings.Cut(r.Pattern, " ")
		op := map[string]any{"summary": r.Summary, "operationId": operationID(method, path)}
		var params []any
		for _, p := range pathParams.FindAllStringSubmatch(path, -1) {
			params = append(params, map[string]any{"name": p[1], "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
		for _, q := range r.Query {
			params = append(params, map[string]any{"name": q.Name, "in": "query", "description": q.Description, "schema": map[string]any{"type": "string"}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if r.Body != nil {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": s.of(reflect.TypeOf(r.Body))}}}
		}
		answer := map[string]any{}
		if r.Answer != nil {
			answer = s.of(reflect.TypeOf(r.Answer))
		}
		op["responses"] = map[string]any{
			"200":     map[string]any{"description": "OK", "content": map[string]any{"application/json": map[string]any{"schema": answer}}},
			"default": map[string]any{"description": "Refused", "content": map[string]any{"application/json": map[string]any{"schema": s.of(reflect.TypeFor[SubmissionAnswer]())}}},
		}
		if !r.Public {
			op["security"] = []any{map[string]any{"bearer": []any{}}}
		}
		item, _ := paths[path].(map[string]any)
		if item == nil {
			item = map[string]any{}
			paths[path] = item
		}
		item[strings.ToLower(method)] = op
	}
	if t != nil && m != nil {
		for _, e := range t.Entities(*m) {
			s.defs["entity:"+e.Type] = entitySchema(e)
		}
		for _, a := range t.Catalog(*m) {
			s.defs["payload:"+a.Schema] = payloadSchema(a)
		}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "Platform host API", "version": "v1",
			"description": "Generated from the host's Go types (ADR-0023 D7). Decisions are submissions of the kernel contract (contract/proto, in their Protobuf JSON form); their meaning is contract/spec."},
		"paths": paths,
		"components": map[string]any{"schemas": s.defs,
			"securitySchemes": map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer",
				"description": "An access token of the host's OpenID issuer, or a development token"}}},
	}
}

var pathParams = regexp.MustCompile(`\{(\w+)\}`)

func operationID(method, path string) string {
	id := strings.ToLower(method)
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		part = strings.Trim(part, "{}")
		part = strings.NewReplacer(".", "_", "-", "_").Replace(part)
		if part != "" && part != "v1" {
			id += strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return id
}

// schemas turns Go types into JSON Schema, naming each struct once.
type schemas struct {
	defs  map[string]any
	names map[reflect.Type]string
}

func newSchemas() *schemas { return &schemas{defs: map[string]any{}, names: map[reflect.Type]string{}} }

var (
	rawType     = reflect.TypeFor[json.RawMessage]()
	messageType = reflect.TypeFor[proto.Message]()
)

// of is the schema of a Go type: a reference to a named struct, or inline.
func (s *schemas) of(t reflect.Type) map[string]any {
	switch {
	case t == rawType:
		return map[string]any{}
	case t == reflect.TypeFor[time.Time]():
		return map[string]any{"type": "string", "format": "date-time"}
	case t.Kind() == reflect.Pointer:
		if t.Implements(messageType) {
			return s.message(t.Elem())
		}
		return s.of(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "contentEncoding": "base64"}
		}
		return map[string]any{"type": "array", "items": s.of(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": s.of(t.Elem())}
	case reflect.Struct:
		if reflect.PointerTo(t).Implements(messageType) {
			return s.message(t)
		}
		return s.ref(t)
	}
	return map[string]any{} // interfaces: any JSON
}

// message is a kernel message, in its Protobuf JSON form: the contract's, not the host's.
func (s *schemas) message(t reflect.Type) map[string]any {
	name := t.Name()
	s.defs[name] = map[string]any{"type": "object", "additionalProperties": true, "x-protobuf": "platform.kernel.v1alpha1." + name,
		"description": "The kernel contract's " + name + " in its Protobuf JSON form (contract/proto)"}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func (s *schemas) ref(t reflect.Type) map[string]any {
	name, ok := s.names[t]
	if !ok {
		name = t.Name()
		if name == "" || strings.Contains(name, "[") {
			return s.object(t)
		}
		if _, taken := s.defs[name]; taken { // two packages name a type alike: the package's name tells them apart
			pkg := t.PkgPath()[strings.LastIndex(t.PkgPath(), "/")+1:]
			name = strings.ToUpper(pkg[:1]) + pkg[1:] + name
		}
		s.names[t] = name
		s.defs[name] = nil // reserved while its fields refer back to it
		s.defs[name] = s.object(t)
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// object is a struct's schema: its JSON fields, embedded structs merged.
func (s *schemas) object(t reflect.Type) map[string]any {
	props, required, order := map[string]any{}, []string{}, []string{}
	var add func(t reflect.Type)
	add = func(t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			if tag == "-" || !f.IsExported() && !f.Anonymous || f.Type.Kind() == reflect.Func || f.Type.Kind() == reflect.Chan {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if f.Anonymous && name == "" {
				ft := f.Type
				if ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					add(ft)
					continue
				}
			}
			if name == "" {
				name = f.Name
			}
			p := s.of(f.Type)
			if enum := f.Tag.Get("enum"); enum != "" {
				p = map[string]any{"type": "string", "enum": strings.Split(enum, ",")}
			} else if choices := f.Tag.Get("choices"); choices != "" && f.Type.Kind() == reflect.String {
				p = map[string]any{"type": "string", "enum": strings.Split(choices, ",")}
			}
			if _, seen := props[name]; !seen {
				order = append(order, name)
			}
			props[name] = p
			if !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero") {
				required = append(required, name)
			}
		}
	}
	add(t)
	out := map[string]any{"type": "object", "properties": props, "x-order": order}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// entitySchema is a tenant's entity type as JSON Schema: its record's fields.
func entitySchema(e platform.EntityInfo) map[string]any {
	props := map[string]any{"id": map[string]any{"type": "string"}, "revision": map[string]any{"type": "integer"}}
	order := []string{"id", "revision"}
	var required []string
	for _, f := range e.Fields {
		p := map[string]any{"title": f.Title}
		switch f.Type {
		case "integer":
			p["type"] = "integer"
		case "decimal":
			p["type"] = "number"
		case "boolean":
			p["type"] = "boolean"
		case "references", "tags":
			p["type"], p["items"] = "array", map[string]any{"type": "string"}
		case "lines":
			p["type"] = "array"
		case "money":
			p["type"], p["properties"] = "object", map[string]any{"amount": map[string]any{"type": "integer"}, "currency": map[string]any{"type": "string"}}
		case "date":
			p["type"], p["format"] = "string", "date"
		case "datetime":
			p["type"], p["format"] = "string", "date-time"
		default:
			p["type"] = "string"
		}
		if len(f.Choices) > 0 {
			p["enum"] = f.Choices
		}
		if f.Help != "" {
			p["description"] = f.Help
		}
		if f.Example != "" {
			p["examples"] = []string{f.Example}
		}
		if f.Required {
			required = append(required, f.Name)
		}
		props[f.Name] = p
		order = append(order, f.Name)
	}
	out := map[string]any{"type": "object", "title": e.Title, "properties": props, "x-order": order, "x-entity": e.Type}
	if e.Description != "" {
		out["description"] = e.Description
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// payloadSchema is an action's payload as JSON Schema.
func payloadSchema(a platform.Action) map[string]any {
	props, order, required := map[string]any{}, []string{}, []string{}
	for _, f := range a.Payload {
		p := map[string]any{"description": f.Description}
		switch f.Type {
		case "integer":
			p["type"] = "integer"
		case "number":
			p["type"] = "number"
		case "boolean":
			p["type"] = "boolean"
		case "string[]":
			p["type"], p["items"] = "array", map[string]any{"type": "string"}
		case "money":
			p["type"] = "object"
		case "date":
			p["type"], p["format"] = "string", "date"
		case "datetime":
			p["type"], p["format"] = "string", "date-time"
		default:
			p["type"] = "string"
		}
		props[f.Name] = p
		order = append(order, f.Name)
		if f.Required {
			required = append(required, f.Name)
		}
	}
	out := map[string]any{"type": "object", "title": a.Title, "description": a.Description, "properties": props, "x-order": order,
		"x-action": a.Schema, "x-target": a.Target}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

// TypeScript is the TypeScript of an OpenAPI document's named schemas, for
// the web edge: one exported type per Go struct the host answers with. Kernel
// messages are the contract's own generated types (their *Json form),
// imported from the modules that export them.
func TypeScript(doc map[string]any, kernelModules map[string]string) string {
	defs := doc["components"].(map[string]any)["schemas"].(map[string]any)
	var names []string
	for name := range defs {
		if !strings.Contains(name, ":") { // per-tenant schemas are not part of the static contract
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var b strings.Builder
	b.WriteString("// Generated by capabilities/server/cmd/api-types from the host's Go types (ADR-0023 D7). Do not edit:\n")
	b.WriteString("// change the Go types, then run `go run ./cmd/api-types` in capabilities/server.\n")
	imports := map[string][]string{}
	for _, name := range names {
		if d, _ := defs[name].(map[string]any); d != nil && d["x-protobuf"] != nil {
			if module := kernelModules[name]; module != "" {
				imports[module] = append(imports[module], name+"Json")
			}
		}
	}
	var modules []string
	for m := range imports {
		modules = append(modules, m)
	}
	slices.Sort(modules)
	for _, m := range modules {
		slices.Sort(imports[m])
		fmt.Fprintf(&b, "import type { %s } from %q;\n", strings.Join(imports[m], ", "), m)
	}
	for _, name := range names {
		d, _ := defs[name].(map[string]any)
		if d["x-protobuf"] != nil {
			if kernelModules[name] != "" {
				fmt.Fprintf(&b, "\n/** The kernel contract's %s (contract/proto). */\nexport type %s = %sJson;\n", name, name, name)
			} else {
				fmt.Fprintf(&b, "\n/** The kernel contract's %s (contract/proto). */\nexport type %s = Record<string, unknown>;\n", name, name)
			}
			continue
		}
		fmt.Fprintf(&b, "\nexport type %s = %s;\n", name, tsOf(d, 0))
	}
	return b.String()
}

func tsOf(s map[string]any, depth int) string {
	if ref, ok := s["$ref"].(string); ok {
		return strings.TrimPrefix(ref, "#/components/schemas/")
	}
	if enum, ok := s["enum"].([]string); ok {
		var parts []string
		for _, e := range enum {
			parts = append(parts, fmt.Sprintf("%q", e))
		}
		return strings.Join(parts, " | ")
	}
	switch s["type"] {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		items, _ := s["items"].(map[string]any)
		inner := tsOf(items, depth)
		if strings.Contains(inner, " | ") {
			inner = "(" + inner + ")"
		}
		return inner + "[]"
	case "object":
		if extra, ok := s["additionalProperties"].(map[string]any); ok {
			return "Record<string, " + tsOf(extra, depth) + ">"
		}
		props, _ := s["properties"].(map[string]any)
		order, _ := s["x-order"].([]string)
		required, _ := s["required"].([]string)
		indent := strings.Repeat("  ", depth+1)
		var b strings.Builder
		b.WriteString("{\n")
		for _, name := range order {
			opt := "?"
			if slices.Contains(required, name) {
				opt = ""
			}
			fmt.Fprintf(&b, "%s%s%s: %s;\n", indent, tsKey(name), opt, tsOf(props[name].(map[string]any), depth+1))
		}
		b.WriteString(strings.Repeat("  ", depth) + "}")
		return b.String()
	}
	return "unknown"
}

func tsKey(name string) string {
	if regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`).MatchString(name) {
		return name
	}
	return fmt.Sprintf("%q", name)
}

// KernelModules finds, in a directory of the contract's generated TypeScript,
// the module that exports each kernel message's JSON type (<Name>Json), as an
// import path relative to that directory.
func KernelModules(dir string) map[string]string {
	out := map[string]string{}
	exported := regexp.MustCompile(`(?m)^export type (\w+)Json =`)
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_pb.ts") {
			return nil
		}
		raw, _ := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, strings.TrimSuffix(path, ".ts"))
		for _, m := range exported.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = "./" + filepath.ToSlash(rel)
		}
		return nil
	})
	return out
}
