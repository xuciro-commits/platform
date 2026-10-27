package platform

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode"
)

// Languages are an app's translations: for each language
// ("zh-CN"), the English text of its titles and descriptions mapped to their
// translation. English is the source and the fallback, so an app is
// translated one text at a time without renaming anything.
type Languages map[string]map[string]string

// LoadLanguages reads the dictionaries <dir>/<language>.json of an app, usually
// embedded with it (//go:embed i18n). A file that does not parse panics: it is
// code, and every composition's tests load it.
func LoadLanguages(fsys fs.FS, dir string) Languages {
	out := Languages{}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic("languages: " + err.Error())
	}
	for _, e := range entries {
		lang, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			panic("languages: " + err.Error())
		}
		dict := map[string]string{}
		if err := json.Unmarshal(raw, &dict); err != nil {
			panic("languages: " + e.Name() + ": " + err.Error())
		}
		out[lang] = dict
	}
	return out
}

// CheckLanguages bounds application-owned declaration dictionaries without
// changing their source texts or translations.
func (a Application) CheckLanguages() error {
	if len(a.Languages) > 32 {
		return fmt.Errorf("application translations exceed 32 languages")
	}
	entries := 0
	for lang, words := range a.Languages {
		if lang == "" || len(lang) > 35 || strings.ContainsAny(lang, "/\\") || strings.ContainsFunc(lang, unicode.IsSpace) {
			return fmt.Errorf("application translations need a language tag")
		}
		entries += len(words)
		for source, translation := range words {
			if strings.TrimSpace(source) == "" || strings.TrimSpace(translation) == "" || len(source) > 4096 || len(translation) > 4096 {
				return fmt.Errorf("application translations need nonempty texts of at most 4096 bytes")
			}
		}
	}
	if entries > 10000 {
		return fmt.Errorf("application translations exceed 10000 entries")
	}
	return nil
}
