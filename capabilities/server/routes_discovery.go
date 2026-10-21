package platformserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// routesDiscovery serves what the caller may see: catalog, apps, entities, definitions, pages, capabilities.
func (h *Host) routesDiscovery(rt *routes) {
	rt.metadata(Route{Pattern: "GET /v1/me", Summary: "Who the caller is on this host: tenant, member, the apps they may open, their language", Answer: MeView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		lang := t.Language(m, r)
		view := MeView{TenantID: m.Tenant, PrincipalID: m.ID, Profile: m, Apps: t.AppsOf(m), Tenants: h.tenantsOf(r),
			Language: lang, Languages: t.i18n.languages(), Preferred: m.Language, Currency: t.setting(t.automation(PlatformApp, false), SettingCurrency)}
		if d, ok := t.app(PlatformApp).(IdentityProfile); ok {
			view.Account, view.Tenant = d.Account(m.ID), d.TenantRecord()
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(view, lang))
	})
	rt.metadata(Route{Pattern: "GET /v1/declarations", Summary: "The data classes and their authorities the tenant's apps declare (K5)", Answer: []*pb.AuthorityDeclaration{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, t *Tenant) {
		out := []json.RawMessage{}
		for _, d := range t.Declarations() {
			raw, _ := protojson.Marshal(d)
			out = append(out, raw)
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.metadata(Route{Pattern: "GET /v1/actions", Summary: "The caller's catalog: the actions their roles permit, in their language (ADR-0057)", Answer: []platform.Action{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Catalog(m), t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/apps", Summary: "The tenant's apps from their manifests", Answer: []AppInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Apps(), t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/words", Summary: "An app's declaration texts (?app=, default build: what the tenant defined) with what every language says for each (administrators; ADR-0005)", Query: []Param{{"app", "App ID"}}, Answer: WordsView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if !m.Holds(PlatformApp, Admin) {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED})
			return
		}
		app := r.URL.Query().Get("app")
		if app == "" {
			app = BuildApp
		}
		WriteJSON(w, http.StatusOK, t.Words(app))
	})
	rt.metadata(Route{Pattern: "GET /v1/protocols", Summary: "The protocols apps provide and consume, and the provider bound to each (ADR-0006)", Answer: []ProtocolInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		protocols := t.Protocols()
		if !m.Holds(BuildApp, "builder") && !m.Holds(PlatformApp, Admin) {
			for i := range protocols {
				protocols[i].Contract = nil
			}
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(protocols, t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/entities", Summary: "The entity types of the apps the caller holds a role in, with their meaning, in their language (ADR-0046、ADR-0005)", Answer: []platform.EntityInfo{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.Entities(m), t.Language(m, r)))
	})
	rt.handle(Route{Pattern: "GET /v1/dispatches/{id}/targets", Summary: "Ready external target identities available to the dispatch's current human reviewer; no environment addresses or credentials", Answer: []DispatchTarget{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, refusal := t.DispatchTargets(m, r.PathValue("id"), h.Now())
		if refusal != nil {
			Reply(w, nil, t.i18n.said(refusal, t.Language(m, r)))
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(Route{Pattern: "GET /v1/applications/{app}/{name}/runs", Summary: "Authorized runs related to current or retained application resources; shared use does not imply exclusive application origin", Answer: ApplicationRunPage{}, Query: []Param{{"offset", "Nonnegative run offset"}, {"limit", "1–100, default 50"}, {"kind", "Run kind"}, {"state", "Original owner state"}, {"subject", "Exact type/id"}, {"since", "Inclusive RFC3339 startup time"}, {"until", "Inclusive RFC3339 startup time"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		offset, limit := pageBounds(r, 0, 50)
		filter := ApplicationRunFilter{Kind: r.URL.Query().Get("kind"), State: r.URL.Query().Get("state"), Subject: r.URL.Query().Get("subject")}
		for key, target := range map[string]*time.Time{"since": &filter.Since, "until": &filter.Until} {
			if value := r.URL.Query().Get(key); value != "" {
				parsed, err := time.Parse(time.RFC3339, value)
				if err != nil {
					Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a valid application run time range"))
					return
				}
				*target = parsed
			}
		}
		answer, err := t.ApplicationRuns(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetApp, Name: r.PathValue("name")}, offset, limit, h.Now(), filter)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(Route{Pattern: "GET /v1/applications/{app}/{name}/runtime", Summary: "Installed application artifact, retained versions, authorized capacity and original-owner recovery projection", Answer: ApplicationRuntime{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ApplicationRuntimeOf(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetApp, Name: r.PathValue("name")}, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(Route{Pattern: "GET /v1/applications/{app}/{name}/records/{type}/{id}/diagnostic", Summary: "Original authorized record decisions and application runs without time-based execution attribution", Answer: ApplicationDiagnostic{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ApplicationRecordDiagnostic(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetApp, Name: r.PathValue("name")}, r.PathValue("type"), r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.metadata(Route{Pattern: "GET /v1/definitions", Summary: "Installed object, action and page definitions the caller may discover, with qualified references and dependencies (ADR-0020)", Answer: []platform.Definition{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		definitions, err := t.definitionsFor(m)
		if err != nil {
			http.Error(w, "Asset discovery is unavailable because accepted owner metadata could not be read", http.StatusInternalServerError)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(definitions, t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/dependencies", Summary: "Authorized declarations with forward dependencies and reverse impact from the same frozen description (ADR-0056)", Query: []Param{{"app", "Asset owner; used alone to select an application"}, {"kind", "Focused asset kind"}, {"name", "Focused asset name"}}, Answer: DependencyView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		query := r.URL.Query()
		var focus platform.AssetRef
		if query.Get("kind") != "" || query.Get("name") != "" {
			focus = platform.AssetRef{App: query.Get("app"), Kind: platform.AssetKind(query.Get("kind")), Name: query.Get("name")}
		}
		answer, err := t.DependencyView(m, focus, query.Get("app"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/provides", Summary: "Capabilities offered by the same authorized asset descriptions, optionally selected by kind and side (ADR-0056)", Query: []Param{{"kind", "action, query, function, operation, protocol or interface"}, {"side", "pure, effect or authoritative"}}, Answer: []ProvidedCapability{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ProvidedCapabilities(m, r.URL.Query().Get("kind"), r.URL.Query().Get("side"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/pages/{app}/{name}/{contentVersion}", Summary: "Read exact published page content through current member discovery permissions (ADR-0015)", Answer: platform.Definition{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.PageContentDefinition(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetPage, Name: r.PathValue("name")}, r.PathValue("contentVersion"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/capabilities", Summary: "Typed Block projections of the caller's installed owner capabilities (ADR-0013)", Answer: []platform.CapabilityDescriptor{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.capabilities(m), t.Language(m, r)))
	})
	rt.metadata(Route{Pattern: "GET /v1/capabilities/{app}/{kind}/{name}", Summary: "Read a callable owner's exact retained input/output schema", Query: []Param{{"version", "Retained query/compute/AI ordinal; zero selects code declarations or the installed compute/AI version"}}, Answer: platform.CapabilityDescriptor{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		version := 0
		if text := r.URL.Query().Get("version"); text != "" {
			var err error
			version, err = strconv.Atoi(text)
			if err != nil || version < 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		answer, err := t.describeCapability(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetKind(r.PathValue("kind")), Name: r.PathValue("name")}, version)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, t.i18n.Translate(answer, t.Language(m, r)))
	})
}

type DispatchTarget struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (t *Tenant) DispatchTargets(member platform.Member, id string, at time.Time) ([]DispatchTarget, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if refusal := t.admits(member); refusal != nil {
		return nil, refusal
	}
	caller := platform.NewCaller(runtime{t}, member, construction.ID, false, false)
	run, found := platform.Get[platform.DispatchRun](caller, id)
	if !found || member.Agent || !member.Holds(construction.ID, run.Definition.ReviewerRole) || !(hostView{t: t}).Readable(member, run.Subject, at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the current dispatch reviewer may choose an external target")
	}
	out := []DispatchTarget{}
	for offset := 0; ; {
		connections, total, refusal := platform.Find[construction.Connection](t.automation(construction.ID, false), platform.Query{Domain: json.RawMessage(`[["state","=","ready"]]`), Sort: []string{"title", "id"}, Limit: 500, Offset: offset})
		if refusal != nil {
			return nil, refusal
		}
		for _, connection := range connections {
			if slices.Contains([]string{"http", "odata"}, connection.Kind) {
				out = append(out, DispatchTarget{ID: connection.ID, Title: connection.Title})
			}
		}
		offset += len(connections)
		if offset >= total {
			return out, nil
		}
	}
}
