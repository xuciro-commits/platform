package transport

import (
	"encoding/json"
	"platformserver/platform"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
)

type Translator struct {
	base         platform.Languages
	apps         func() []platform.Service
	manifest     func(platform.Service) platform.Manifest
	tenant       func() map[string]map[string]string
	mu           sync.Mutex
	applications map[string]platform.Languages
	conflicts    map[string]platform.Languages
	dictionaries map[dictionaryKey]*translationDictionary
}

type dictionaryKey struct{ app, lang string }

type translationDictionary struct {
	words    map[string]string
	once     sync.Once
	patterns []pattern
	sayMu    sync.RWMutex
	said     map[string]string
}

func (x *Translator) appManifest(a platform.Service) platform.Manifest {
	if x.manifest != nil {
		return x.manifest(a)
	}
	return a.Manifest()
}

func (x *Translator) InstallApplications(definitions []platform.Definition) {
	applications := map[string]platform.Languages{}
	conflicts := map[string]platform.Languages{}
	merge := func(scope string, languages platform.Languages) {
		if applications[scope] == nil {
			applications[scope], conflicts[scope] = platform.Languages{}, platform.Languages{}
		}
		for lang, words := range languages {
			if applications[scope][lang] == nil {
				applications[scope][lang], conflicts[scope][lang] = map[string]string{}, map[string]string{}
			}
			merged, ambiguous := applications[scope][lang], conflicts[scope][lang]
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
	for _, d := range definitions {
		if d.Application == nil {
			continue
		}
		scopes := []string{d.Ref.App}
		addScope := func(scope string) {
			if scope != "" && !slices.Contains(scopes, scope) {
				scopes = append(scopes, scope)
			}
		}
		if d.Application.Name != "" {
			addScope(scopeOf(d.Ref.App, d.Application.Name+"._"))
		}
		for _, page := range d.Application.Pages {
			addScope(scopeOf(d.Ref.App, page))
		}
		for _, resource := range d.Application.Resources {
			addScope(scopeOf(d.Ref.App, resource.Name))
		}
		for _, scope := range scopes {
			merge(scope, d.Application.Languages)
		}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.applications, x.conflicts, x.dictionaries = applications, conflicts, nil
}

func scopeOf(app, name string) string {
	app, _, _ = strings.Cut(app, "/")
	if namespace, _, ok := strings.Cut(name, "."); ok && namespace != "" && namespace != app {
		return app + "/" + namespace
	}
	return app
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
			base, _, scoped := strings.Cut(app, "/")
			for _, a := range x.apps() {
				manifest := x.appManifest(a)
				if manifest.ID == base {
					add(manifest.Languages[lang])
					break
				}
			}
			add(x.applications[base][lang])
			for scope, languages := range x.applications {
				if scope == app && scoped || !scoped && strings.HasSuffix(scope, "/"+app) {
					add(languages[lang])
				}
			}
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

func (x *Translator) Origin(scope, lang, s, said string) (string, bool) {
	x.mu.Lock()
	conflict, packaged := x.conflicts[scope][lang][s] != "" && x.applications[scope][lang][s] == "", false
	for _, languages := range x.applications {
		packaged = packaged || languages[lang][s] == said
	}
	x.mu.Unlock()
	if x.tenant != nil && x.tenant()[lang][s] != "" {
		return "workspace", conflict
	}
	if said == s {
		return "", conflict
	}
	if packaged {
		return "app", conflict
	}
	return "platform", conflict
}

func (x *Translator) SayInApp(app, lang, s string) string {
	return x.say(app, lang, s, 0)
}

var (
	declarationReads = map[string]bool{"settings": true, "flows": true, "agents": true}
	messageReads     = map[string]bool{"notifications": true, "inbox": true, "requests": true, "timeline": true}
)

func (x *Translator) ForRead(name string, out any, lang string) any {
	if declarationReads[name] {
		out = x.Translate(out, lang)
	}
	if messageReads[name] {
		out = x.TranslateMessages(out, lang)
	}
	return out
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
				if !opaquePresentationValue(node, key) {
					presentation(child, owner)
				}
			}
		}
	}
	presentation(tree, app)
	walkTextsWithApp(tree, app, func(app, s string) string { return x.SayInApp(app, lang, s) })
	walkChoicesWithApp(tree, app, func(app string, m map[string]any, choices []any) {
		declared, _ := m["choiceTitles"].([]any)
		titles := make([]any, len(choices))
		for i, c := range choices {
			s, _ := c.(string)
			if len(declared) == len(choices) {
				s, _ = declared[i].(string)
			}
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

func (p pattern) couldMatch(s string) bool {
	if len(s) < p.fixed || !strings.HasPrefix(s, p.parts[0]) || !strings.HasSuffix(s, p.parts[len(p.parts)-1]) {
		return false
	}
	rest := s[len(p.parts[0]) : len(s)-len(p.parts[len(p.parts)-1])]
	if len(rest) < len(p.names) {
		return false
	}
	for _, lit := range p.parts[1 : len(p.parts)-1] {
		if len(rest) < 2 {
			return false
		}
		i := strings.Index(rest[1:], lit)
		if i < 0 {
			return false
		}
		rest = rest[1+i+len(lit):]
	}
	return len(rest) > 0
}

func (x *Translator) split(app, lang string, p pattern, s string, depth int) ([]string, bool) {
	if !p.couldMatch(s) {
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

func (x *Translator) Said(err *platform.Error, lang string) *platform.Error {
	if err == nil || err.Message == "" {
		return err
	}
	translated := *err
	translated.Message = x.Say(lang, err.Message)
	return &translated
}

func (x *Translator) Say(lang, s string) string { return x.say("", lang, s, 0) }

func (x *Translator) say(app, lang, s string, depth int) string {
	if lang == "" || s == "" || depth > maxSay {
		return s
	}
	if tr, ok := x.lookup(app, lang, s); ok {
		return tr
	}
	var dict *translationDictionary
	if depth == 0 {
		dict = x.dictionary(app, lang)
		dict.sayMu.RLock()
		tr, ok := dict.said[s]
		dict.sayMu.RUnlock()
		if ok {
			return tr
		}
	}
	out := s
	var p pattern
	var filled []string
	for _, q := range x.patterns(app, lang) {
		if !q.couldMatch(s) {
			continue
		}
		if found, ok := x.split(app, lang, q, s, depth); ok {
			p, filled = q, found
			break
		}
		if filled == nil {
			if m := q.re.FindStringSubmatch(s); m != nil {
				p, filled = q, m[1:]
			}
		}
	}
	if filled != nil {
		values := map[string]string{}
		for i, name := range p.names {
			values[name] = x.say(app, lang, filled[i], depth+1)
		}
		out = placeholder.ReplaceAllStringFunc(p.out, func(x string) string {
			if v, ok := values[x[1:len(x)-1]]; ok {
				return v
			}
			return x
		})
	}
	if dict != nil {
		dict.sayMu.Lock()
		if dict.said == nil {
			dict.said = map[string]string{}
		}
		if len(dict.said) < 8192 {
			dict.said[s] = out
		}
		dict.sayMu.Unlock()
	}
	return out
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

func NewTranslator(apps func() []platform.Service, manifest func(platform.Service) platform.Manifest, tenant func() map[string]map[string]string, base platform.Languages) Translator {
	return Translator{apps: apps, manifest: manifest, tenant: tenant, base: base}
}

func WalkChoices(v any, f func(map[string]any, []any)) {
	walkChoicesWithApp(v, "", func(_ string, m map[string]any, c []any) { f(m, c) })
}

func opaquePresentationValue(node map[string]any, key string) bool {
	return key == "languages" || key == "widgetSpec" || key == "presentationWords" ||
		key == "view" && node["standard"] != nil && node["fields"] != nil
}

func walkChoicesWithApp(v any, currentApp string, f func(string, map[string]any, []any)) {
	switch x := v.(type) {
	case map[string]any:
		app := ownerOf(x, currentApp)
		if choices, ok := x["choices"].([]any); ok {
			f(app, x, choices)
		}
		for key, child := range x {
			if opaquePresentationValue(x, key) {
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

func ownerOf(x map[string]any, current string) string {
	app := current
	ref, _ := x["ref"].(map[string]any)
	if owner, ok := ref["app"].(string); ok {
		app = owner
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
	for _, id := range []any{x["type"], x["object"], x["target"], x["schema"], ref["name"]} {
		if name, ok := id.(string); ok && strings.Contains(name, ".") {
			return scopeOf(app, name)
		}
	}
	if name, ok := x["name"].(string); ok && name != "" {
		if _, hasMenu := x["menu"]; hasMenu || ref["kind"] == string(platform.AssetApp) {
			return scopeOf(app, name+"._")
		}
	}
	if application, ok := x["application"].(map[string]any); ok {
		if name, ok := application["name"].(string); ok && name != "" {
			return scopeOf(app, name+"._")
		}
	}
	return app
}

func WalkTexts(v any, f func(string) string) {
	walkTextsWithApp(v, "", func(_ string, s string) string { return f(s) })
}

func translatedKey(x map[string]any, k string) bool {
	if translated[k] {
		return true
	}
	if k == "group" {
		_, hasName := x["name"].(string)
		return hasName
	}
	return false
}

func walkTextsWithApp(v any, currentApp string, f func(string, string) string) {
	switch x := v.(type) {
	case map[string]any:
		app := ownerOf(x, currentApp)
		for k, child := range x {
			if opaquePresentationValue(x, k) {
				continue
			}
			if s, ok := child.(string); ok && translatedKey(x, k) {
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

var translated = map[string]bool{"title": true, "plural": true, "description": true, "help": true, "synonyms": true, "label": true}
