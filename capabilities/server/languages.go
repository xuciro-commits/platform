package platformserver

import (
	"embed"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"unicode"

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
	messageReads     = map[string]bool{"notifications": true, "inbox": true, "requests": true}
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
	known := t.languages()
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

// Translate returns v, as JSON, with every declaration text said in a
// language: exactly, or by a pattern (a generated action's "Create {thing}").
func (t *Tenant) Translate(v any, lang string) any {
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
	tr := func(s string) string { return t.Say(lang, s) }
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
		if !t.says(lang, s, 0) {
			out = append(out, s)
		}
	}
	return out
}

// lookup is a text's entry in a language's dictionary; a text in lower case,
// as generated sentences hold a title ("Create purchase request"), finds its
// title's entry too.
func (t *Tenant) lookup(lang, s string) (string, bool) {
	dict := t.Dictionary(lang)
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

// says tells whether a language can say a text: by its entry, or by a
// pattern whose values it can say in turn (numbers and names need none).
func (t *Tenant) says(lang, s string, depth int) bool {
	if _, ok := t.lookup(lang, s); ok || depth > 3 || !strings.ContainsFunc(s, unicode.IsLetter) {
		return ok || depth <= 3
	}
	for _, p := range t.patterns(lang) {
		m := p.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		all := true
		for _, v := range m[1:] {
			all = all && t.says(lang, v, depth+1)
		}
		if all {
			return true
		}
	}
	return false
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
	for _, lang := range t.languages() {
		dict := t.Dictionary(lang)
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
	return t.knowledge.termsFor(t, declaration)
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
	for _, x := range t.knowledge.terms(t, app) {
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

// A pattern is a dictionary key with {placeholders}, such as "Downtime on
// {resource}": it matches the English an app wrote with fmt ("Downtime on
// R-7") and says it in the dictionary's language with the same values.
type pattern struct {
	re    *regexp.Regexp
	names []string
	out   string
	fixed int // characters outside placeholders: the more, the more specific
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// patterns are the dictionary's keys with placeholders, the most specific first.
func (t *Tenant) patterns(lang string) []pattern {
	if p, ok := t.patternCache.Load(lang); ok {
		return p.([]pattern)
	}
	var out []pattern
	for key, tr := range t.Dictionary(lang) {
		idx := placeholder.FindAllStringSubmatchIndex(key, -1)
		if len(idx) == 0 {
			continue
		}
		expr, names, last, fixed := "^", []string{}, 0, 0
		for _, m := range idx {
			expr += regexp.QuoteMeta(key[last:m[0]]) + "(.+)"
			fixed += m[0] - last
			names = append(names, key[m[2]:m[3]])
			last = m[1]
		}
		expr += regexp.QuoteMeta(key[last:]) + "$"
		fixed += len(key) - last
		out = append(out, pattern{re: regexp.MustCompile("(?s)" + expr), names: names, out: tr, fixed: fixed})
	}
	slices.SortFunc(out, func(a, b pattern) int { return b.fixed - a.fixed })
	t.patternCache.Store(lang, out)
	return out
}

// Say is a text an app wrote for people, in a language: its translation, or
// the translation of the first pattern it matches, with the matched values
// said in turn (a state, an action's title, a nested text). What nothing
// matches stays as written.
func (t *Tenant) Say(lang, s string) string { return t.say(lang, s, 0) }

func (t *Tenant) say(lang, s string, depth int) string {
	if lang == "" || s == "" || depth > 3 {
		return s
	}
	if tr, ok := t.lookup(lang, s); ok {
		return tr
	}
	for _, p := range t.patterns(lang) {
		m := p.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		values := map[string]string{}
		for i, name := range p.names {
			values[name] = t.say(lang, m[i+1], depth+1)
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

// TranslateMessages returns v, as JSON, with the titles and bodies apps wrote
// said in a language.
func (t *Tenant) TranslateMessages(v any, lang string) any {
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
			if answers, ok := x["answers"].([]any); ok { // a question's answers stay the values submitted
				titles := make([]any, len(answers))
				for i, a := range answers {
					s, _ := a.(string)
					titles[i] = t.Say(lang, s)
				}
				x["answerTitles"] = titles
			}
			for k, child := range x {
				if s, ok := child.(string); ok && (k == "title" || k == "body") {
					x[k] = t.Say(lang, s)
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
