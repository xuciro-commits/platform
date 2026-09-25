package platformserver

import (
	"embed"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"platformserver/platform"
)

// Languages (ADR-0023). The host translates the declarations it serves —
// apps, entity types, fields, states, transitions, actions, settings, flows
// and agents — into the language of the request, from the app's dictionary
// and then the platform's. Records, history and the journal are never
// translated: they are what people and apps wrote.

//go:embed i18n
var platformFiles embed.FS

// PlatformLanguages translate the platform apps' titles and descriptions.
var PlatformLanguages = platform.LoadLanguages(platformFiles, "i18n")

// translated are the keys whose string values are declaration texts.
var translated = map[string]bool{"title": true, "plural": true, "description": true}

// declarationReads are named reads that serve declarations, not records.
var declarationReads = map[string]bool{"settings": true, "flows": true, "agents": true}

// Language is the first language of the request's Accept-Language the tenant
// has a dictionary for; "" is English, the source.
func (t *Tenant) Language(r *http.Request) string {
	known := t.languages()
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = canonical(tag)
		if tag == "" || strings.HasPrefix(tag, "en") {
			return ""
		}
		for _, lang := range known {
			if strings.EqualFold(lang, tag) {
				return lang
			}
		}
	}
	return ""
}

// canonical names Chinese by its script as the dictionaries do: zh, zh-Hans
// and zh-SG are zh-CN; zh-Hant, zh-HK and zh-MO are zh-TW.
func canonical(tag string) string {
	lower := strings.ToLower(tag)
	if lower != "zh" && !strings.HasPrefix(lower, "zh-") {
		return tag
	}
	for _, traditional := range []string{"hant", "tw", "hk", "mo"} {
		if strings.Contains(lower, traditional) {
			return "zh-TW"
		}
	}
	return "zh-CN"
}

// languages are the languages any dictionary of the tenant has, sorted.
func (t *Tenant) languages() []string {
	out := []string{}
	add := func(l platform.Languages) {
		for lang := range l {
			if !slices.Contains(out, lang) {
				out = append(out, lang)
			}
		}
	}
	add(PlatformLanguages)
	for _, a := range t.apps {
		add(a.Manifest().Languages)
	}
	slices.Sort(out)
	return out
}

// Dictionary is the tenant's dictionary for a language: the platform's, then
// each app's in the order the tenant runs them.
func (t *Tenant) Dictionary(lang string) map[string]string {
	if lang == "" {
		return nil
	}
	if d, ok := t.dictionaries.Load(lang); ok {
		return d.(map[string]string)
	}
	d := map[string]string{}
	for k, v := range PlatformLanguages[lang] {
		d[k] = v
	}
	for _, a := range t.apps {
		for k, v := range a.Manifest().Languages[lang] {
			d[k] = v
		}
	}
	t.dictionaries.Store(lang, d)
	return d
}

// Translate returns v, as JSON, with every declaration text translated.
func Translate(v any, dict map[string]string) any {
	if len(dict) == 0 {
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
	tr := func(s string) string {
		if out, ok := dict[s]; ok && out != "" {
			return out
		}
		return s
	}
	walkTexts(tree, tr)
	walkChoices(tree, func(m map[string]any, choices []any) {
		titles := make([]any, len(choices))
		for i, c := range choices {
			s, _ := c.(string)
			titles[i] = tr(s)
		}
		m["choiceTitles"] = titles // the values stay what records hold
	})
	return tree
}

// walkChoices calls f for each object of a JSON tree that lists choices.
func walkChoices(v any, f func(map[string]any, []any)) {
	switch x := v.(type) {
	case map[string]any:
		if choices, ok := x["choices"].([]any); ok {
			f(x, choices)
		}
		for _, child := range x {
			walkChoices(child, f)
		}
	case []any:
		for _, child := range x {
			walkChoices(child, f)
		}
	}
}

// walkTexts replaces each declaration text in a JSON tree.
func walkTexts(v any, f func(string) string) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if s, ok := child.(string); ok && translated[k] {
				x[k] = f(s)
				continue
			}
			walkTexts(child, f)
		}
	case []any:
		for _, child := range x {
			walkTexts(child, f)
		}
	}
}

// Texts are every declaration text of an app, as the host serves them: what a
// dictionary must translate for the app to read in another language.
func (t *Tenant) Texts(app string) []string {
	a := t.app(app)
	if a == nil {
		return nil
	}
	m := a.Manifest()
	var parts []any
	parts = append(parts, map[string]any{"title": m.Title}, m.Actions.All(), m.Settings, m.Emits)
	for _, et := range t.records.types {
		if et.info.App == app {
			parts = append(parts, et.info)
		}
	}
	for _, f := range m.Flows {
		parts = append(parts, map[string]any{"title": f.Title})
	}
	for _, ag := range m.Agents {
		parts = append(parts, map[string]any{"title": ag.Title, "description": ag.Description})
	}
	seen := map[string]bool{}
	out := []string{}
	for _, p := range parts {
		raw, _ := json.Marshal(p)
		var tree any
		json.Unmarshal(raw, &tree)
		add := func(s string) string {
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
			return s
		}
		walkTexts(tree, add)
		walkChoices(tree, func(_ map[string]any, choices []any) {
			for _, c := range choices {
				if s, ok := c.(string); ok {
					add(s)
				}
			}
		})
	}
	slices.Sort(out)
	return out
}

// Untranslated are the app's declaration texts the tenant's dictionary for a
// language lacks. Apps' tests require none for the languages they ship.
func (t *Tenant) Untranslated(app, lang string) []string {
	dict := t.Dictionary(lang)
	out := []string{}
	for _, s := range t.Texts(app) {
		if _, ok := dict[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}
