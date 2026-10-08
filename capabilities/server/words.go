package platformserver

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Tenant words (ADR-0083 D1): the tenant's own dictionary, one more layer on
// the platform's and the apps' (ADR-0023). An administrator says what a
// declaration text — above all one the tenant defined in Build: an object
// type, a field, a state, an action, an application — reads as in each
// language; the host then serves it like any shipped translation. Names are
// written once in one language and said in the others: never "EN/中文" in one
// title.
const (
	BuildApp             = "build" // the app whose definitions the tenant writes
	TranslationType      = "platform.translation"
	SchemaTranslationSet = "platform.translation.set"
)

// tenantWords are the entries, language → text → translation, under their own
// lock as settings are: read by the translator, set by one console action.
type tenantWords struct {
	mu    sync.Mutex
	words map[string]map[string]string
}

func (w *tenantWords) set(lang, text, translation string) {
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
func (w *tenantWords) clone() map[string]map[string]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := map[string]map[string]string{}
	for lang, d := range w.words {
		out[lang] = maps.Clone(d)
	}
	return out
}

func (w *tenantWords) restore(words map[string]map[string]string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.words = words
	if w.words == nil {
		w.words = map[string]map[string]string{}
	}
}

func wordsActions() []platform.Action {
	return []platform.Action{
		{Schema: SchemaTranslationSet, Target: TranslationType, Capability: "settings", Title: "Translate a text",
			Description: "Say a declaration text (an object type, field, state, action or application name the tenant defined, or any shipped one) in a language (target: the language tag, such as zh-CN). An empty translation removes the tenant's entry.",
			Payload: []platform.Field{
				{Name: "text", Type: "string", Required: true, Description: "The text as declared, in its source language"},
				{Name: "translation", Type: "string", Description: "What it reads as in the target language; empty removes"},
			}, Roles: []string{Admin}},
	}
}

// decideTranslation decides platform.translation.set: a language tag and a
// text are required; the entry applies to every declaration served from then on.
func (t *Tenant) decideTranslation(_ platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	lang := strings.TrimSpace(s.GetTarget().GetId())
	var p struct{ Text, Translation string }
	json.Unmarshal(s.GetPayload(), &p)
	if lang == "" || strings.ContainsAny(lang, " /") || strings.TrimSpace(p.Text) == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return func(*pb.ChangeRecord) {
		t.words.set(lang, p.Text, strings.TrimSpace(p.Translation))
		t.i18n.reset()
	}, nil
}

// A Word is a declaration text with what each language says for it, as the
// Languages page edits them: `translations` is what the host serves (tenant
// entry over shipped dictionary), `own` the tenant's entries only.
type Word struct {
	Text         string            `json:"text"`
	Translations map[string]string `json:"translations"`
	Own          map[string]string `json:"own"`
}

// WordsView answers GET /v1/words: the texts of one app with their
// translations in every language the tenant has.
type WordsView struct {
	App       string   `json:"app"`
	Apps      []string `json:"apps"`      // the apps whose texts may be listed
	Languages []string `json:"languages"` // the languages any dictionary has
	Words     []Word   `json:"words"`
}

// Words lists an app's declaration texts (ADR-0023 Texts, plus the tenant's
// installed definitions for Build) with what every language says for each.
func (t *Tenant) Words(app string) WordsView {
	view := WordsView{App: app, Apps: []string{}, Languages: t.i18n.languages(), Words: []Word{}}
	for _, a := range t.apps {
		view.Apps = append(view.Apps, a.Manifest().ID)
	}
	texts := t.Texts(app)
	if app == BuildApp {
		seen := map[string]bool{}
		for _, s := range texts {
			seen[s] = true
		}
		var parts []any
		for _, d := range t.definitions {
			if d.Ref.App == BuildApp {
				parts = append(parts, d)
			}
		}
		raw, _ := json.Marshal(parts)
		var tree any
		json.Unmarshal(raw, &tree)
		walkTexts(tree, func(s string) string {
			if s != "" && !seen[s] {
				seen[s] = true
				texts = append(texts, s)
			}
			return s
		})
		slices.Sort(texts)
	}
	own := t.words.clone()
	for _, s := range texts {
		w := Word{Text: s, Translations: map[string]string{}, Own: map[string]string{}}
		for _, lang := range view.Languages {
			if tr := t.i18n.Say(lang, s); tr != s {
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
