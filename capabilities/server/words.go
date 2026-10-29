package platformserver

import (
	"encoding/json"
	"platformserver/kernel/transport"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"slices"
	"strings"

	"time"
)

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
		t.words.Set(lang, p.Text, strings.TrimSpace(p.Translation))
		t.i18n.Reset()
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

// Words lists an app's declaration texts ( Texts, plus the tenant's
// installed definitions for Build) with what every language says for each.
func (t *Tenant) Words(app string) WordsView {
	view := WordsView{App: app, Apps: []string{}, Languages: t.i18n.Languages(), Words: []Word{}}
	for _, a := range t.apps {
		view.Apps = append(view.Apps, a.Manifest().ID)
	}
	texts := t.Texts(app)
	if app == controlowner.BuildApp {
		seen := map[string]bool{}
		for _, s := range texts {
			seen[s] = true
		}
		var parts []any
		for _, d := range t.definitions {
			if d.Ref.App == controlowner.BuildApp {
				parts = append(parts, d)
			}
		}
		raw, _ := json.Marshal(parts)
		var tree any
		json.Unmarshal(raw, &tree)
		transport.WalkTexts(tree, func(s string) string {
			if s != "" && !seen[s] {
				seen[s] = true
				texts = append(texts, s)
			}
			return s
		})
		slices.Sort(texts)
	}
	own := t.words.Clone()
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
