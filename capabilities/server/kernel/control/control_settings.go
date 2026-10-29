package control

import (
	"maps"
	"sync"
	"sync/atomic"
)

// Settings are the apps' settings as set in this tenant, "<app>/<name>" →
// value (declared defaults apply where none is set). Reads are lock-free: a
// request asks for a dozen settings, and settings change once in a while, so a
// write replaces the whole map instead of blocking the readers.
type Settings struct {
	mu     sync.Mutex // serializes writers
	values atomic.Pointer[map[string]string]
}

func (s *Settings) Get(key string) (string, bool) {
	if values := s.values.Load(); values != nil {
		v, ok := (*values)[key]
		return v, ok
	}
	return "", false
}

func (s *Settings) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]string{}
	if current := s.values.Load(); current != nil {
		next = maps.Clone(*current)
	}
	next[key] = value
	s.values.Store(&next)
}

func (s *Settings) Snapshot() map[string]string {
	if current := s.values.Load(); current != nil {
		return maps.Clone(*current)
	}
	return map[string]string{}
}

func (s *Settings) Restore(values map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if values == nil {
		values = map[string]string{}
	}
	s.values.Store(&values)
}

// tenantWords are the entries, language → text → translation, under their own
// lock as settings are: read by the translator, set by one console action.
type Words struct {
	mu    sync.Mutex
	words map[string]map[string]string
}

func (w *Words) Set(lang, text, translation string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.words == nil {
		w.words = map[string]map[string]string{}
	}
	if translation == "" {
		delete(w.words[lang], text)
		if len(w.words[lang]) == 0 {
			delete(w.words, lang)
		}
		return
	}
	if w.words[lang] == nil {
		w.words[lang] = map[string]string{}
	}
	w.words[lang][text] = translation
}

// clone is a copy of every language's entries: what the translator layers.
func (w *Words) Clone() map[string]map[string]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]map[string]string{}
	for lang, d := range w.words {
		out[lang] = maps.Clone(d)
	}
	return out
}

func (w *Words) Restore(words map[string]map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.words = words
	if w.words == nil {
		w.words = map[string]map[string]string{}
	}
}
