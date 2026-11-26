package transport

import (
	"cmp"
	"encoding/json"
	"maps"
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

type Said struct {
	Text     string `json:"text"`
	Origin   string `json:"origin,omitempty"`
	Conflict bool   `json:"conflict,omitempty"`
}

type Word struct {
	Text  string          `json:"text"`
	App   string          `json:"app"`
	Area  string          `json:"area"`
	Kind  string          `json:"kind"`
	Says  map[string]Said `json:"says"`
	Stale bool            `json:"stale,omitempty"`
	scope string
}

type WordsView struct {
	Languages []string `json:"languages"`
	Words     []Word   `json:"words"`
}

var wordKinds = []string{"type", "choice", "action", "page", "other"}

func kindOf(k platform.AssetKind) string {
	switch k {
	case platform.AssetObject, platform.AssetObjectTypeGroup, platform.AssetLinkType, platform.AssetPropertyType, platform.AssetValueType, platform.AssetInterface:
		return "type"
	case platform.AssetAction:
		return "action"
	case platform.AssetPage, platform.AssetApp:
		return "page"
	}
	return "other"
}

func WordsOf(x *Translator, manifests []platform.Manifest, definitions []platform.Definition, infos []platform.RecordTypeInfo, lang string, project func(platform.AssetRef) string) WordsView {
	view := WordsView{Languages: x.Languages(), Words: []Word{}}
	titles, areas, seen := map[string]string{}, map[string]int{}, map[[3]string]bool{}
	for i, m := range manifests {
		titles[m.ID] = x.SayInApp(m.ID, lang, cmp.Or(m.Title, m.ID))
		areas[titles[m.ID]] = 1<<20 + i
	}
	collect := func(app, area, kind string, part any) {
		area = cmp.Or(area, titles[app], app)
		if _, ok := areas[area]; !ok {
			areas[area] = len(areas)
		}
		raw, _ := json.Marshal(part)
		var tree any
		json.Unmarshal(raw, &tree)
		put := func(kind, scope, s string) string {
			if key := [3]string{app, area, s}; s != "" && !seen[key] {
				seen[key] = true
				view.Words = append(view.Words, Word{Text: s, App: app, Area: area, Kind: kind, scope: scope})
			}
			return s
		}
		walkChoicesWithApp(tree, app, func(scope string, _ map[string]any, choices []any) {
			for _, c := range choices {
				if s, ok := c.(string); ok {
					put("choice", scope, s)
				}
			}
		})
		walkTextsWithApp(tree, app, func(scope, s string) string { return put(kind, scope, s) })
	}
	projects, owners := map[string]string{}, make([]string, len(definitions))
	for i, d := range definitions {
		if d.Source != "code" {
			owners[i] = project(d.Ref)
		}
		if d.Ref.Kind == platform.AssetObject {
			projects[d.Ref.Name] = owners[i]
		}
	}
	definitionsOf := func(types bool) {
		for i, d := range definitions {
			if kind := kindOf(d.Ref.Kind); (kind == "type") == types {
				area := owners[i]
				if d.Action != nil {
					area = cmp.Or(area, projects[d.Action.Target])
				}
				if area != "" {
					area = x.SayInApp(scopeOf(d.Ref.App, d.Ref.Name), lang, area)
				}
				collect(d.Ref.App, area, kind, d)
			}
		}
	}
	definitionsOf(true)
	for _, info := range infos {
		area := projects[info.Type]
		if area != "" {
			area = x.SayInApp(scopeOf(info.App, info.Type), lang, area)
		}
		collect(info.App, area, "type", info)
	}
	definitionsOf(false)
	for _, m := range manifests {
		collect(m.ID, "", "page", map[string]any{"title": m.Title})
		if m.Actions != nil {
			for _, a := range m.Actions.All() {
				area := projects[a.Target]
				if area != "" {
					area = x.SayInApp(scopeOf(m.ID, a.Target), lang, area)
				}
				collect(m.ID, area, "action", a)
			}
		}
		collect(m.ID, "", "page", m.Pages)
		collect(m.ID, "", "other", []any{m.Settings, m.Emits, DeclaredTexts(m)})
	}
	slices.SortStableFunc(view.Words, func(a, b Word) int {
		return cmp.Or(areas[a.Area]-areas[b.Area], slices.Index(wordKinds, a.Kind)-slices.Index(wordKinds, b.Kind), strings.Compare(a.Text, b.Text))
	})
	for i := range view.Words {
		w := &view.Words[i]
		w.Says = map[string]Said{}
		for _, l := range view.Languages {
			said := x.SayInApp(w.scope, l, w.Text)
			origin, conflict := x.Origin(w.scope, l, w.Text, said)
			w.Says[l] = Said{Text: said, Origin: origin, Conflict: conflict}
		}
	}
	if x.tenant == nil {
		return view
	}
	current := map[string]bool{}
	for _, w := range view.Words {
		current[w.Text] = true
	}
	stale := map[string]Word{}
	for l, words := range x.tenant() {
		for source, translation := range words {
			if current[source] || translation == "" {
				continue
			}
			w, ok := stale[source]
			if !ok {
				w = Word{Text: source, Kind: "other", Says: map[string]Said{}, Stale: true}
			}
			w.Says[l] = Said{Text: translation, Origin: "workspace"}
			stale[source] = w
		}
	}
	for _, source := range slices.Sorted(maps.Keys(stale)) {
		view.Words = append(view.Words, stale[source])
	}
	return view
}
