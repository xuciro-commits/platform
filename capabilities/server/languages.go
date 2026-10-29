package platformserver

import (
	"embed"
	"encoding/json"
	"net/http"
	controlowner "platformserver/kernel/control"
	"platformserver/kernel/transport"
	"slices"
	"strings"

	"platformserver/platform"
)

//go:embed i18n
var platformFiles embed.FS

var PlatformLanguages = platform.LoadLanguages(platformFiles, "i18n")

var (
	declarationReads = map[string]bool{"settings": true, "flows": true, "agents": true}
	messageReads     = map[string]bool{"notifications": true, "inbox": true, "requests": true, "timeline": true}
)

func (t *Tenant) Language(m platform.Member, r *http.Request) string {
	if m.Language != "" {
		return m.Language
	}
	if lang, asked := t.asked(r); asked {
		return lang
	}
	if d, ok := t.app(controlowner.PlatformApp).(platform.IdentityProfile); ok {
		return d.Language("")
	}
	return ""
}

func (t *Tenant) asked(r *http.Request) (string, bool) {
	header := r.Header.Get("Accept-Language")
	if header == "" {
		return "", false
	}
	var known []string
	for _, part := range strings.Split(header, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = canonical(tag)
		if tag == "" {
			continue
		}
		if known == nil {
			known = t.i18n.Languages()
		}
		if strings.HasPrefix(tag, "en") {
			if slices.Contains(known, "en") {
				return "en", true
			}
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

func (t *Tenant) Texts(app string) []string {
	a := t.app(app)
	if a == nil {
		return nil
	}
	m := a.Manifest()
	var parts []any
	parts = append(parts, map[string]any{"title": m.Title}, m.Actions.All(), m.Pages, m.Settings, m.Emits)
	for _, et := range t.records.Types() {
		if et.Info().App == app {
			parts = append(parts, et.Info())
		}
	}
	for _, a := range m.Actions.All() {
		if a.Approval != nil {
			for _, l := range a.Approval.Levels {
				parts = append(parts, map[string]any{"title": l.Title})
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
		transport.WalkTexts(tree, add)
		transport.WalkChoices(tree, func(_ map[string]any, choices []any) {
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

func (t *Tenant) Untranslated(app, lang string) []string {
	out := []string{}
	for _, s := range t.Texts(app) {
		if !t.i18n.CanSay(lang, s) {
			out = append(out, s)
		}
	}
	return out
}

func (t *Tenant) meaning(app string) string {
	var b strings.Builder
	for _, et := range t.records.SortedTypes() {
		info := et.Info()
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

func (t *Tenant) names(info platform.EntityInfo, q string) (string, bool) {
	lower := " " + strings.ToLower(strings.Join(strings.Fields(q), " ")) + " "
	candidates := []string{info.Type, info.Title, info.Plural}
	candidates = append(candidates, strings.Split(info.Synonyms, ",")...)
	for _, lang := range t.i18n.Languages() {
		dict := t.i18n.Dictionary(lang)
		for _, s := range []string{info.Title, info.Plural, info.Synonyms} {
			if tr := dict[s]; tr != "" {
				candidates = append(candidates, strings.Split(tr, ",")...)
			}
		}
	}
	candidates = append(candidates, t.termsFor(info.Type)...)
	slices.SortFunc(candidates, func(a, b string) int { return len(b) - len(a) })
	for _, c := range candidates {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		wide := c[0] >= 0x80
		for _, form := range []string{c + "s", c} {
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

func (t *Tenant) termsFor(declaration string) []string {
	if t.knowledge == nil {
		return nil
	}
	return t.knowledge.TermsFor(declaration)
}

func (t *Tenant) declares(name string) bool {
	for _, et := range t.records.SortedTypes() {
		if et.Info().Type == name {
			return true
		}
		if field, ok := strings.CutPrefix(name, et.Info().Type+"."); ok {
			if _, ok := et.Info().Field(field); ok {
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

func (t *Tenant) Say(lang, s string) string { return t.i18n.Say(lang, s) }
