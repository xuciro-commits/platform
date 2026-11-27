package host

import (
	"cmp"
	"embed"
	"net/http"
	"platformserver/kernel/transport"
	ontology "platformserver/services/ontology"
	build "platformserver/services/resources/authoring"

	"platformserver/platform"
)

//go:embed i18n
var platformFiles embed.FS

var PlatformLanguages = platform.LoadLanguages(platformFiles, "i18n")

func (t *Tenant) Language(m platform.Member, r *http.Request) string {
	if m.Language != "" {
		return m.Language
	}
	if r != nil {
		if lang, asked := transport.NegotiateLanguage(r.Header.Get("Accept-Language"), t.i18n.Languages); asked {
			return lang
		}
	}
	if t.IdentityOwner != nil {
		return t.IdentityOwner.Language("")
	}
	return ""
}

func (t *Tenant) Words(lang string) transport.WordsView {
	manifests := []platform.Manifest{}
	for _, a := range t.apps {
		manifests = append(manifests, a.Manifest())
	}
	return transport.WordsOf(&t.i18n, manifests, t.definitions, t.typeInfos(), lang, func(ref platform.AssetRef) string {
		resource, _ := build.StoreResourceRow(t.Projection, t.ID, ref)
		return cmp.Or(build.StoreProjectTitle(t.Projection, resource.Project), resource.Project)
	})
}

func (t *Tenant) typeInfos() []platform.RecordTypeInfo {
	var infos []platform.RecordTypeInfo
	for _, et := range t.Projection.SortedTypes() {
		infos = append(infos, et.Info())
	}
	return infos
}

func (t *Tenant) names(info platform.RecordTypeInfo, q string) (string, bool) {
	var terms []string
	if t.Glossary != nil {
		terms = t.Glossary.TermsFor(info.Type)
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
	if t.Glossary == nil {
		return ""
	}
	return ontology.Glossary(t.Glossary.Terms(app))
}

func (t *Tenant) Say(lang, s string) string { return t.i18n.Say(lang, s) }
