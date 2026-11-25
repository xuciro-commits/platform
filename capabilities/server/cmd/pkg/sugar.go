package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"platformserver/platform"
	packagedesc "platformserver/services/devops/descriptor"
	"platformserver/services/resources/publication"
)

const localesDir = "locales"

func objectFields(body *object) map[string]exprField {
	out := map[string]exprField{"id": {kind: "text"}, "state": {kind: "choice"}}
	list, _ := body.get("fields")
	items, _ := list.([]any)
	for _, item := range items {
		f := asObject(item)
		if f == nil {
			continue
		}
		out[text(f, "name")] = exprField{kind: fieldKind(text(f, "type")), ref: text(f, "ref")}
	}
	return out
}

func compileAt(value any, s exprScope, want, at string) (any, error) {
	src, ok := value.(string)
	if !ok {
		return value, nil
	}
	out, err := compileExpr(src, s, want)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", at, err)
	}
	return out, nil
}

func expandSets(body *object) {
	value, ok := body.get("sets")
	declared := asObject(value)
	if !ok || declared == nil {
		return
	}
	list := []any{}
	collections := &object{}
	for _, name := range declared.order() {
		entry := &object{}
		spec := asObject(declared.values[name])
		target, _ := declared.values[name].(string)
		from, limit := name, any(float64(200))
		if spec != nil {
			target = text(spec, "object")
			if f := text(spec, "from"); f != "" {
				from = f
			}
			if l, ok := spec.get("limit"); ok {
				limit = l
			}
		}
		order := &object{}
		id := exprObject("", "text")
		id.set("ref", "id")
		order.set("expr", id)
		entry.set("direction", "inverse")
		entry.set("from", from)
		entry.set("limit", limit)
		entry.set("name", name)
		entry.set("object", target)
		entry.set("order", []any{order})
		list = append(list, entry)
		read := &object{}
		read.set("app", "build")
		read.set("object", target)
		read.set("policy", "complete")
		read.set("query", "")
		read.set("set", name)
		read.set("version", float64(0))
		collections.set(name, read)
	}
	body.set("sets", list)
	if _, ok := body.get("reads"); !ok {
		reads := &object{}
		reads.set("collections", collections)
		body.set("reads", reads)
	}
}

func setNames(body *object) map[string]string {
	out := map[string]string{}
	value, _ := body.get("sets")
	list, _ := value.([]any)
	for _, item := range list {
		if set := asObject(item); set != nil {
			out[text(set, "name")] = text(set, "object")
		}
	}
	return out
}

func expandObject(body *object, path string, definitions map[string]*object, types map[string]map[string]exprField) error {
	self := refName("object", body)
	if !body.has("type") {
		body.set("type", self)
	}
	expandSets(body)
	s := exprScope{objects: types, self: self, sets: setNames(body)}
	list, _ := body.get("fields")
	fields, _ := list.([]any)
	for _, item := range fields {
		f := asObject(item)
		if f == nil {
			continue
		}
		kind := fieldKind(text(f, "type"))
		if formula, ok := f.get("formula"); ok {
			f.remove("formula")
			f.set("formulaExpr", formula)
		}
		for _, key := range []string{"from", "formulaExpr"} {
			if value, ok := f.get(key); ok {
				compiled, err := compileAt(value, s, kind, path+": field "+text(f, "name")+" "+key)
				if err != nil {
					return err
				}
				f.set(key, compiled)
			}
		}
	}
	list, _ = body.get("constraints")
	constraints, _ := list.([]any)
	for _, item := range constraints {
		if c := asObject(item); c != nil {
			if value, ok := c.get("expr"); ok {
				compiled, err := compileAt(value, s, "boolean", path+": constraint "+text(c, "name"))
				if err != nil {
					return err
				}
				c.set("expr", compiled)
			}
		}
	}
	if value, ok := body.get("createTransaction"); ok {
		creation := &object{}
		creation.set("transaction", value)
		if err := expandAction(creation, s, path+": createTransaction"); err != nil {
			return err
		}
		compiled, _ := creation.get("transaction")
		body.set("createTransaction", compiled)
	}
	value, declared := body.get("actions")
	actions, _ := value.([]any)
	if !declared {
		names := make([]string, 0, len(definitions))
		for name := range definitions {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			actions = append(actions, name)
		}
	}
	used := map[string]bool{}
	for i, item := range actions {
		if name, ok := item.(string); ok {
			definition, found := definitions[name]
			if !found {
				return fmt.Errorf("%s: action %s has no file actions/%s.yaml", path, name, name)
			}
			if !definition.has("name") {
				definition.set("name", name)
			}
			actions[i], used[name] = definition, true
		}
		action := asObject(actions[i])
		if action == nil {
			continue
		}
		if err := expandAction(action, s, path+": action "+text(action, "name")); err != nil {
			return err
		}
	}
	for name := range definitions {
		if !used[name] {
			return fmt.Errorf("%s: actions/%s.yaml is not listed in actions", path, name)
		}
	}
	if len(actions) > 0 {
		body.set("actions", actions)
	}
	return nil
}

func (o *object) has(key string) bool {
	_, ok := o.get(key)
	return ok
}

func expandAction(action *object, base exprScope, at string) error {
	s := base
	s.inputs, s.steps = map[string]exprField{}, map[string]string{}
	list, _ := action.get("inputs")
	inputs, _ := list.([]any)
	for _, item := range inputs {
		if input := asObject(item); input != nil {
			s.inputs[text(input, "name")] = exprField{kind: fieldKind(text(input, "type")), ref: text(input, "ref")}
		}
	}
	value, ok := action.get("transaction")
	txn := asObject(value)
	if !ok || txn == nil {
		return nil
	}
	if !txn.has("subject") {
		txn.set("subject", s.self)
	}
	if guard, ok := txn.get("guard"); ok {
		compiled, err := compileAt(guard, s, "boolean", at+" guard")
		if err != nil {
			return err
		}
		txn.set("guard", compiled)
	}
	list, _ = txn.get("steps")
	steps, _ := list.([]any)
	for _, item := range steps {
		step := asObject(item)
		if step == nil {
			continue
		}
		if use, ok := step.values["request"].(string); ok {
			if err := expandRequest(step, use, s, at); err != nil {
				return err
			}
			continue
		}
		target, err := stepTarget(step, s)
		if err != nil {
			return fmt.Errorf("%s step %s: %w", at, text(step, "name"), err)
		}
		if when, ok := step.get("when"); ok {
			compiled, err := compileAt(when, s, "boolean", at+" step "+text(step, "name")+" when")
			if err != nil {
				return err
			}
			step.set("when", compiled)
		}
		if set := asObject(step.values["set"]); set != nil {
			changes := []any{}
			for _, field := range set.order() {
				change := &object{}
				change.set("field", field)
				if src, _ := set.values[field].(string); src == "$subject" {
					change.set("from", "$subject")
				} else if strings.ReplaceAll(src, " ", "") == "me()" {
					change.set("from", "$me")
				} else {
					want := s.objects[target][field].kind
					raw := set.values[field]
					if _, isText := raw.(string); !isText {
						raw = literalSource(raw)
					}
					compiled, err := compileAt(raw, s, want, at+" step "+text(step, "name")+" "+field)
					if err != nil {
						return err
					}
					change.set("expr", compiled)
				}
				changes = append(changes, change)
			}
			step.remove("set")
			step.set("changes", changes)
		}
		s.steps[text(step, "name")] = target
	}
	return nil
}

func expandRequest(step *object, use string, s exprScope, at string) error {
	if !step.has("name") {
		step.set("name", "request")
	}
	payload := []any{}
	if with := asObject(step.values["with"]); with != nil {
		for _, field := range with.order() {
			raw := with.values[field]
			if _, isText := raw.(string); !isText {
				raw = literalSource(raw)
			}
			compiled, err := compileAt(raw, s, "text", at+" step "+text(step, "name")+" "+field)
			if err != nil {
				return err
			}
			set := &object{}
			set.set("field", field)
			set.set("expr", compiled)
			payload = append(payload, set)
		}
		step.remove("with")
	}
	request := &object{}
	request.set("use", use)
	request.set("payload", payload)
	step.set("kind", "request")
	step.set("request", request)
	return nil
}

func stepTarget(step *object, s exprScope) (string, error) {
	value, _ := step.get("target")
	if raw, ok := value.(string); ok {
		target := &object{}
		switch {
		case raw == "$subject":
			target.set("type", s.self)
			target.set("id", "$subject")
		case strings.HasPrefix(raw, "$subject."):
			via := strings.TrimPrefix(raw, "$subject.")
			f, ok := s.objects[s.self][via]
			if !ok || f.kind != "reference" {
				return "", fmt.Errorf("%s is not a reference of %s", via, s.self)
			}
			target.set("type", f.ref)
			target.set("id", "$subject")
			target.set("via", via)
		default:
			target.set("type", raw)
		}
		step.set("target", target)
		if !step.has("kind") {
			if target.has("id") {
				step.set("kind", "update")
			} else {
				step.set("kind", "create")
			}
		}
		if !step.has("name") && raw == "$subject" {
			step.set("name", "subject")
		}
		value = target
	}
	if target := asObject(value); target != nil {
		return text(target, "type"), nil
	}
	return "", nil
}

func expandFlow(body *object, types map[string]map[string]exprField, at string) error {
	definition := asObject(body.values["definition"])
	if definition == nil {
		return nil
	}
	if trigger, ok := definition.get("trigger"); ok {
		compiled, err := compileAt(trigger, exprScope{objects: types, json: true}, "boolean", at+" trigger")
		if err != nil {
			return err
		}
		definition.set("trigger", compiled)
	}
	return nil
}

func standardPage(source *object, title string) *object {
	full := refName("object", source)
	fields := []any{}
	list, _ := source.get("fields")
	items, _ := list.([]any)
	for _, item := range items {
		f := &object{}
		f.set("name", text(asObject(item), "name"))
		fields = append(fields, f)
	}
	value := func(raw string) any {
		parsed, err := parseJSON([]byte(raw))
		if err != nil {
			panic(err)
		}
		return parsed
	}
	ref := value(`{"app":"build","kind":"object","name":` + quoteJSON(full) + `}`)
	binding := func(id, widget, mode string) any {
		ports := &object{}
		ports.set("selectionMode", mode)
		ports.set("widget", widget)
		if widget == "table" {
			ports.set("parentable", "true")
		}
		model := &object{}
		model.set("fields", fields)
		model.set("type", full)
		b := &object{}
		b.set("fields", fields)
		b.set("id", id)
		b.set("kind", "record")
		b.set("model", model)
		b.set("object", ref)
		b.set("owner", "binding:"+id)
		b.set("ports", ports)
		return b
	}
	page := &object{}
	page.set("actionTriggers", value(`[{"actions":[{"app":"build","kind":"action","name":`+quoteJSON(full+".create")+`},{"app":"build","kind":"action","name":`+quoteJSON(full+".edit")+`}],"binding":"page","id":"page"},{"binding":"list","id":"list","owner":"binding:list"},{"binding":"detail","id":"detail","owner":"binding:detail"}]`))
	page.set("dataBindings", []any{binding("list", "table", "write"), binding("detail", "detail", "read")})
	page.set("format", float64(4))
	page.set("inputSchema", &object{})
	page.set("name", full)
	page.set("object", ref)
	page.set("scopeVisibility", value(`{"owners":{"detail":"binding:detail","list":"binding:list"},"root":"page","scopes":[{"id":"page","kind":"page"},{"binding":"list","id":"binding:list","kind":"page","parent":"page"},{"binding":"detail","id":"binding:detail","kind":"page","parent":"page"}]}`))
	page.set("title", title)
	page.set("widgetSpec", value(`{"body":{"document":null,"layout":"list-detail","sections":[{"binding":"list","id":"list","triggers":["list"]},{"binding":"detail","id":"detail","triggers":["detail"]}]},"format":"platform.page.web.v1"}`))
	return page
}

func quoteJSON(s string) string {
	var b strings.Builder
	quote(&b, s)
	return b.String()
}

func readLocales(dir string) (map[string]*object, error) {
	entries, err := os.ReadDir(filepath.Join(dir, localesDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]*object{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			return nil, fmt.Errorf("%s/%s: a locale is <language>.yaml", localesDir, entry.Name())
		}
		value, err := readYAML(filepath.Join(dir, localesDir, entry.Name()))
		if err != nil {
			return nil, err
		}
		words := asObject(value)
		if words == nil {
			return nil, fmt.Errorf("%s/%s: a mapping from source text to translation", localesDir, entry.Name())
		}
		out[strings.TrimSuffix(entry.Name(), ".yaml")] = words
	}
	return out, nil
}

func expand(dir string, resources []resource, released bool, external map[string]map[string]exprField) ([]resource, error) {
	types := map[string]map[string]exprField{}
	for name, fields := range external {
		types[name] = fields
	}
	types["enterprise.element"] = map[string]exprField{"id": {kind: "text"}, "name": {kind: "text"}, "stereotype": {kind: "text"}, "kind": {kind: "text"}}
	definitions := map[string]map[string]*object{}
	var out []resource
	for _, r := range resources {
		if r.ref.Kind == "action" && strings.HasPrefix(r.ref.Name, "@") {
			parts := strings.Split(strings.TrimPrefix(r.ref.Name, "@"), "/")
			objectDir, verb := parts[1], strings.TrimSuffix(parts[3], ".yaml")
			if definitions[objectDir] == nil {
				definitions[objectDir] = map[string]*object{}
			}
			definitions[objectDir][verb] = asObject(r.body)
			continue
		}
		if r.ref.Kind == "object" {
			types[r.ref.Name] = objectFields(asObject(r.body))
		}
		out = append(out, r)
	}
	pages := map[string]bool{}
	objects := map[string]*object{}
	var apps []*object
	for _, r := range out {
		body := asObject(r.body)
		if icon := text(body, "icon"); !released && icon != "" && (r.ref.Kind == "object" || r.ref.Kind == "app") && !slices.Contains(packagedesc.Icons, icon) {
			return nil, fmt.Errorf("%s: icon %q is not one of the platform icons (Heroicons outline names, e.g. user-plus, briefcase, building-office-2)", r.path, icon)
		}
		switch r.ref.Kind {
		case "object":
			objectDir := strings.Split(filepath.ToSlash(r.path), "/")[1]
			if err := expandObject(body, r.path, definitions[objectDir], types); err != nil {
				return nil, err
			}
			delete(definitions, objectDir)
			objects[r.ref.Name] = body
		case "flow":
			if err := expandFlow(body, types, r.path); err != nil {
				return nil, err
			}
		case "page":
			pages[r.ref.Name] = true
		case "app":
			apps = append(apps, body)
		}
	}
	for objectDir := range definitions {
		return nil, fmt.Errorf("objects/%s/actions: no object.yaml beside them", objectDir)
	}
	for _, app := range apps {
		list, _ := app.get("pages")
		names, _ := list.([]any)
		for _, item := range names {
			name, _ := item.(string)
			body, isObject := objects[name]
			if pages[name] || !isObject {
				continue
			}
			pages[name] = true
			out = append(out, resource{ref: assetRef("page", name), path: "objects/" + text(body, "name") + "/page.yaml", body: standardPage(body, text(body, "plural"))})
		}
	}
	locales, err := readLocales(dir)
	if err != nil {
		return nil, err
	}
	if len(locales) > 0 {
		if len(apps) != 1 {
			return nil, fmt.Errorf("%s: translations need exactly one application in the package", localesDir)
		}
		languages := asObject(apps[0].values["languages"])
		if languages == nil {
			languages = &object{}
		}
		names := make([]string, 0, len(locales))
		for name := range locales {
			names = append(names, name)
		}
		slices.Sort(names)
		wanted := map[string]bool{}
		for _, r := range out {
			wording(r.body, "", "", wanted)
		}
		for _, name := range names {
			if languages.has(name) {
				return nil, fmt.Errorf("%s/%s.yaml and the application both translate %s", localesDir, name, name)
			}
			var missing []string
			for text := range wanted {
				if !locales[name].has(text) {
					missing = append(missing, text)
				}
			}
			if len(missing) > 0 {
				slices.Sort(missing)
				return nil, fmt.Errorf("%s/%s.yaml has no translation for %d texts: %q", localesDir, name, len(missing), missing)
			}
			languages.set(name, locales[name])
		}
		apps[0].set("languages", languages)
	}
	sortResources(out)
	return out, nil
}

func literalSource(value any) string {
	switch v := value.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return jsNumber(v)
	}
	return fmt.Sprint(value)
}

func assetRef(kind, name string) platform.AssetRef {
	return platform.AssetRef{App: "build", Kind: platform.AssetKind(kind), Name: name}
}

func sortResources(list []resource) {
	slices.SortFunc(list, func(a, b resource) int { return publication.CompareAssetRef(a.ref, b.ref) })
}

var worded = map[string]bool{"title": true, "plural": true, "description": true, "help": true, "oneOf": true, "message": true, "body": true, "exit": true}

func wording(value any, key, within string, into map[string]bool) {
	switch v := value.(type) {
	case *object:
		for _, k := range v.order() {
			wording(v.values[k], k, within, into)
		}
	case []any:
		for _, item := range v {
			wording(item, key, key, into)
		}
	case string:
		switch {
		case (worded[key] || key == "group" && within == "fields") && strings.TrimSpace(v) != "":
			into[v] = true
		case key == "choices":
			for _, choice := range strings.Split(v, ",") {
				if _, label, ok := strings.Cut(choice, ":"); ok && label != "" {
					into[label] = true
				}
			}
		}
	}
}
