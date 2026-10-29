package transport

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"platformkernel/kernel"

	"google.golang.org/protobuf/proto"

	"platformserver/platform"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
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

func TypeScript(doc map[string]any, kernelModules map[string]string) string {
	defs := doc["components"].(map[string]any)["schemas"].(map[string]any)
	var names []string
	for name := range defs {
		if !strings.Contains(name, ":") {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var b strings.Builder
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
				fmt.Fprintf(&b, "\nexport type %s = %sJson;\n", name, name)
			} else {
				fmt.Fprintf(&b, "\nexport type %s = Record<string, unknown>;\n", name)
			}
			continue
		}
		fmt.Fprintf(&b, "\nexport type %s = %s;\n", name, tsOf(d, 0))
	}
	limits, _ := json.Marshal(map[string]int{"maxRegions": platform.MaxImageRegions, "maxLabelBytes": platform.MaxImageRegionLabelBytes, "maxIDBytes": platform.MaxImageRegionIDBytes})
	fmt.Fprintf(&b, "\nexport const imageRegionLimits = %s as const;\n", limits)
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

type Translator struct {
	base         platform.Languages
	apps         func() []platform.App
	manifest     func(platform.App) platform.Manifest
	tenant       func() map[string]map[string]string
	mu           sync.Mutex
	applications map[string]platform.Languages
	dictionaries map[dictionaryKey]*translationDictionary
}

type dictionaryKey struct{ app, lang string }
type translationDictionary struct {
	words    map[string]string
	once     sync.Once
	patterns []pattern
}

func (x *Translator) appManifest(a platform.App) platform.Manifest {
	if x.manifest != nil {
		return x.manifest(a)
	}
	return a.Manifest()
}

func (x *Translator) InstallApplications(definitions []platform.Definition) {
	applications := map[string]platform.Languages{}
	conflicts := map[string]platform.Languages{}
	for _, d := range definitions {
		if d.Application == nil {
			continue
		}
		if applications[d.Ref.App] == nil {
			applications[d.Ref.App] = platform.Languages{}
			conflicts[d.Ref.App] = platform.Languages{}
		}
		for lang, words := range d.Application.Languages {
			if applications[d.Ref.App][lang] == nil {
				applications[d.Ref.App][lang] = map[string]string{}
				conflicts[d.Ref.App][lang] = map[string]string{}
			}
			merged, ambiguous := applications[d.Ref.App][lang], conflicts[d.Ref.App][lang]
			for source, translation := range words {
				if ambiguous[source] != "" {
					continue
				}
				if previous, exists := merged[source]; exists && previous != translation {
					delete(merged, source)
					ambiguous[source] = "conflict"
				} else {
					merged[source] = translation
				}
			}
		}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.applications, x.dictionaries = applications, nil
}

type pattern struct {
	re    *regexp.Regexp
	names []string
	parts []string
	out   string
	fixed int
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

const maxSay = 8

func (x *Translator) Languages() []string {
	out := []string{}
	add := func(l platform.Languages) {
		for lang := range l {
			if !slices.Contains(out, lang) {
				out = append(out, lang)
			}
		}
	}
	add(x.base)
	for _, a := range x.apps() {
		add(x.appManifest(a).Languages)
	}
	x.mu.Lock()
	for _, languages := range x.applications {
		add(languages)
	}
	x.mu.Unlock()
	if x.tenant != nil {
		add(x.tenant())
	}
	slices.Sort(out)
	return out
}

func (x *Translator) Dictionary(lang string) map[string]string {
	return x.dictionary("", lang).words
}

func (x *Translator) dictionary(app, lang string) *translationDictionary {
	x.mu.Lock()
	defer x.mu.Unlock()
	key := dictionaryKey{app, lang}
	if cached := x.dictionaries[key]; cached != nil {
		return cached
	}
	words := map[string]string{}
	add := func(dict map[string]string) {
		for source, translation := range dict {
			words[source] = translation
		}
	}
	if lang != "" {
		add(x.base[lang])
		for _, a := range x.apps() {
			manifest := x.appManifest(a)
			add(manifest.Languages[lang])
			add(x.applications[manifest.ID][lang])
		}
		if app != "" {
			for _, a := range x.apps() {
				manifest := x.appManifest(a)
				if manifest.ID == app {
					add(manifest.Languages[lang])
					break
				}
			}
			add(x.applications[app][lang])
		}
		if x.tenant != nil {
			add(x.tenant()[lang])
		}
	}
	d := &translationDictionary{words: words}
	if x.dictionaries == nil {
		x.dictionaries = map[dictionaryKey]*translationDictionary{}
	}
	x.dictionaries[key] = d
	return d
}

func (x *Translator) Reset() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.dictionaries = nil
}

func (x *Translator) SayInApp(app, lang, s string) string {
	return x.say(app, lang, s, 0)
}

func (x *Translator) Translate(v any, lang string) any {
	return x.TranslateInApp(v, "", lang)
}

func (x *Translator) TranslateInApp(v any, app, lang string) any {
	if lang == "" {
		return v
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var tree any
	if json.Unmarshal(raw, &tree) != nil {
		return v
	}
	var presentation func(any, string)
	presentation = func(value any, owner string) {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				presentation(child, owner)
			}
		case map[string]any:
			if ref, ok := node["ref"].(map[string]any); ok {
				if name, ok := ref["app"].(string); ok {
					owner = name
				}
			}
			if application, ok := node["application"].(map[string]any); ok {
				if name, ok := application["app"].(string); ok {
					owner = name
				}
			}
			if name, ok := node["app"].(string); ok && name != "" {
				owner = name
			}
			if _, old := node["layout"].(string); old || node["format"] == float64(3) {
				if _, page := node["name"].(string); page {
					node["presentationWords"] = x.dictionary(owner, lang).words
				}
			}
			for key, child := range node {
				if key != "languages" && key != "widgetSpec" && key != "presentationWords" {
					presentation(child, owner)
				}
			}
		}
	}
	presentation(tree, app)
	walkTextsWithApp(tree, app, func(app, s string) string { return x.SayInApp(app, lang, s) })
	walkChoicesWithApp(tree, app, func(app string, m map[string]any, choices []any) {
		titles := make([]any, len(choices))
		for i, c := range choices {
			s, _ := c.(string)
			titles[i] = x.SayInApp(app, lang, s)
		}
		m["choiceTitles"] = titles
	})
	return tree
}

func (x *Translator) lookup(app, lang, s string) (string, bool) {
	dict := x.dictionary(app, lang).words
	if tr, ok := dict[s]; ok && tr != "" {
		return tr, true
	}
	if s != "" {
		if tr, ok := dict[strings.ToUpper(s[:1])+s[1:]]; ok && tr != "" {
			return tr, true
		}
	}
	return "", false
}

func (x *Translator) says(lang, s string, depth int) bool {
	return x.saysInApp("", lang, s, depth)
}

func (x *Translator) saysInApp(app, lang, s string, depth int) bool {
	if _, ok := x.lookup(app, lang, s); ok || depth > maxSay || !strings.ContainsFunc(s, unicode.IsLetter) {
		return ok || depth <= maxSay
	}
	for _, p := range x.patterns(app, lang) {
		if _, ok := x.split(app, lang, p, s, depth); ok {
			return true
		}
	}
	return false
}

func (x *Translator) split(app, lang string, p pattern, s string, depth int) ([]string, bool) {
	if !p.re.MatchString(s) {
		return nil, false
	}
	for _, values := range p.splits(s, 32) {
		if !slices.ContainsFunc(values, func(v string) bool { return !x.saysInApp(app, lang, v, depth+1) }) {
			return values, true
		}
	}
	return nil, false
}

func (x *Translator) patterns(app, lang string) []pattern {
	d := x.dictionary(app, lang)
	d.once.Do(func() {
		var out []pattern
		for key, tr := range d.words {
			idx := placeholder.FindAllStringSubmatchIndex(key, -1)
			if len(idx) == 0 {
				continue
			}
			expr, names, parts, last, fixed := "^", []string{}, []string{}, 0, 0
			for _, m := range idx {
				parts = append(parts, key[last:m[0]])
				expr += regexp.QuoteMeta(key[last:m[0]]) + "(.+)"
				fixed += m[0] - last
				names = append(names, key[m[2]:m[3]])
				last = m[1]
			}
			expr += regexp.QuoteMeta(key[last:]) + "$"
			parts = append(parts, key[last:])
			fixed += len(key) - last
			out = append(out, pattern{re: regexp.MustCompile("(?s)" + expr), names: names, parts: parts, out: tr, fixed: fixed})
		}
		slices.SortFunc(out, func(a, b pattern) int { return b.fixed - a.fixed })
		d.patterns = out
	})
	return d.patterns
}

func (x *Translator) Said(err *kernel.Error, lang string) *kernel.Error {
	if err == nil || err.Message == "" {
		return err
	}
	return &kernel.Error{Code: err.Code, Message: x.Say(lang, err.Message)}
}

func (x *Translator) Say(lang, s string) string { return x.say("", lang, s, 0) }

func (x *Translator) say(app, lang, s string, depth int) string {
	if lang == "" || s == "" || depth > maxSay {
		return s
	}
	if tr, ok := x.lookup(app, lang, s); ok {
		return tr
	}
	var p pattern
	var filled []string
	for _, q := range x.patterns(app, lang) {
		if found, ok := x.split(app, lang, q, s, depth); ok {
			p, filled = q, found
			break
		}
		if m := q.re.FindStringSubmatch(s); m != nil && filled == nil {
			p, filled = q, m[1:]
		}
	}
	if filled != nil {
		values := map[string]string{}
		for i, name := range p.names {
			values[name] = x.say(app, lang, filled[i], depth+1)
		}
		return placeholder.ReplaceAllStringFunc(p.out, func(x string) string {
			if v, ok := values[x[1:len(x)-1]]; ok {
				return v
			}
			return x
		})
	}
	return s
}

func (tr *Translator) TranslateMessages(v any, lang string) any {
	if lang == "" {
		return v
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var tree any
	if json.Unmarshal(raw, &tree) != nil {
		return v
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if answers, ok := x["answers"].([]any); ok {
				titles := make([]any, len(answers))
				for i, a := range answers {
					s, _ := a.(string)
					titles[i] = tr.Say(lang, s)
				}
				x["answerTitles"] = titles
			}
			for k, child := range x {
				if s, ok := child.(string); ok && (k == "title" || k == "body") {
					x[k] = tr.Say(lang, s)
					continue
				}
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(tree)
	return tree
}

func NewTranslator(apps func() []platform.App, manifest func(platform.App) platform.Manifest, tenant func() map[string]map[string]string, base platform.Languages) Translator {
	return Translator{apps: apps, manifest: manifest, tenant: tenant, base: base}
}
func (x *Translator) CanSay(lang, text string) bool { return x.says(lang, text, 0) }

func WalkChoices(v any, f func(map[string]any, []any)) {
	walkChoicesWithApp(v, "", func(_ string, m map[string]any, c []any) { f(m, c) })
}

func walkChoicesWithApp(v any, currentApp string, f func(string, map[string]any, []any)) {
	switch x := v.(type) {
	case map[string]any:
		app := currentApp
		if ref, ok := x["ref"].(map[string]any); ok {
			if owner, ok := ref["app"].(string); ok {
				app = owner
			}
		}
		if application, ok := x["application"].(map[string]any); ok {
			if owner, ok := application["app"].(string); ok {
				app = owner
			}
		}
		if a, ok := x["app"].(string); ok && a != "" {
			app = a
		} else if target, ok := x["target"].(string); ok && target != "" && strings.Contains(target, ".") {
			app, _, _ = strings.Cut(target, ".")
		} else if schema, ok := x["schema"].(string); ok && schema != "" && strings.Contains(schema, ".") {
			app, _, _ = strings.Cut(schema, ".")
		}
		if choices, ok := x["choices"].([]any); ok {
			f(app, x, choices)
		}
		for key, child := range x {
			if key == "languages" || key == "widgetSpec" || key == "presentationWords" {
				continue
			}
			walkChoicesWithApp(child, app, f)
		}
	case []any:
		for _, child := range x {
			walkChoicesWithApp(child, currentApp, f)
		}
	}
}

func WalkTexts(v any, f func(string) string) {
	walkTextsWithApp(v, "", func(_ string, s string) string { return f(s) })
}

func walkTextsWithApp(v any, currentApp string, f func(string, string) string) {
	switch x := v.(type) {
	case map[string]any:
		app := currentApp
		if ref, ok := x["ref"].(map[string]any); ok {
			if owner, ok := ref["app"].(string); ok {
				app = owner
			}
		}
		if application, ok := x["application"].(map[string]any); ok {
			if owner, ok := application["app"].(string); ok {
				app = owner
			}
		}
		if a, ok := x["app"].(string); ok && a != "" {
			app = a
		} else if target, ok := x["target"].(string); ok && target != "" && strings.Contains(target, ".") {
			app, _, _ = strings.Cut(target, ".")
		} else if schema, ok := x["schema"].(string); ok && schema != "" && strings.Contains(schema, ".") {
			app, _, _ = strings.Cut(schema, ".")
		}
		for k, child := range x {
			if k == "languages" || k == "widgetSpec" || k == "presentationWords" {
				continue
			}
			if s, ok := child.(string); ok && translated[k] {
				x[k] = f(app, s)
				continue
			}
			walkTextsWithApp(child, app, f)
		}
	case []any:
		for _, child := range x {
			walkTextsWithApp(child, currentApp, f)
		}
	}
}

func (p pattern) splits(s string, limit int) [][]string {
	var out [][]string
	var fill func(rest string, i int, values []string)
	fill = func(rest string, i int, values []string) {
		if len(out) >= limit {
			return
		}
		lit := p.parts[i+1]
		if i == len(p.names)-1 {
			if v, ok := strings.CutSuffix(rest, lit); ok && v != "" {
				out = append(out, append(slices.Clone(values), v))
			}
			return
		}
		for j := 1; j < len(rest); j++ {
			if strings.HasPrefix(rest[j:], lit) {
				fill(rest[j+len(lit):], i+1, append(values, rest[:j]))
			}
		}
	}
	if rest, ok := strings.CutPrefix(s, p.parts[0]); ok && len(p.names) > 0 {
		fill(rest, 0, nil)
	}
	return out
}

var translated = map[string]bool{"title": true, "plural": true, "description": true, "help": true, "synonyms": true}

type ViewCache struct {
	mu    sync.Mutex
	gen   uint64
	entry map[string]*viewEntry
}

const maxViewEntries = 8

func Rights(m platform.Member) string {
	raw, _ := json.Marshal(m)
	return strconv.FormatBool(m.AIContext) + string(raw)
}

func (c *ViewCache) settle(gen uint64) bool {
	if c.gen == gen {
		return true
	}
	c.gen, c.entry = gen, nil
	return false
}

func (c *ViewCache) store(gen uint64, key string, value func(*viewEntry)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) || c.entry == nil && gen == 0 {
		return
	}
	if c.entry == nil {
		c.entry = map[string]*viewEntry{}
	}
	if len(c.entry) >= maxViewEntries {
		if _, ok := c.entry[key]; !ok {
			c.entry = map[string]*viewEntry{}
		}
	}
	e := c.entry[key]
	if e == nil {
		e = &viewEntry{}
		c.entry[key] = e
	}
	value(e)
}

func (c *ViewCache) Catalog(gen uint64, key string) ([]platform.Action, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.catalog != nil {
		return e.catalog, true
	}
	return nil, false
}

func (c *ViewCache) StoreCatalog(gen uint64, key string, value []platform.Action) {
	c.store(gen, key, func(e *viewEntry) { e.catalog = value })
}

func (c *ViewCache) Entities(gen uint64, key string) ([]platform.EntityInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.entities != nil {
		return e.entities, true
	}
	return nil, false
}

func (c *ViewCache) StoreEntities(gen uint64, key string, value []platform.EntityInfo) {
	c.store(gen, key, func(e *viewEntry) { e.entities = value })
}

func (c *ViewCache) Definitions(gen uint64, key string) ([]platform.Definition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.definitions != nil {
		return e.definitions, true
	}
	return nil, false
}

func (c *ViewCache) StoreDefinitions(gen uint64, key string, value []platform.Definition) {
	c.store(gen, key, func(e *viewEntry) { e.definitions = value })
}

func (c *ViewCache) body(gen uint64, key, bodyKey string) (*viewBody, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.bodies != nil {
		if body, ok := e.bodies[bodyKey]; ok {
			return body, true
		}
	}
	return nil, false
}

func (c *ViewCache) putBody(gen uint64, key, bodyKey string, body *viewBody) {
	c.store(gen, key, func(e *viewEntry) {
		if e.bodies == nil {
			e.bodies = map[string]*viewBody{}
		}
		e.bodies[bodyKey] = body
	})
}

var manifestCache sync.Map

type manifestEntry struct {
	gen      uint64
	manifest platform.Manifest
}

var manifestCacheSize atomic.Int64

func ManifestOf(a platform.App, gen uint64) platform.Manifest {
	if key := reflect.ValueOf(a); !key.IsValid() || !key.Type().Comparable() {
		return a.Manifest()
	}
	if entry, ok := manifestCache.Load(a); ok {
		if cached := entry.(manifestEntry); cached.gen == gen {
			return cached.manifest
		}
	}
	manifest := a.Manifest()
	if manifestCacheSize.Add(1) > 512 {
		manifestCache.Range(func(key, _ any) bool { manifestCache.Delete(key); return true })
		manifestCacheSize.Store(1)
	}
	manifestCache.Store(a, manifestEntry{gen: gen, manifest: manifest})
	return manifest
}

type viewBody struct {
	plain []byte
	etag  string
	once  sync.Once
	gz    []byte
}

func (b *viewBody) compressed() []byte {
	b.once.Do(func() {
		var buf bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&buf, 6)
		_, _ = writer.Write(b.plain)
		_ = writer.Close()
		b.gz = buf.Bytes()
	})
	return b.gz
}

type viewEntry struct {
	entities    []platform.EntityInfo
	catalog     []platform.Action
	definitions []platform.Definition
	bodies      map[string]*viewBody
}

func (c *ViewCache) cachedView(gen uint64, key, kind, lang string, source func() any, translate func(any, string) any) *viewBody {
	bodyKey := kind + ":" + lang
	if body, ok := c.body(gen, key, bodyKey); ok {
		return body
	}
	value := source()
	if lang != "" {
		value = translate(value, lang)
	}
	plain, err := json.Marshal(value)
	if err != nil {
		plain = []byte("null")
	}
	sum := sha256.Sum256(plain)
	body := &viewBody{plain: plain, etag: `"` + hex.EncodeToString(sum[:16]) + `"`}
	c.putBody(gen, key, bodyKey, body)
	return body
}

func (c *ViewCache) WriteView(gen uint64, key, kind, lang string, w http.ResponseWriter, r *http.Request, source func() any, translate func(any, string) any) {
	body := c.cachedView(gen, key, kind, lang, source, translate)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", body.etag)
	if match := r.Header.Get("If-None-Match"); match != "" && contentTypeMatches(match, body.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if noCompression, ok := w.(interface{ NoCompression() }); ok {
		noCompression.NoCompression()
	}
	if AcceptsGzip(r) {
		compressed := body.compressed()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", itoa(len(compressed)))
		_, _ = w.Write(compressed)
		return
	}
	w.Header().Set("Content-Length", itoa(len(body.plain)))
	_, _ = w.Write(body.plain)
}

func contentTypeMatches(header, etag string) bool {
	for _, candidate := range bytes.Split([]byte(header), []byte(",")) {
		if bytes.Equal(bytes.TrimSpace(candidate), []byte(etag)) {
			return true
		}
	}
	return false
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
