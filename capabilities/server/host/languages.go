package host

import (
	"embed"
	"net/http"
	"platformserver/kernel/transport"
	ontology "platformserver/services/ontology"

	"platformserver/platform"
)

//go:embed i18n
var platformFiles embed.FS

var PlatformLanguages = platform.LoadLanguages(platformFiles, "i18n")

func (t *Tenant) Language(m platform.Member, r *http.Request) string {
	if m.Language != "" {
		return m.Language
	}
	if lang, asked := transport.NegotiateLanguage(r.Header.Get("Accept-Language"), t.i18n.Languages); asked {
		return lang
	}
	if t.identityOwner != nil {
		return t.identityOwner.Language("")
	}
	return ""
}

func (t *Tenant) Words(app string) transport.WordsView {
	apps := []string{}
	for _, a := range t.apps {
		apps = append(apps, a.Manifest().ID)
	}
	var manifest *platform.Manifest
	if a := t.App(app); a != nil {
		m := a.Manifest()
		manifest = &m
	}
	return transport.WordsOf(app, apps, t.i18n.Languages(), transport.TextsOf(app, manifest, t.definitions, t.typeInfos()), t.i18n.Say, t.words.Clone())
}

func (t *Tenant) typeInfos() []platform.EntityInfo {
	var infos []platform.EntityInfo
	for _, et := range t.Projection.SortedTypes() {
		infos = append(infos, et.Info())
	}
	return infos
}

func (t *Tenant) names(info platform.EntityInfo, q string) (string, bool) {
	var terms []string
	if t.knowledge != nil {
		terms = t.knowledge.TermsFor(info.Type)
	}
	return ontology.Mention(q, info, t.i18n.Languages(), t.i18n.Dictionary, terms)
}

func (t *Tenant) meaning(app string) string { return ontology.Meaning(app, t.typeInfos()) }

func (t *Tenant) declares(name string) bool {
	return ontology.Declares(name, t.typeInfos(), func(action string) bool {
		for _, a := range t.apps {
			if _, ok := a.Manifest().Actions.Action(action); ok {
				return true
			}
		}
		return false
	})
}

func (t *Tenant) glossary(app string) string {
	if t.knowledge == nil {
		return ""
	}
	return ontology.Glossary(t.knowledge.Terms(app))
}

func (t *Tenant) Say(lang, s string) string { return t.i18n.Say(lang, s) }
