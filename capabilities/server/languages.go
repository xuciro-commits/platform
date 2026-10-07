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
var translated = map[string]bool{"title": true, "plural": true, "description": true, "help": true, "synonyms": true}

// declarationReads are named reads that serve declarations, not records;
// messageReads serve texts apps wrote for people (notifications, tasks,
// requests), which read in the member's language through Say.
var (
	declarationReads = map[string]bool{"settings": true, "flows": true, "agents": true}
	messageReads     = map[string]bool{"notifications": true, "inbox": true, "requests": true, "timeline": true}
)

// Language is the language a member reads in a request: their own choice,
// else the first of the request's Accept-Language the tenant has a dictionary
// for, else the tenant's default; "" is English, the source.
func (t *Tenant) Language(m platform.Member, r *http.Request) string {
	if m.Language != "" {
		return m.Language
	}
	if lang, asked := t.asked(r); asked {
		return lang
	}
	if d, ok := t.app(PlatformApp).(*Console); ok {
		return d.language("")
	}
	return ""
}

// asked is the first language of Accept-Language the tenant speaks, and
// whether the request named one at all (English included).
func (t *Tenant) asked(r *http.Request) (string, bool) {
	known := t.i18n.languages()
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = canonical(tag)
		if tag == "" {
			continue
		}
		if strings.HasPrefix(tag, "en") {
			return "", true
		}
		for _, lang := range known {
			if strings.EqualFold(lang, tag) {
				return lang, true
			}
		}
	}
	return "", false
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
	parts = append(parts, map[string]any{"title": m.Title}, m.Actions.All(), m.Pages, m.Settings, m.Emits)
	for _, et := range t.records.types {
		if et.info.App == app {
			parts = append(parts, et.info)
		}
	}
	for _, a := range m.Actions.All() {
		if a.Approval != nil {
			for _, l := range a.Approval.Levels {
				parts = append(parts, map[string]any{"title": l.Title}) // a level's title reads in the approver's task
			}
		}
	}
	for _, f := range m.Flows {
		parts = append(parts, map[string]any{"title": f.Title})
	}
	for _, ag := range m.Agents {
		parts = append(parts, map[string]any{"title": ag.Title, "description": ag.Description})
	}
	for _, f := range m.Functions {
		parts = append(parts, map[string]any{"title": f.Title, "description": f.Description, "output": f.Output})
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

// Untranslated are the app's declaration texts a language cannot say: no
// entry, and no pattern whose every value it can say. Apps' tests require
// none for the languages they ship.
func (t *Tenant) Untranslated(app, lang string) []string {
	out := []string{}
	for _, s := range t.Texts(app) {
		if !t.i18n.says(lang, s, 0) {
			out = append(out, s)
		}
	}
	return out
}

// meaning describes an app's entity types as their declarations explain them
// (ADR-0023 D1): what each type is, its other names, and what its fields and
// states hold. Only what is declared is said; "" when nothing is.
func (t *Tenant) meaning(app string) string {
	var b strings.Builder
	for _, et := range t.records.sortedTypes() {
		info := et.info
		if info.App != app {
			continue
		}
		var lines []string
		for _, f := range info.Fields {
			if f.Help == "" && f.Synonyms == "" && f.Example == "" {
				continue
			}
			line := "  - " + f.Name + " (" + f.Title + ")"
			if f.Help != "" {
				line += ": " + f.Help
			}
			if f.Synonyms != "" {
				line += "; also called " + f.Synonyms
			}
			if f.Example != "" {
				line += "; e.g. " + f.Example
			}
			lines = append(lines, line)
		}
		if info.Lifecycle != nil {
			for _, st := range info.Lifecycle.States {
				if st.Description != "" {
					lines = append(lines, "  - state "+st.Name+": "+st.Description)
				}
			}
		}
		if info.Description == "" && info.Synonyms == "" && len(lines) == 0 {
			continue
		}
		b.WriteString("- " + info.Type + " (" + info.Title + ")")
		if info.Description != "" {
			b.WriteString(": " + info.Description)
		}
		if info.Synonyms != "" {
			b.WriteString("; also called " + info.Synonyms)
		}
		b.WriteString("\n")
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// names tells whether q names an entity type — by its type, title, plural or
// synonyms, in English or any language the tenant has a dictionary for — and
// what of q is left to search for within it.
func (t *Tenant) names(info platform.EntityInfo, q string) (string, bool) {
	lower := " " + strings.ToLower(strings.Join(strings.Fields(q), " ")) + " "
	candidates := []string{info.Type, info.Title, info.Plural}
	candidates = append(candidates, strings.Split(info.Synonyms, ",")...)
	for _, lang := range t.i18n.languages() {
		dict := t.i18n.Dictionary(lang)
		for _, s := range []string{info.Title, info.Plural, info.Synonyms} {
			if tr := dict[s]; tr != "" {
				candidates = append(candidates, strings.Split(tr, ",")...)
			}
		}
	}
	candidates = append(candidates, t.termsFor(info.Type)...)
	slices.SortFunc(candidates, func(a, b string) int { return len(b) - len(a) }) // the longest name first
	for _, c := range candidates {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		wide := c[0] >= 0x80                        // CJK text has no spaces between words
		for _, form := range []string{c + "s", c} { // an English plural names the type too
			if i := strings.Index(lower, " "+form+" "); i >= 0 && !wide {
				return strings.TrimSpace(lower[:i] + " " + lower[i+len(form)+2:]), true
			}
		}
		if i := strings.Index(lower, c); wide && i >= 0 {
			return strings.TrimSpace(lower[:i] + lower[i+len(c):]), true
		}
	}
	return q, false
}

// termsFor are the tenant's glossary terms and synonyms that refer to a
// declaration (ADR-0023 D1); the glossary layers names on top, never changes it.
func (t *Tenant) termsFor(declaration string) []string {
	if t.knowledge == nil {
		return nil
	}
	return t.knowledge.TermsFor(declaration)
}

// declares tells whether a name is one of the tenant's declarations: an entity
// type, one of its fields as <type>.<field>, or an action.
func (t *Tenant) declares(name string) bool {
	for _, et := range t.records.sortedTypes() {
		if et.info.Type == name {
			return true
		}
		if field, ok := strings.CutPrefix(name, et.info.Type+"."); ok {
			if _, ok := et.info.Field(field); ok {
				return true
			}
		}
	}
	for _, a := range t.apps {
		if _, ok := a.Manifest().Actions.Action(name); ok {
			return true
		}
	}
	return false
}

// glossary is the tenant's terms an app's agents read, for their prompt.
func (t *Tenant) glossary(app string) string {
	if t.knowledge == nil {
		return ""
	}
	var b strings.Builder
	for _, x := range t.knowledge.Terms(app) {
		b.WriteString("- " + x.Term + ": " + x.Meaning)
		if x.Synonyms != "" {
			b.WriteString(" (also: " + x.Synonyms + ")")
		}
		if x.RefersTo != "" {
			b.WriteString(" [" + x.RefersTo + "]")
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

// splits are the ways s fills the pattern's placeholders, at most limit of
// them: "Kind: Where it goes: out" fills "{field}: {help}" twice (F-25).
func (p pattern) splits(s string, limit int) [][]string {
	var out [][]string
	var fill func(rest string, i int, values []string)
	fill = func(rest string, i int, values []string) {
		if len(out) >= limit {
			return
		}
		lit := p.parts[i+1]
		if i == len(p.names)-1 { // the last placeholder takes all but the closing text
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
