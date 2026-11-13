package host

import (
	"cmp"
	"context"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"io"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	transport "platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"strconv"
	"time"
)

func (h *Host) routesDiscovery(rt *routes) {
	rt.metadata(transport.Route{Pattern: "GET /v1/me", Summary: "Who the caller is on this host: tenant, member, the apps they may open, their language", Answer: MeView{}}, translating(func(r *http.Request, m platform.Member, t *Tenant) (MeView, *kernel.Error) {
		view := MeView{TenantID: m.Tenant, PrincipalID: m.ID, Profile: m, Apps: t.AppsOf(m), Tenants: h.tenantsOf(r),
			Language: t.Language(m, r), Languages: t.i18n.Languages(), Preferred: m.Language, Currency: t.setting(t.automation(controlowner.PlatformApp, false), controlowner.SettingCurrency)}
		if d, ok := t.App(controlowner.PlatformApp).(platform.IdentityProfile); ok {
			view.Account, view.Tenant = d.Account(m.ID), d.TenantRecord()
		}
		return view, nil
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/declarations", Summary: "The data classes and their authorities the tenant's apps declare (K5)", Answer: []*pb.AuthorityDeclaration{}}, transport.Answering(func(_ *http.Request, _ platform.Member, t *Tenant) ([]json.RawMessage, *kernel.Error) {
		out := []json.RawMessage{}
		for _, d := range t.Declarations() {
			raw, _ := protojson.Marshal(d)
			out = append(out, raw)
		}
		return out, nil
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/actions", Summary: "The caller's catalog: the actions their roles permit, in their language", Answer: []platform.Action{}}, viewRoute("catalog", "Catalog is unavailable"))
	rt.metadata(transport.Route{Pattern: "GET /v1/apps", Summary: "The tenant's apps from their manifests", Answer: []transport.AppInfo{}}, translating(func(_ *http.Request, _ platform.Member, t *Tenant) ([]transport.AppInfo, *kernel.Error) {
		return t.Apps(), nil
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/words", Summary: "An app's declaration texts (?app=, default build: what the tenant defined) with what every language says for each (administrators)", Query: []transport.Param{{Name: "app", Description: "App ID"}}, Answer: WordsView{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (WordsView, *kernel.Error) {
		if !m.Holds(controlowner.PlatformApp, controlowner.Admin) {
			return WordsView{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
		return t.Words(cmp.Or(r.URL.Query().Get("app"), controlowner.BuildApp)), nil
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/protocols", Summary: "The protocols apps provide and consume, and the provider bound to each", Answer: []actions.ProtocolInfo{}}, translating(func(_ *http.Request, m platform.Member, t *Tenant) ([]actions.ProtocolInfo, *kernel.Error) {
		protocols := t.router().Protocols()
		if !m.Holds(controlowner.BuildApp, "builder") && !m.Holds(controlowner.PlatformApp, controlowner.Admin) {
			for i := range protocols {
				protocols[i].Contract = nil
			}
		}
		return protocols, nil
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/entities", Summary: "The entity types of the apps the caller holds a role in, with their meaning, in their language", Answer: []platform.EntityInfo{}}, viewRoute("entities", "Entity declarations are unavailable"))
	rt.handle(transport.Route{Pattern: "GET /v1/dispatches/{id}/targets", Summary: "Ready external target identities available to the dispatch's current human reviewer; no environment addresses or credentials", Answer: []build.DispatchTarget{}}, saying(func(r *http.Request, m platform.Member, t *Tenant) ([]build.DispatchTarget, *kernel.Error) {
		return t.DispatchTargets(m, r.PathValue("id"), h.Now())
	}))
	rt.handle(transport.Route{Pattern: "GET /v1/applications/{app}/{name}/runs", Summary: "Authorized runs related to current or retained application resources; shared use does not imply exclusive application origin", Answer: platform.ApplicationRunPage{}, Query: []transport.Param{{Name: "offset", Description: "Nonnegative run offset"}, {Name: "limit", Description: "1–100, default 50"}, {Name: "kind", Description: "Run kind"}, {Name: "state", Description: "Original owner state"}, {Name: "subject", Description: "Exact type/id"}, {Name: "since", Description: "Inclusive RFC3339 startup time"}, {Name: "until", Description: "Inclusive RFC3339 startup time"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.ApplicationRunPage, *kernel.Error) {
		offset, limit := pageBounds(r, 0, 50)
		filter, refusal := transport.ApplicationRunFilterOf(r.URL.Query())
		if refusal != nil {
			return platform.ApplicationRunPage{}, refusal
		}
		return t.ApplicationRuns(m, applicationRef(r), offset, limit, h.Now(), filter)
	}))
	rt.handle(transport.Route{Pattern: "GET /v1/applications/{app}/{name}/runtime", Summary: "Installed application artifact, retained versions, authorized capacity and original-owner recovery projection", Answer: platform.ApplicationRuntime{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.ApplicationRuntime, *kernel.Error) {
		return t.ApplicationRuntimeOf(m, applicationRef(r), h.Now())
	}))
	rt.handle(transport.Route{Pattern: "GET /v1/applications/{app}/{name}/records/{type}/{id}/diagnostic", Summary: "Original authorized record decisions and application runs without time-based execution attribution", Answer: platform.ApplicationDiagnostic{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.ApplicationDiagnostic, *kernel.Error) {
		return t.ApplicationRecordDiagnostic(m, applicationRef(r), r.PathValue("type"), r.PathValue("id"), h.Now())
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/definitions", Summary: "Installed object, action and page definitions the caller may discover, with qualified references and dependencies", Answer: []platform.Definition{}}, viewRoute("definitions", "Asset discovery is unavailable because accepted owner metadata could not be read"))
	rt.metadata(transport.Route{Pattern: "GET /v1/dependencies", Summary: "Authorized declarations with forward dependencies and reverse impact from the same frozen description", Query: []transport.Param{{Name: "app", Description: "Asset owner; used alone to select an application"}, {Name: "kind", Description: "Focused asset kind"}, {Name: "name", Description: "Focused asset name"}}, Answer: releaseowner.DependencyView{}}, translating(func(r *http.Request, m platform.Member, t *Tenant) (releaseowner.DependencyView, *kernel.Error) {
		query := r.URL.Query()
		var focus platform.AssetRef
		if query.Get("kind") != "" || query.Get("name") != "" {
			focus = platform.AssetRef{App: query.Get("app"), Kind: platform.AssetKind(query.Get("kind")), Name: query.Get("name")}
		}
		return t.describer().DependencyView(m, focus, query.Get("app"))
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/provides", Summary: "Capabilities offered by the same authorized asset descriptions, optionally selected by kind and side", Query: []transport.Param{{Name: "kind", Description: "action, query, function, operation, protocol or interface"}, {Name: "side", Description: "pure, effect or authoritative"}}, Answer: []releaseowner.ProvidedCapability{}}, translating(func(r *http.Request, m platform.Member, t *Tenant) ([]releaseowner.ProvidedCapability, *kernel.Error) {
		return t.describer().ProvidedCapabilities(m, r.URL.Query().Get("kind"), r.URL.Query().Get("side"))
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/pages/{app}/{name}/{contentVersion}", Summary: "Read exact published page content through current member discovery permissions", Answer: platform.Definition{}}, translating(func(r *http.Request, m platform.Member, t *Tenant) (platform.Definition, *kernel.Error) {
		return t.PageContentDefinition(m, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetPage, Name: r.PathValue("name")}, r.PathValue("contentVersion"))
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/capabilities", Summary: "Typed Block projections of the caller's installed owner capabilities", Answer: []platform.CapabilityDescriptor{}}, translating(func(_ *http.Request, m platform.Member, t *Tenant) ([]platform.CapabilityDescriptor, *kernel.Error) {
		return t.capabilities(m), nil
	}))
	rt.metadata(transport.Route{Pattern: "GET /v1/capabilities/{app}/{kind}/{name}", Summary: "Read a callable owner's exact retained input/output schema", Query: []transport.Param{{Name: "version", Description: "Retained query/compute/AI ordinal; zero selects code declarations or the installed compute/AI version"}}, Answer: platform.CapabilityDescriptor{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
		t.answerIn(w, r, m, answer, err)
	})
}

func applicationRef(r *http.Request) platform.AssetRef {
	return platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetApp, Name: r.PathValue("name")}
}

func viewRoute(kind, failure string) func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) {
	return func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		if err := t.WriteView(kind, m, t.Language(m, r), w, r); err != nil {
			http.Error(w, failure, http.StatusInternalServerError)
		}
	}
}

func translating[R any](f func(*http.Request, platform.Member, *Tenant) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) {
	return func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, refusal := f(r, m, t)
		t.answerIn(w, r, m, out, refusal)
	}
}

func (t *Tenant) answerIn(w http.ResponseWriter, r *http.Request, m platform.Member, answer any, refusal *kernel.Error) {
	if refusal == nil {
		answer = t.i18n.Translate(answer, t.Language(m, r))
	}
	transport.Answer(w, answer, refusal)
}

func saying[R any](f func(*http.Request, platform.Member, *Tenant) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) {
	return func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, refusal := f(r, m, t)
		transport.Answer(w, out, t.i18n.Said(refusal, t.Language(m, r)))
	}
}

func (t *Tenant) DispatchTargets(member platform.Member, id string, at time.Time) ([]build.DispatchTarget, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) ([]build.DispatchTarget, *kernel.Error) {
		return build.DispatchTargets(build.DispatchTargetHooks{
			Admits:     t.Admits,
			Caller:     func() platform.Caller { return platform.NewCaller(runtime{t: t}, member, build.ID, false, false) },
			Automation: func() platform.Caller { return t.automation(build.ID, false) },
			Readable: func(member platform.Member, subject string, at time.Time) bool {
				return (hostView{t: t}).Readable(member, subject, at)
			},
		}, member, id, at)
	})
}

func (t *Tenant) WriteView(kind string, m platform.Member, lang string, w http.ResponseWriter, r *http.Request) error {
	m = t.authorizationMember(m)
	return t.viewCache().WriteView(t.definitionsFingerprint(), transport.Rights(m)+"/"+strconv.FormatUint(t.Projection.Generation(), 10), kind, lang, w, r, func() (any, error) {
		switch kind {
		case "entities":
			return t.Entities(m), nil
		case "catalog":
			return t.Catalog(m), nil
		case "definitions":
			return t.DefinitionsFor(m)
		}
		return nil, nil
	}, t.i18n.Translate)
}

func (h *Host) routesApplications(rt *routes) {
	rt.metadata(transport.Route{Pattern: "GET /v1/applications", Summary: "Every application this member may open, with the pages each offers", Answer: []platform.ApplicationView{}},
		translating(func(_ *http.Request, m platform.Member, t *Tenant) ([]platform.ApplicationView, *kernel.Error) {
			return t.Applications(m), nil
		}))
	rt.metadata(transport.Route{Pattern: "POST /v1/page-view", Summary: "One page of an application resolved for the caller: its object, fields and the actions they may take", Body: PageRequest{}, Answer: platform.PageView{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var body PageRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF || body.App == "" || body.Page == "" {
			Reply(w, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A page view names one application and one page"))
			return
		}
		view, refusal := t.PageView(m, body.App, body.Owner, body.Page)
		t.answerIn(w, r, m, view, refusal)
	})
}

type PageRequest struct {
	App   string `json:"app"`
	Owner string `json:"owner,omitempty"`
	Page  string `json:"page"`
}
