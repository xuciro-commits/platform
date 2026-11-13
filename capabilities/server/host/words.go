package host

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
