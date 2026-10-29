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
