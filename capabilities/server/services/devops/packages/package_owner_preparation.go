package packages

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type PackagePreparation struct {
	Package           string                   `json:"package"`
	Version           string                   `json:"version"`
	Applications      []PackageApplication     `json:"applications"`
	Connections       []PackageConnection      `json:"connections"`
	AI                []PackageAIRequirement   `json:"ai"`
	Roles             []PackageRoleRequirement `json:"roles"`
	Setup             *platform.PackageSetup   `json:"setup,omitempty"`
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
	Problem string            `json:"problem,omitempty"`
	Ref     platform.AssetRef `json:"ref"`
	DraftID string            `json:"draftId,omitempty"`
	Model   string            `json:"model,omitempty"`
	Ready   bool              `json:"ready"`
}

type PackageRoleRequirement struct {
	App       string `json:"app"`
	Role      string `json:"role"`
	Held      bool   `json:"held"`
	Available bool   `json:"available"`
}

type DraftState struct {
	platform.Record
	Name, Title, State, Address, Secret, Published, Model string
	ModelOptions                                          platform.ModelOptions `json:"modelOptions,omitempty"`
	MaxTokens                                             int                   `json:"maxTokens,omitempty"`
	Pages                                                 []string
}

func Preparation(id, version string, blueprint platform.PackageBlueprint, installed *platform.InstalledPackage, candidate platform.ReleaseCandidate, m platform.Member, source PreparationSource) (PackagePreparation, error) {
	out := PackagePreparation{Package: id, Version: version, Applications: []PackageApplication{}, Connections: []PackageConnection{}, AI: []PackageAIRequirement{}, Roles: []PackageRoleRequirement{}, Setup: blueprint.Setup}
	if installed != nil {
		out.InstalledVersion, out.InstallationState = installed.Version, installed.State
		out.Installed = installed.Version == version && installed.State == "active"
	}
	role := func(app, name string) {
		if name == "" || slices.ContainsFunc(out.Roles, func(r PackageRoleRequirement) bool { return r.App == app && r.Role == name }) {
			return
		}
		owner := source.App(app)
		out.Roles = append(out.Roles, PackageRoleRequirement{App: app, Role: name, Held: m.Holds(app, name), Available: owner != nil && slices.Contains(owner.Manifest().AllRoles(), name)})
	}
	for _, root := range blueprint.Roots {
		if root.Kind != platform.AssetApp {
			continue
		}
		artifact, err := platform.ArtifactOfApplication(root, candidate.Assets)
		if err != nil {
			return PackagePreparation{}, err
		}
		state, err := source.Find(build.AppType, root.Name)
		if err != nil {
			return PackagePreparation{}, err
		}
		var pages []string
		for _, asset := range candidate.Assets {
			if asset.Ref == root {
				var app platform.Application
				if err := json.Unmarshal(asset.Body, &app); err != nil {
					return PackagePreparation{}, err
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
		if asset.Ref.App != build.ID {
			continue
		}
		switch asset.Ref.Kind {
		case platform.AssetConnection:
			var def build.ConnectionDefinition
			if err := json.Unmarshal(asset.Body, &def); err != nil {
				return PackagePreparation{}, err
			}
			state, err := source.Find(build.ConnectionType, asset.Ref.Name)
			if err != nil {
				return PackagePreparation{}, err
			}
			out.Connections = append(out.Connections, PackageConnection{Ref: asset.Ref, Title: def.Title, DraftID: state.ID, Kind: def.Kind, Credential: def.Credential, Configured: state.Address != "" && (!def.Credential || state.Secret != ""), Ready: state.State == "ready" && (!def.Credential || state.Secret != "")})
		case platform.AssetAgent, platform.AssetFunction, platform.AssetEvaluation:
			typ := build.FunctionType
			if asset.Ref.Kind == platform.AssetAgent {
				typ = "build.agent"
			} else if asset.Ref.Kind == platform.AssetEvaluation {
				typ = build.TestPlanType
			}
			state, err := source.Find(typ, asset.Ref.Name)
			if err != nil {
				return PackagePreparation{}, err
			}
			var def struct {
				Model        string                `json:"model"`
				ModelOptions platform.ModelOptions `json:"modelOptions,omitempty"`
				MaxTokens    int                   `json:"maxTokens,omitempty"`
			}
			if err := json.Unmarshal(asset.Body, &def); err != nil {
				return PackagePreparation{}, err
			}
			model, options, tokens := def.Model, def.ModelOptions, def.MaxTokens
			if state.ID != "" {
				model, options, tokens = state.Model, state.ModelOptions, state.MaxTokens
			}
			purpose := "generation"
			if asset.Ref.Kind == platform.AssetAgent {
				purpose = "agent"
			}
			if asset.Ref.Kind == platform.AssetFunction {
				options = options.Override(platform.ModelOptions{MaxTokens: tokens})
			}
			resolved, problem := source.Model(purpose, model, options)
			ready := problem == ""
			if ready {
				model = resolved
			}

			out.AI = append(out.AI, PackageAIRequirement{Ref: asset.Ref, DraftID: state.ID, Model: model, Ready: ready, Problem: problem})
		}
	}
	if out.Setup != nil {
		for _, r := range out.Setup.Roles {
			role(r.App, r.Role)
		}
	}
	slices.SortFunc(out.Roles, func(a, b PackageRoleRequirement) int { return strings.Compare(a.App+"/"+a.Role, b.App+"/"+b.Role) })
	return out, nil
}

type PreparationSource struct {
	Find  func(string, string) (DraftState, error)
	App   func(string) platform.App
	Model func(string, string, platform.ModelOptions) (string, string)
}

func Inspect(ctx context.Context, tenant string, request platform.PackageInspectRequest, send func(*http.Request, bool) (*http.Response, error), files platform.FileStore, check func(context.Context, platform.PackageDescriptor) (platform.Precheck, error)) (platform.PackagePreview, error) {
	var preview platform.PackagePreview
	content, origin := []byte(request.Content), "local-file"
	if (request.URL == "") == (len(content) == 0) {
		return preview, fmt.Errorf("Choose one package source: a URL or a local package file")
	}
	if request.URL != "" {
		address, err := url.Parse(request.URL)
		if err != nil || address.Scheme != "https" || address.Host == "" || address.User != nil || address.Fragment != "" {
			return preview, fmt.Errorf("Use an HTTPS address for a platform package, without embedded credentials")
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
		if err != nil {
			return preview, err
		}
		req.Header.Set("Accept", "application/json")
		response, err := send(req, false)
		if err != nil {
			return preview, fmt.Errorf("The package could not be downloaded; check its address and try again")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return preview, fmt.Errorf("The package source answered HTTP %d", response.StatusCode)
		}
		content, err = io.ReadAll(io.LimitReader(response.Body, MaxPackageImportBytes+1))
		if err != nil {
			return preview, fmt.Errorf("The package download could not be read")
		}
		address.RawQuery, address.ForceQuery = "", false
		origin = address.String()
	}
	if len(content) == 0 || len(content) > MaxPackageImportBytes {
		return preview, fmt.Errorf("A platform package must be non-empty and at most 16 MiB")
	}
	descriptor, err := DecodeContent(content)
	if err != nil {
		return preview, err
	}
	if err := descriptor.Validate(); err != nil {
		return preview, err
	}
	precheck, err := check(ctx, descriptor)
	if err != nil {
		return preview, err
	}
	raw, err := json.Marshal(platform.Offer{Descriptor: descriptor, Origin: origin})
	if err != nil {
		return platform.PackagePreview{}, err
	}
	sum := sha256.Sum256(raw)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if err = files.Put(ctx, OfferKey(tenant, digest), raw, "application/json"); err != nil {
		return platform.PackagePreview{}, err
	}
	return platform.PackagePreview{Descriptor: descriptor, Digest: digest, Origin: origin, Precheck: precheck}, nil

}
