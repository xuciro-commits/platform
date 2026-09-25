package platform

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
)

// Languages are an app's translations (ADR-0023 D2, D3): for each language
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
