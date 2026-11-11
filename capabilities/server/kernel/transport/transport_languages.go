package transport

import (
	"encoding/json"
	"slices"
	"strings"
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
