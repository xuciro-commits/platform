package host

import (
	"fmt"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	releaseowner "platformserver/services/devops/releases"
	build "platformserver/services/resources/authoring"
	"platformserver/services/resources/publication"
	controlowner "platformserver/services/security/control"
	"strings"
	"time"
)

func (h hostView) PrepareManagedPackage(c platform.Caller, descriptor platform.PackageDescriptor, available []platform.ReleaseAsset, archive, upgrade, downgrade bool) (func(*pb.ChangeRecord), *platform.Error) {
	if c.Replaying || descriptor.Blueprint == nil && !archive {
		return nil, nil
	}
	d := h.decision
	managed := packages.NewManaged(platform.NewCaller(d, c.Member, build.ID, true, false), descriptor, func() *records.Store { return d.Records }, func(installation platform.ReleaseInstallation) {
		d.Installations = append(d.Installations, installation)
	}, d.FailDecision)
	return packages.ManagedPreparation{
		Managed: managed, Archive: archive, Available: available, At: d.At, Upgrade: upgrade, Downgrade: downgrade,
		Owner: h.t.builders[build.ID], Resolver: PublicationResolver(), Code: h.CodeReleaseAssets,
		Impact: func(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
			return h.t.packageLifecycleImpact(c.Member, id, after)
		},
		Bootstrap: func(candidate runtimeprotocol.ReleaseCandidate, context runtimeprotocol.AssetReleaseContext) error {
			_, _, err := h.t.bootstrapOwnerDraftsLockedWithArchived(candidate, context, false, nil, true, d)
			return err
		},
		Registry:        func() (*publication.AssetRegistry, error) { return d.tenant.assetRegistryLocked() },
		Projection:      h.t.Projection,
		CheckExtensions: func() error { return d.tenant.checkExtensionSet() },
		CheckStorage:    func(upgrade, downgrade bool) error { return h.t.validateAssetStorage(d.tenant, upgrade, downgrade) },
	}.Prepare()
}

func (t *Tenant) prepareBatchDefinitions(rows *records.Store, installations []platform.ReleaseInstallation) (*Tenant, error) {
	definitionRows := rows.ForkRecords()
	packages.RestoreReferencedResources(definitionRows, installations)
	view, err := t.assetInstallationView(definitionRows)
	if err != nil {
		return nil, err
	}
	registry, err := view.assetRegistryLocked()
	if err != nil {
		return nil, err
	}
	view.installingSet = true
	err = releaseowner.StageInstallations(registry, installations, runtimeprotocol.AssetApplyContext{Private: true},
		func(plan runtimeprotocol.AssetInstallation, _ platform.ReleaseInstallation) error {
			typ, id, _ := strings.Cut(plan.Record, "/")
			if et := rows.Type(typ); et == nil || et.Rows().Get(id) == nil {
				return fmt.Errorf("installed resource %s has no accepted image", plan.Record)
			}
			return nil
		}, view.registerInstalledAsset)
	if err != nil {
		return nil, err
	}
	view.installingSet = false
	if err := view.Projection.Anchor(t.Projection); err != nil {
		return nil, err
	}
	return view, nil
}

func (t *Tenant) baseModules() []runtimeprotocol.CapabilityModule {
	return packages.Composition{
		Base:     []runtimeprotocol.CapabilityModule{t.services.RuleModule, t.services.AuthModule, t.services.KernelModule, t.services.StorageModule},
		Apps:     t.apps,
		Bindings: func() map[string]string { return t.bindings.All() },
		Entity: func(typ string) (platform.RecordTypeInfo, bool) {
			if et := t.Projection.Type(typ); et != nil {
				return et.Info(), true
			}
			return platform.RecordTypeInfo{}, false
		},
		PlatformApp: controlowner.PlatformApp, Directory: "enterprise",
	}.Modules()
}

func (h *Host) routesComposition(rt *routes) {
	rt.authorized(transport.Route{Pattern: "GET /v1/composition", Summary: "Selected runtime contracts, implementations and immutable dependency order", Answer: runtimeprotocol.CompositionPlan{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		plan, err := packages.ActiveComposition(t.baseModules(), t.OwnerActive)
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, plan)
	})
}

func (h hostView) PackageLifecycleImpact(c platform.Caller, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	return h.t.packageLifecycleImpact(c.Member, id, after)
}

func (t *Tenant) packageLifecycleImpact(m platform.Member, id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
	ctx := t.applicationContext()
	return packages.StoredImpact(t.Projection, t.definitions, t.OwnerActive, t.releaseExtensionReview, func(ref platform.AssetRef, offset int) (runtimeprotocol.ApplicationRunPage, *platform.Error) {
		return ctx.Runs(m, ref, offset, 100, time.Now())
	}).Of(id, after)
}

func (h *Host) routesPackageOwnership(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/ownership", Summary: "Review an explicit resource ownership transfer before installing a split product", Answer: packages.PackageOwnership{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.App("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/ownership", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.App("platform").(interface {
			PackageViews() []runtimeprotocol.PackageView
		})
		plan, err := packages.OwnershipFor(catalog.PackageViews(), r.PathValue("id"), r.URL.Query().Get("version"), t.Projection, t.builders[build.ID])
		if err == nil && plan == nil {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "The exact product version is unavailable"})
			return
		}
		transport.AnswerOr(w, http.StatusConflict, plan, err)
	})
}

func (h *Host) routesPackageReview(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/review", Summary: "Review the exact product's resource differences and customer extensions", Answer: packages.PackageReview{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.App("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/review", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.App("platform").(interface {
			PackageViews() []runtimeprotocol.PackageView
			LifecycleImpact(platform.Caller, string, []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error)
		})
		views := catalog.PackageViews()
		chosen := packages.FindView(views, r.PathValue("id"), r.URL.Query().Get("version"))
		if chosen == nil {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "The exact product version is unavailable"})
			return
		}
		answer, err := packages.Review{
			Resolver:  PublicationResolver(),
			Blueprint: t.builders[build.ID].CompleteBlueprintAssets,
			Impact: func(id string, after []platform.ReleaseAsset) (platform.PackageLifecycleImpact, error) {
				return catalog.LifecycleImpact(c, id, after)
			},
		}.Of(views, chosen, r.URL.Query().Get("mode") == "uninstall")
		transport.AnswerOr(w, http.StatusConflict, answer, err)
	})
}
