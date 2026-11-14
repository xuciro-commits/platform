package transport

import (
	"encoding/json"
	"slices"
	"strings"

	"platformserver/platform"
)

func NegotiateLanguage(header string, known func() []string) (string, bool) {
	if header == "" {
		return "", false
	}
	var langs []string
	for _, part := range strings.Split(header, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		tag = canonical(tag)
		if tag == "" {
			continue
		}
		if langs == nil {
			langs = known()
		}
		if strings.HasPrefix(tag, "en") {
			if slices.Contains(langs, "en") {
				return "en", true
			}
			return "", true
		}
		for _, lang := range langs {
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

func Texts(parts []any) []string {
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
		WalkTexts(tree, add)
		WalkChoices(tree, func(_ map[string]any, choices []any) {
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

func DeclaredTexts(m platform.Manifest) []any {
	var parts []any
	if m.Actions != nil {
		for _, a := range m.Actions.All() {
			if a.Approval != nil {
				for _, l := range a.Approval.Levels {
					parts = append(parts, map[string]any{"title": l.Title})
				}
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
	return parts
}

type Word struct {
	Text         string            `json:"text"`
	Translations map[string]string `json:"translations"`
	Own          map[string]string `json:"own"`
}

type WordsView struct {
	App       string   `json:"app"`
	Apps      []string `json:"apps"`
	Languages []string `json:"languages"`
	Words     []Word   `json:"words"`
}

func WordsOf(app string, apps, languages, texts []string, say func(lang, s string) string, own map[string]map[string]string) WordsView {
	view := WordsView{App: app, Apps: apps, Languages: languages, Words: []Word{}}
	for _, s := range texts {
		w := Word{Text: s, Translations: map[string]string{}, Own: map[string]string{}}
		for _, lang := range languages {
			if tr := say(lang, s); tr != s {
				w.Translations[lang] = tr
			}
			if tr, ok := own[lang][s]; ok {
				w.Own[lang] = tr
			}
		}
		view.Words = append(view.Words, w)
	}
	return view
}

func TextsOf(app string, manifest *platform.Manifest, definitions []platform.Definition, infos []platform.EntityInfo) []string {
	var m platform.Manifest
	var parts []any
	if manifest != nil {
		m = *manifest
		parts = append(parts, map[string]any{"title": m.Title}, m.Actions.All(), m.Pages, m.Settings, m.Emits)
	}
	for _, definition := range definitions {
		if definition.Ref.App == app {
			parts = append(parts, definition)
		}
	}
	for _, info := range infos {
		if info.App == app {
			parts = append(parts, info)
		}
	}
	parts = append(parts, DeclaredTexts(m)...)
	return Texts(parts)
}
