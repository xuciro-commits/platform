package transport

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"platformserver/platform"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type Route struct {
	Pattern string
	Summary string
	Query   []Param
	Body    any
	Answer  any
	Public  bool
}

type Param struct{ Name, Description string }

func OpenAPI(routes []Route, retained []reflect.Type, refusal reflect.Type, entities []platform.EntityInfo, actions []platform.Action) map[string]any {
	s := newSchemas()
	for _, typ := range retained {
		s.of(typ)
	}
	paths := map[string]any{}
	for _, r := range routes {
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
			"default": map[string]any{"description": "Refused", "content": map[string]any{"application/json": map[string]any{"schema": s.of(refusal)}}},
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
	for _, e := range entities {
		s.defs["entity:"+e.Type] = entitySchema(e)
	}
	for _, a := range actions {
		s.defs["payload:"+a.Schema] = payloadSchema(a)
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{"title": "Platform host API", "version": "v1",
			"description": "Generated from the host's Go types. Decisions use the Protobuf JSON kernel contract in contract/proto."},
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

type schemas struct {
	defs  map[string]any
	names map[reflect.Type]string
}

func newSchemas() *schemas { return &schemas{defs: map[string]any{}, names: map[reflect.Type]string{}} }

var (
	rawType     = reflect.TypeFor[json.RawMessage]()
	messageType = reflect.TypeFor[proto.Message]()
)

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
	return map[string]any{}
}

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
		switch t.PkgPath() {
		case "platformserver/platform":
			if public := map[string]string{
				"AIVendor": "Vendor", "AIProvider": "Provider", "AIModel": "Model", "AILimit": "Limit",
				"AIUsage": "Usage", "AITotal": "Total", "AIUsageReport": "AIUsage",
				"FlowToken": "Token", "FlowLoopFrame": "LoopFrame", "FlowTraceLine": "TraceLine",
				"RelationLink": "Link", "RelationNote": "Note",
				"LoginIdentity": "Identity",
			}[name]; public != "" {
				name = public
			}
		case "platformserver/engine/organization":
			if name == "Model" {
				name = "EnterpriseModel"
			}
		}
		if name == "" || strings.Contains(name, "[") {
			return s.object(t)
		}
		if _, taken := s.defs[name]; taken {
			pkg := t.PkgPath()[strings.LastIndex(t.PkgPath(), "/")+1:]
			name = strings.ToUpper(pkg[:1]) + pkg[1:] + name
		}
		s.names[t] = name
		s.defs[name] = nil
		s.defs[name] = s.object(t)
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func (s *schemas) object(t reflect.Type) map[string]any {
	props, required, order := map[string]any{}, []string{}, []string{}
	var add func(t reflect.Type)
	add = func(t reflect.Type) {
		for i := range t.NumField() {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			if f.Tag.Get("api") == "retained" || tag == "-" || !f.IsExported() && !f.Anonymous || f.Type.Kind() == reflect.Func || f.Type.Kind() == reflect.Chan {
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
		case "attachment":
			p = attachmentSchema(p)
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
		case "attachment":
			p = attachmentSchema(p)
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

func itoa(n int) string {
	var digits [20]byte
	i := len(digits)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func attachmentSchema(schema map[string]any) map[string]any {
	schema["type"] = "object"
	schema["properties"] = map[string]any{"mediaSet": map[string]any{"type": "string", "minLength": 1}, "item": map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}}
	schema["required"] = []string{"mediaSet", "item"}
	schema["additionalProperties"] = false
	return schema
}
