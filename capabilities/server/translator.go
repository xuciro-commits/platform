package platformserver

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"

	"platformkernel/kernel"
	"platformserver/platform"
)

// translator derives immutable dictionaries and patterns from installed
// declarations. Application snapshots arrive from the definition owner, so
// translation never reaches back into the tenant's decision lock.
type translator struct {
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

func (x *translator) appManifest(a platform.App) platform.Manifest {
	if x.manifest != nil {
		return x.manifest(a)
	}
	return a.Manifest()
}

// installApplications projects only published application declarations. A
// conflicting text has no winner, irrespective of application order.
func (x *translator) installApplications(definitions []platform.Definition) {
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

// A pattern is a dictionary key with {placeholders}, such as "Downtime on
// {resource}": it matches the English an app wrote with fmt ("Downtime on
// R-7") and says it in the dictionary's language with the same values.
type pattern struct {
	re    *regexp.Regexp
	names []string
	parts []string // the text around the placeholders: len(names)+1
	out   string
	fixed int // characters outside placeholders: the more, the more specific
}

var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// maxSay bounds how deep patterns nest: a list of choices is said one item a
// level ("{a}, {b}"), so eight lets a field name up to six of them.
const maxSay = 8

// languages are the languages any dictionary of the tenant has, sorted.
func (x *translator) languages() []string {
	out := []string{}
	add := func(l platform.Languages) {
		for lang := range l {
			if !slices.Contains(out, lang) {
				out = append(out, lang)
			}
		}
	}
	add(PlatformLanguages)
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

// Dictionary is the shared dictionary. App-scoped reads additionally prefer
// their owner's declarations; tenant wording always has the last word.
func (x *translator) Dictionary(lang string) map[string]string {
	return x.dictionary("", lang).words
}

func (x *translator) dictionary(app, lang string) *translationDictionary {
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
		add(PlatformLanguages[lang])
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

// reset forgets derived dictionaries after the tenant's words change.
func (x *translator) reset() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.dictionaries = nil
}

func (x *translator) SayInApp(app, lang, s string) string {
	return x.say(app, lang, s, 0)
}

// Translate returns v, as JSON, with every declaration text said in a
// language: exactly, or by a pattern (a generated action's "Create {thing}").
func (x *translator) Translate(v any, lang string) any {
	return x.translate(v, "", lang)
}

func (x *translator) translate(v any, app, lang string) any {
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
	walkTextsWithApp(tree, app, func(app, s string) string { return x.SayInApp(app, lang, s) })
	walkChoicesWithApp(tree, app, func(app string, m map[string]any, choices []any) {
		titles := make([]any, len(choices))
		for i, c := range choices {
			s, _ := c.(string)
			titles[i] = x.SayInApp(app, lang, s)
		}
		m["choiceTitles"] = titles // the values stay what records hold
	})
	return tree
}

// lookup is a text's entry in a language's dictionary; a text in lower case,
// as generated sentences hold a title ("Create purchase request"), finds its
// title's entry too.
func (x *translator) lookup(app, lang, s string) (string, bool) {
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

// says tells whether a language can say a text: by its entry, or by a
// pattern whose values it can say in turn (numbers and names need none).
func (x *translator) says(lang, s string, depth int) bool {
	return x.saysInApp("", lang, s, depth)
}

func (x *translator) saysInApp(app, lang, s string, depth int) bool {
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

// split is the first way s fills p whose every value the language can say.
func (x *translator) split(app, lang string, p pattern, s string, depth int) ([]string, bool) {
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

// patterns are the dictionary's keys with placeholders, the most specific first.
func (x *translator) patterns(app, lang string) []pattern {
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

// said is a refusal with its message in a language (F-23); the code stays.
func (x *translator) said(err *kernel.Error, lang string) *kernel.Error {
	if err == nil || err.Message == "" {
		return err
	}
	return &kernel.Error{Code: err.Code, Message: x.Say(lang, err.Message)}
}

// Say is a text an app wrote for people, in a language: its translation, or
// the translation of the first pattern it matches, with the matched values
// said in turn (a state, an action's title, a nested text). What nothing
// matches stays as written.
func (x *translator) Say(lang, s string) string { return x.say("", lang, s, 0) }

func (x *translator) say(app, lang, s string, depth int) string {
	if lang == "" || s == "" || depth > maxSay {
		return s
	}
	if tr, ok := x.lookup(app, lang, s); ok {
		return tr
	}
	// The most specific pattern with a split the language can say wholly; else
	// the first that matches at all, with what it cannot say left as written.
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

// TranslateMessages returns v, as JSON, with the titles and bodies apps wrote
// said in a language.
func (tr *translator) TranslateMessages(v any, lang string) any {
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
