package host

import (
	"embed"
	"net/http"
	"platformserver/kernel/transport"
	ontology "platformserver/services/ontology"
	controlowner "platformserver/services/security/control"
	"strings"

	"platformserver/platform"
)

//go:embed i18n
var platformFiles embed.FS

var PlatformLanguages = platform.LoadLanguages(platformFiles, "i18n")

var (
	declarationReads = map[string]bool{"settings": true, "flows": true, "agents": true}
	messageReads     = map[string]bool{"notifications": true, "inbox": true, "requests": true, "timeline": true}
)

func (t *Tenant) Language(m platform.Member, r *http.Request) string {
	if m.Language != "" {
		return m.Language
	}
	if lang, asked := t.asked(r); asked {
		return lang
	}
	if d, ok := t.App(controlowner.PlatformApp).(platform.IdentityProfile); ok {
		return d.Language("")
	}
	return ""
}

func (t *Tenant) asked(r *http.Request) (string, bool) {
	return transport.NegotiateLanguage(r.Header.Get("Accept-Language"), t.i18n.Languages)
}

func (t *Tenant) Texts(app string) []string {
	var m platform.Manifest
	var parts []any
	if a := t.App(app); a != nil {
		m = a.Manifest()
		parts = append(parts, map[string]any{"title": m.Title}, m.Actions.All(), m.Pages, m.Settings, m.Emits)
	}
	for _, definition := range t.definitions {
		if definition.Ref.App == app {
			parts = append(parts, definition)
		}
	}
	for _, et := range t.Projection.Types() {
		if et.Info().App == app {
			parts = append(parts, et.Info())
		}
	}
	parts = append(parts, transport.DeclaredTexts(m)...)
	return transport.Texts(parts)
}

func (t *Tenant) meaning(app string) string {
	var infos []platform.EntityInfo
	for _, et := range t.Projection.SortedTypes() {
		if et.Info().App == app {
			infos = append(infos, et.Info())
		}
	}
	return ontology.Meaning(infos)
}

func (t *Tenant) names(info platform.EntityInfo, q string) (string, bool) {
	candidates := []string{info.Type, info.Title, info.Plural}
	candidates = append(candidates, strings.Split(info.Synonyms, ",")...)
	for _, lang := range t.i18n.Languages() {
		dict := t.i18n.Dictionary(lang)
		for _, s := range []string{info.Title, info.Plural, info.Synonyms} {
			if tr := dict[s]; tr != "" {
				candidates = append(candidates, strings.Split(tr, ",")...)
			}
		}
	}
	candidates = append(candidates, t.termsFor(info.Type)...)
	return ontology.StripMention(q, candidates)
}

func (t *Tenant) termsFor(declaration string) []string {
	if t.knowledge == nil {
		return nil
	}
	return t.knowledge.TermsFor(declaration)
}

func (t *Tenant) declares(name string) bool {
	for _, et := range t.Projection.SortedTypes() {
		if et.Info().Type == name {
			return true
		}
		if field, ok := strings.CutPrefix(name, et.Info().Type+"."); ok {
			if _, ok := et.Info().Field(field); ok {
				return true
			}
		}
	}
	for _, a := range t.apps {
		if _, ok := a.Manifest().Actions.Action(name); ok {
			return true
		}
	}
	return false
}

func (t *Tenant) glossary(app string) string {
	if t.knowledge == nil {
		return ""
	}
	return ontology.Glossary(t.knowledge.Terms(app))
}

func (t *Tenant) Say(lang, s string) string { return t.i18n.Say(lang, s) }
