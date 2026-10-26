package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

// Preparation is a read projection of sealed package content and its original
// owner records. It is not an installation, execution plan or second registry.
type PackagePreparation struct {
	Package           string                   `json:"package"`
	Version           string                   `json:"version"`
	Applications      []PackageApplication     `json:"applications"`
	Connections       []PackageConnection      `json:"connections"`
	AI                []PackageAIRequirement   `json:"ai"`
	Roles             []PackageRoleRequirement `json:"roles"`
	Setup             *PackageSetup            `json:"setup,omitempty"`
	Installed         bool                     `json:"installed"`
	InstalledVersion  string                   `json:"installedVersion,omitempty"`
	InstallationState string                   `json:"installationState,omitempty"`
}
type PackageApplication struct {
	Ref       platform.AssetRef `json:"ref"`
	Title     string            `json:"title"`
	DraftID   string            `json:"draftId,omitempty"`
	State     string            `json:"state,omitempty"`
	Published bool              `json:"published"`
	Pages     []string          `json:"pages"`
}
type PackageConnection struct {
	Ref        platform.AssetRef `json:"ref"`
	Title      string            `json:"title"`
	DraftID    string            `json:"draftId,omitempty"`
	Kind       string            `json:"kind"`
	Credential bool              `json:"credential"`
	Configured bool              `json:"configured"`
	Ready      bool              `json:"ready"`
}
type PackageAIRequirement struct {
	Ref     platform.AssetRef `json:"ref"`
	DraftID string            `json:"draftId,omitempty"`
	Model   string            `json:"model,omitempty"`
	Ready   bool              `json:"ready"`
}
type PackageRoleRequirement struct {
	App  string `json:"app"`
	Role string `json:"role"`
	Held bool   `json:"held"`
}
type packageDraftState struct {
	platform.Record
	Name, Title, State, Address, Secret, Published, Model string
	Pages                                                 []string
}

type packagePreparationSource interface {
	PreparationBlueprint(id, version string) (PackageBlueprint, *InstalledPackage, error)
}

func (d *Console) PreparationBlueprint(id, version string) (PackageBlueprint, *InstalledPackage, error) {
	for _, view := range d.PackageViews() {
		if view.Descriptor.ID != id || view.Descriptor.Version != version {
			continue
		}
		if view.Installed != nil && view.Installed.Version == version && view.Installed.State == "active" {
			blueprint, err := d.packageBlueprint(id, version)
			return blueprint, view.Installed, err
		}
		if view.Descriptor.Blueprint != nil {
			return *view.Descriptor.Blueprint, view.Installed, nil
		}
	}
	return PackageBlueprint{}, nil, fmt.Errorf("application package %s@%s is unavailable", id, version)
}

func (h *Host) routesPackagePreparation(rt *routes) {
	rt.authorized(Route{Pattern: "GET /v1/packages/{id}/preparation", Summary: "Describe every application and target-environment prerequisite before creating or publishing a package", Answer: PackagePreparation{}}, construction.ID, []string{construction.Builder}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		value, err := t.PackagePreparation(m, r.PathValue("id"), r.URL.Query().Get("version"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, value)
	})
}

func (t *Tenant) PackagePreparation(m platform.Member, id, version string, at time.Time) (PackagePreparation, *kernel.Error) {
	return coordinatedRecordRead(t, context.Background(), func() (PackagePreparation, *kernel.Error) {
		fail := func(err error) (PackagePreparation, *kernel.Error) {
			return PackagePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
		}
		if !holdsIndependentBuildRole(m, construction.Builder) {
			return PackagePreparation{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Builder role required")
		}
		_, refusal := t.read(m, "packages")
		if refusal != nil {
			return PackagePreparation{}, refusal
		}
		source, ok := t.app(PlatformApp).(packagePreparationSource)
		if !ok {
			return fail(fmt.Errorf("package owner is unavailable"))
		}
		blueprint, installed, err := source.PreparationBlueprint(id, version)
		if err != nil {
			return fail(err)
		}
		owner, ok := t.app(construction.ID).(host.ConstructionDrafts)
		if !ok {
			return fail(fmt.Errorf("Build is unavailable"))
		}
		code, err := (hostView{t: t}).CodeReleaseAssets()
		if err != nil {
			return fail(err)
		}
		candidate, err := blueprint.candidate(owner, code)
		if err != nil {
			return fail(err)
		}
		out := PackagePreparation{Package: id, Version: version, Applications: []PackageApplication{}, Connections: []PackageConnection{}, AI: []PackageAIRequirement{}, Roles: []PackageRoleRequirement{}, Setup: blueprint.Setup}
		if installed != nil {
			out.InstalledVersion, out.InstallationState = installed.Version, installed.State
			out.Installed = installed.Version == version && installed.State == "active"
		}
		find := func(typ, name string) (packageDraftState, error) {
			page, refusal := t.recordsRead(m, typ, platform.Query{Domain: platform.Raw([]any{[]any{"name", "=", name}}), Limit: 2}, at)
			if refusal != nil {
				return packageDraftState{}, refusal
			}
			if page.Total > 1 {
				return packageDraftState{}, fmt.Errorf("draft %s/%s is ambiguous", typ, name)
			}
			var state packageDraftState
			if len(page.Records) > 0 {
				if err := json.Unmarshal(platform.Raw(page.Records[0]), &state); err != nil {
					return state, err
				}
			}
			return state, nil
		}
		role := func(app, name string) {
			if name == "" || slices.ContainsFunc(out.Roles, func(r PackageRoleRequirement) bool { return r.App == app && r.Role == name }) {
				return
			}
			out.Roles = append(out.Roles, PackageRoleRequirement{App: app, Role: name, Held: m.Holds(app, name)})
		}
		for _, root := range blueprint.Roots {
			if root.Kind != platform.AssetApp {
				continue
			}
			artifact, err := platform.ArtifactOfApplication(root, candidate.Assets)
			if err != nil {
				return fail(err)
			}
			state, err := find(construction.AppType, root.Name)
			if err != nil {
				return fail(err)
			}
			var pages []string
			for _, asset := range candidate.Assets {
				if asset.Ref == root {
					var app platform.Application
					if err := json.Unmarshal(asset.Body, &app); err != nil {
						return fail(err)
					}
					pages = app.Pages
				}
			}
			out.Applications = append(out.Applications, PackageApplication{Ref: root, Title: artifact.Title, DraftID: state.ID, State: state.State, Published: state.Published != "", Pages: pages})
			for _, r := range artifact.Roles {
				role(r.Resource.Ref.App, r.Role)
			}
		}
		for _, asset := range candidate.Assets {
			if asset.Ref.App != construction.ID {
				continue
			}
			switch asset.Ref.Kind {
			case platform.AssetConnection:
				var def construction.ConnectionDefinition
				if err := json.Unmarshal(asset.Body, &def); err != nil {
					return fail(err)
				}
				state, err := find(construction.ConnectionType, asset.Ref.Name)
				if err != nil {
					return fail(err)
				}
				out.Connections = append(out.Connections, PackageConnection{Ref: asset.Ref, Title: def.Title, DraftID: state.ID, Kind: def.Kind, Credential: def.Credential, Configured: state.Address != "" && (!def.Credential || state.Secret != ""), Ready: state.State == "ready" && (!def.Credential || state.Secret != "")})
			case platform.AssetAgent, platform.AssetFunction, platform.AssetEvaluation:
				typ := construction.FunctionType
				if asset.Ref.Kind == platform.AssetAgent {
					typ = "build.agent"
				} else if asset.Ref.Kind == platform.AssetEvaluation {
					typ = construction.TestPlanType
				}
				state, err := find(typ, asset.Ref.Name)
				if err != nil {
					return fail(err)
				}
				var def struct {
					Model string `json:"model"`
				}
				if err := json.Unmarshal(asset.Body, &def); err != nil {
					return fail(err)
				}
				model := state.Model
				if model == "" {
					model = def.Model
				}
				if model == "" {
					if asset.Ref.Kind == platform.AssetAgent {
						model = t.setting(t.automation(AgentApp, false), SettingAgentModel)
					} else {
						model = t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel)
					}
				}
				ready := false
				if model != "" && t.ai != nil {
					_, _, problem := t.ai.Callable(m, model)
					ready = problem == nil
				}
				out.AI = append(out.AI, PackageAIRequirement{Ref: asset.Ref, DraftID: state.ID, Model: model, Ready: ready})
			}
		}
		if out.Setup != nil {
			for _, r := range out.Setup.Roles {
				role(r.App, r.Role)
			}
		}
		slices.SortFunc(out.Roles, func(a, b PackageRoleRequirement) int { return strings.Compare(a.App+"/"+a.Role, b.App+"/"+b.Role) })
		return out, nil
	})
}
