package host

import (
	"context"
	"fmt"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	"platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
	"time"
)

func (h *Host) routesPackages(rt *routes) {
	h.routesPackageOwnership(rt)
	h.routesPackagePreparation(rt)
	h.routesPackageSources(rt)
	h.routesComposition(rt)
}

func (h *Host) routesPackagePreparation(rt *routes) {
	rt.authorized(transport.Route{Pattern: "GET /v1/packages/{id}/preparation", Summary: "Describe every application and target-environment prerequisite before creating or publishing a package", Answer: packages.PackagePreparation{}}, build.ID, []string{build.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		value, err := t.PackagePreparation(r.Context(), m, r.PathValue("id"), r.URL.Query().Get("version"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		for i := range value.AI {
			value.AI[i].Problem = t.i18n.Say(t.Language(m, r), value.AI[i].Problem)
		}
		WriteJSON(w, http.StatusOK, value)
	})
}

func (t *Tenant) PackagePreparation(ctx context.Context, m platform.Member, id, version string, at time.Time) (packages.PackagePreparation, *kernel.Error) {
	return coordinatedRecordRead(t, ctx, func(t *Tenant) (packages.PackagePreparation, *kernel.Error) {
		return packages.Prepare(id, version, m, packages.PreparationHooks{
			Authorize: func() *kernel.Error {
				if !build.HoldsIndependentRole(m, build.Builder) {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Builder role required")
				}
				_, refusal := t.read(m, "packages")
				return refusal
			},
			Source: func() (packages.PreparationOwner, bool) {
				source, ok := t.App(controlowner.PlatformApp).(packages.PreparationOwner)
				return source, ok
			},
			Drafts: func() (build.ConstructionDrafts, bool) {
				owner, ok := t.App(build.ID).(build.ConstructionDrafts)
				return owner, ok
			},
			Records: func(typ string, q platform.Query) (int, []any, *kernel.Error) {
				page, refusal := t.recordsRead(m, typ, q, at)
				return page.Total, page.Records, refusal
			},
			App: (hostView{t: t}).App,
			Model: func(purpose, model string, options platform.ModelOptions) (string, string) {
				binding, _, refusal := t.modelResolver().Resolve(purpose, model, options)
				if refusal != nil {
					problem := refusal.Message
					if problem == "" {
						problem = "The selected model is disabled or missing"
					}
					return model, problem
				}
				if _, _, refusal = t.ai.Callable(m, binding.Model); refusal != nil {
					return binding.Model, "Ask an AI administrator to grant access to this model"
				}
				return binding.Model, ""
			},
		})
	})
}

func (h *Host) routesPackageSources(rt *routes) {
	h.routesPackageReview(rt)
	rt.authorized(transport.Route{Pattern: "POST /v1/packages/inspect", Summary: "Inspect an online or local platform package without installing or executing it", Body: platform.PackageInspectRequest{}, Answer: platform.PackagePreview{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request platform.PackageInspectRequest
		if err := transport.DecodeBodyLimit(r, &request, packages.MaxPackageImportBytes+4096); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": t.Say(t.Language(m, r), err.Error())})
			return
		}
		preview, err := t.inspectPackage(r.Context(), request)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": t.Say(t.Language(m, r), err.Error())})
			return
		}
		WriteJSON(w, http.StatusOK, preview)
	})
}

func (t *Tenant) inspectPackage(ctx context.Context, request platform.PackageInspectRequest) (platform.PackagePreview, error) {
	send := t.sender()
	return packages.Inspect(ctx, t.ID, request, send, t.files(), func(ctx context.Context, descriptor platform.PackageDescriptor) (platform.Precheck, error) {
		precheck, refusal := recordRead(t, ctx, func(t *Tenant) (platform.Precheck, *kernel.Error) {
			owner, ok := t.App(controlowner.PlatformApp).(interface {
				Precheck(platform.PackageDescriptor) platform.Precheck
			})
			if !ok {
				return platform.Precheck{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Package management is unavailable")
			}
			return owner.Precheck(descriptor), nil
		})
		if refusal != nil {
			return platform.Precheck{}, fmt.Errorf("The package dependencies could not be inspected")
		}
		return precheck, nil
	})
}
