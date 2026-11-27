package host

import (
	"context"
	"fmt"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"time"
)

func (h *Host) routesPackages(rt *routes) {
	h.routesPackageOwnership(rt)
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/preparation", Summary: "Describe every application and target-environment prerequisite before creating or publishing a package", Answer: packages.PackagePreparation{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (packages.PackagePreparation, *platform.Error) {
		value, err := t.PackagePreparation(r.Context(), m, r.PathValue("id"), r.URL.Query().Get("version"), h.Now())
		value.TranslateProblems(func(problem string) string { return t.i18n.Say(t.Language(m, r), problem) })
		return value, err
	}))
	h.routesPackageReview(rt)
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/readiness", Summary: "Check whether the installed version's shipped reference rows are present", Answer: packages.PackageReadiness{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (packages.PackageReadiness, *platform.Error) {
		if refusal := authorizationRefusal(t.authorizeNative(t.caller(m, t.App("platform"), false), "GET /v1/packages/{id}/readiness", "read", []string{"admin"}, true)); refusal != nil {
			return packages.PackageReadiness{}, refusal
		}
		return packages.ReferenceReadiness(t.PackageOwner, r.PathValue("id"), r.URL.Query().Get("version"), t.Projection)
	}))
	rt.authorized(transport.Route{Pattern: "POST /v1/packages/inspect", Summary: "Inspect an online or local platform package without installing or executing it", Body: protocol.PackageInspectRequest{}, Answer: protocol.PackagePreview{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var req protocol.PackageInspectRequest
		if err := transport.DecodeBodyLimit(r, &req, packages.MaxPackageImportBytes/3*4+4096); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": t.Say(t.Language(m, r), err.Error())})
		} else if preview, err := t.InspectPackage(r.Context(), req); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": t.Say(t.Language(m, r), err.Error())})
		} else {
			WriteJSON(w, http.StatusOK, t.i18n.TranslatePackages(preview, t.Language(m, r)))
		}
	})
	rt.authorized(transport.Route{Pattern: "GET /v1/packages/libraries", Summary: "List configured remote package libraries and their cached catalog entries", Answer: []protocol.PackageLibraryView{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.TranslatePackageLibraries(packages.ReadLibraries(r.Context(), t.ID, packages.EffectiveLibraries(readOnly(t, func(t *Tenant) []protocol.PackageLibrary { return t.PackageOwner.PackageLibraries() })), t.files()), t.Language(m, r)))
	})
	rt.authorized(transport.Route{Pattern: "POST /v1/packages/libraries/refresh", Summary: "Refresh one or all configured remote package libraries", Body: protocol.PackageLibraryRefreshRequest{}, Answer: []protocol.PackageLibraryView{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var req protocol.PackageLibraryRefreshRequest
		_ = transport.DecodeBodyLimit(r, &req, 4096)
		libs := packages.EffectiveLibraries(readOnly(t, func(t *Tenant) []protocol.PackageLibrary { return t.PackageOwner.PackageLibraries() }))
		if views, err := packages.RefreshLibraries(r.Context(), t.ID, libs, req.ID, t.sender(), t.files()); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": t.Say(t.Language(m, r), err.Error())})
		} else {
			WriteJSON(w, http.StatusOK, t.i18n.TranslatePackageLibraries(views, t.Language(m, r)))
		}
	})
	h.routesComposition(rt)
}

func (t *Tenant) PackagePreparation(ctx context.Context, m platform.Member, id, version string, at time.Time) (packages.PackagePreparation, *platform.Error) {
	return coordinatedRecordRead(t, ctx, func(t *Tenant) (packages.PackagePreparation, *platform.Error) {
		return packages.Prepare(id, version, m, packages.PreparationHooks{
			Authorize: func() *platform.Error {
				if !m.Holds(controlowner.PlatformApp, controlowner.Admin) && !build.HoldsIndependentRole(m, build.Builder) {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Builder role required")
				}
				_, refusal := t.read(m, "packages")
				return refusal
			},
			Resolver: PublicationResolver(), Source: func() (packages.PreparationOwner, bool) { return t.PackageOwner, t.PackageOwner != nil },
			Drafts: func() (build.ConstructionDrafts, bool) { owner := t.builders[build.ID]; return owner, owner != nil },
			Records: func(typ string, q platform.Query) (int, []any, *platform.Error) {
				p, r := t.objectOwner(t.Projection).Records(packages.PreparationReader(m), typ, q, at)
				return p.Total, p.Records, r
			},
			App: (hostView{t: t}).App,
			Resolve: func(purpose, model string, options platform.ModelOptions) (string, *platform.Error) {
				b, _, r := t.modelResolver().Resolve(purpose, model, options)
				return b.Model, r
			},
			Callable:       func(model string) *platform.Error { _, _, r := t.Models.Callable(m, model); return r },
			BoundContracts: t.router().BoundContracts,
		})
	})
}

func (t *Tenant) InspectPackage(ctx context.Context, request protocol.PackageInspectRequest) (protocol.PackagePreview, error) {
	return packages.Inspect(ctx, PublicationResolver(), t.ID, request, t.sender(), t.files(), func(ctx context.Context, descriptor platform.PackageDescriptor) (protocol.Precheck, error) {
		precheck, refusal := recordRead(t, ctx, func(t *Tenant) (protocol.Precheck, *platform.Error) {
			if t.PackageOwner == nil {
				return protocol.Precheck{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Package management is unavailable")
			}
			return t.PackageOwner.Precheck(descriptor), nil
		})
		if refusal != nil {
			return protocol.Precheck{}, fmt.Errorf("The package dependencies could not be inspected")
		}
		return precheck, nil
	})
}
