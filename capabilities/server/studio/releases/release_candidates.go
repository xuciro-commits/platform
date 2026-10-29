package releases

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"platformserver/platform"
	"platformserver/studio/build"
	"slices"
	"strings"
)

// A changed object or page can affect a page or application above it. Include
// those owners in the preview roots so its ID and diff describe the actual
// impacted closure, not just the object picked in the editor.
func DependentRoots(ref platform.AssetRef, available []platform.ReleaseAsset) []platform.AssetRef {
	descriptors := make([]platform.Dependencies, 0, len(available))
	for _, asset := range available {
		descriptors = append(descriptors, platform.Dependencies{Ref: asset.Ref, Requires: asset.Requires, Provides: asset.Provides, Version: asset.SourceVersion})
	}
	graph, err := platform.NewDependencyGraph(descriptors)
	if err != nil {
		return []platform.AssetRef{ref}
	}
	roots, err := graph.Impact([]platform.AssetRef{ref})
	if err != nil {
		return []platform.AssetRef{ref}
	}
	return roots
}

// ReleasePreview is a builder-only comparison between the installed
// development definition and one saved draft. The IDs are candidate IDs,
// not published versions or an active release pointer.
type ReleasePreview struct {
	CurrentID   string              `json:"currentId,omitempty"`
	CandidateID string              `json:"candidateId,omitempty"`
	Included    []platform.AssetRef `json:"included"`
	Added       []platform.AssetRef `json:"added"`
	Removed     []platform.AssetRef `json:"removed"`
	Changed     []platform.AssetRef `json:"changed"`
	Diagnostic  string              `json:"diagnostic,omitempty"`
	// Drafts is the exact saved-draft selection this review was built from, in
	// the order it was selected: the provenance of a joint
	// candidate. One draft keeping its installed dependency is not repeated.
	Drafts []build.JointDraftRef `json:"drafts,omitempty"`
	// CandidateActions are owner-compiled draft inputs for builder test forms,
	// not the installed member catalog or permission to execute.
	CandidateActions []platform.Action `json:"candidateActions"`
}

type ReleasePreviewRequest struct {
	ApplicationDrafts bool               `json:"applicationDrafts,omitempty"`
	Kind              platform.AssetKind `json:"kind,omitempty"`
	ID                string             `json:"id,omitempty"`
	// Drafts selects several saved drafts as one joint candidate.
	// A request carries either one kind/id or a non-empty drafts list.
	Drafts []build.JointDraftRef `json:"drafts,omitempty"`
}

type ReleaseSaveRequest struct {
	Kind        platform.AssetKind    `json:"kind,omitempty"`
	ID          string                `json:"id,omitempty"`
	Drafts      []build.JointDraftRef `json:"drafts,omitempty"`
	CandidateID string                `json:"candidateId"`
	Key         string                `json:"key"`
}

// ReleaseDraftsRequest names one saved draft to list dependencies for.
type ReleaseDraftsRequest struct {
	Kind platform.AssetKind `json:"kind"`
	ID   string             `json:"id"`
}

// ReleaseDraftClosure is the saved record drafts that must be delivered with
// the chosen draft, in dependency order breadth-first from it.
type ReleaseDraftClosure struct {
	Drafts []build.JointDraftRef `json:"drafts"`
}

type ReleaseSaved struct {
	ID string `json:"id"`
}

// ReleaseActivateRequest moves the tenant's release pointer to a saved candidate.
type ReleaseActivateRequest struct {
	CandidateID string `json:"candidateId"`
	Key         string `json:"key"`
	UpgradeID   string `json:"upgradeId,omitempty"`
}

// ReleaseActive is the tenant's active release, empty before any activation.
type ReleaseActive struct {
	ID string `json:"id"`
}

// Code behavior is pinned by Manifest.Version rather than serialized Go
// callbacks. This body preserves the complete declarative
// security and lifecycle contract that JSON discovery intentionally omits.
func CodeObjectDescriptor(entity platform.Entity, info platform.EntityInfo) any {
	type scope struct {
		Levels       map[string]string `json:"levels"`
		Default      string            `json:"default"`
		Structure    string            `json:"structure"`
		Unit         string            `json:"unit"`
		Owner        string            `json:"owner"`
		Participants bool              `json:"participants"`
		Through      bool              `json:"through"`
	}
	type transition struct {
		Name, Title, Description string
		From, To                 []string
		Roles                    []string
		Payload                  []platform.Field
		Capability               string
		Approval                 any
		Do, After                bool
	}
	type lifecycle struct {
		Field, Initial string
		States         []platform.State
		Transitions    []transition
	}
	var steps *lifecycle
	if entity.Lifecycle != nil {
		steps = &lifecycle{Field: entity.Lifecycle.Field, Initial: entity.Lifecycle.Initial, States: entity.Lifecycle.States}
		for _, step := range entity.Lifecycle.Transitions {
			steps.Transitions = append(steps.Transitions, transition{
				Name: step.Name, Title: step.Title, Description: step.Description, From: step.From, To: step.To,
				Roles: step.Roles, Payload: step.Payload, Capability: step.Capability,
				Approval: codeApprovalDescriptor(step.Approval), Do: step.Do != nil, After: step.After != nil,
			})
		}
	}
	return struct {
		Type      string                `json:"type"`
		Entity    platform.EntityInfo   `json:"entity"`
		Scope     scope                 `json:"scope"`
		Standard  platform.Standard     `json:"standard"`
		Derived   []platform.Derivation `json:"derived"`
		Withheld  string                `json:"withheld"`
		Lifecycle *lifecycle            `json:"lifecycle"`
	}{
		Type: info.Type, Entity: info,
		Scope: scope{Levels: entity.Scope.Levels, Default: entity.Scope.Default, Structure: entity.Scope.Structure,
			Unit: entity.Scope.Unit, Owner: entity.Scope.Owner, Participants: entity.Scope.Participants != nil,
			Through: entity.Scope.Through != nil},
		Standard: entity.Standard, Derived: entity.Derived, Withheld: entity.Withheld, Lifecycle: steps,
	}
}

func CodeActionDescriptor(action platform.Action) any {
	return struct {
		Schema   string          `json:"schema"`
		Target   string          `json:"target"`
		Action   platform.Action `json:"action"`
		Roles    []string        `json:"roles"`
		Approval any             `json:"approval"`
	}{Schema: action.Schema, Target: action.Target, Action: action, Roles: action.Roles,
		Approval: codeApprovalDescriptor(action.Approval)}
}

func codeApprovalDescriptor(approval *platform.Approval) any {
	if approval == nil {
		return nil
	}
	type level struct {
		Title, Structure, Role, AppRole, Member string
		All                                     bool
		HasWhen                                 bool
		Due                                     int64
		WorkingDays                             int
	}
	levels := make([]level, 0, len(approval.Levels))
	for _, l := range approval.Levels {
		levels = append(levels, level{Title: l.Title, Structure: l.Structure, Role: l.Role, AppRole: l.AppRole,
			Member: l.Member, All: l.All, HasWhen: l.When != nil, Due: int64(l.Due), WorkingDays: l.WorkingDays})
	}
	return struct {
		Pending, Rejected string
		Levels            []level
	}{Pending: approval.Pending, Rejected: approval.Rejected, Levels: levels}
}

// A page or flow's explicit binding survives newer installations. Resolve each edge
// through its original owner before the language-neutral candidate validator
// checks the complete bytes. A single candidate cannot contain two versions
// of one named asset, or replace an explicitly requested asset root.
func CandidateWithBindings(roots []platform.AssetRef, available []platform.ReleaseAsset, fixed []platform.AssetRef, resolve func(platform.AssetRef, string, ...string) (platform.ReleaseAsset, error)) (platform.ReleaseCandidate, error) {
	assets := slices.Clone(available)
	indices := map[platform.AssetRef]int{}
	for i, asset := range assets {
		indices[asset.Ref] = i
	}
	visited := map[platform.AssetRef]bool{}
	pins := map[platform.AssetRef]string{}
	contentPins := map[platform.AssetRef]string{}
	// Rebuild the traversal after selecting retained page bytes. This discards
	// bindings collected from superseded descriptors, regardless of root order.
	restart := errors.New("restart retained page closure")
	var visit func(platform.AssetRef) error
	visit = func(ref platform.AssetRef) error {
		if visited[ref] {
			return nil
		}
		visited[ref] = true
		i, ok := indices[ref]
		if !ok {
			return fmt.Errorf("release requires missing asset %s", ref)
		}
		asset := assets[i]
		var bindings []platform.AssetBinding
		if ref.Kind == platform.AssetObject {
			var object struct {
				Fields []struct {
					Property *platform.AssetBinding `json:"property,omitempty"`
				} `json:"fields"`
				Sets    []platform.RecordSet `json:"sets,omitempty"`
				Actions []struct {
					Transaction *platform.BusinessTxn `json:"transaction,omitempty"`
				} `json:"actions,omitempty"`
			}
			if json.Unmarshal(asset.Body, &object) != nil {
				return fmt.Errorf("invalid property consumer")
			}
			for _, set := range object.Sets {
				if set.Binding != nil {
					bindings = append(bindings, *set.Binding)
				}
			}
			var bindSteps func([]platform.TxnStep)
			bindSteps = func(steps []platform.TxnStep) {
				for _, step := range steps {
					if step.Link != nil {
						bindings = append(bindings, step.Link.Binding)
					}
					if step.ForEach != nil {
						bindSteps(step.ForEach.Steps)
					}
				}
			}
			for _, action := range object.Actions {
				if action.Transaction != nil {
					bindSteps(action.Transaction.Steps)
				}
			}
			for _, f := range object.Fields {
				if f.Property != nil {
					bindings = append(bindings, *f.Property)
				}
			}
		} else if ref.Kind == platform.AssetEvaluation {
			descriptor, err := platform.ReadEvaluationSet(asset.Body)
			if err != nil {
				return err
			}
			bindings = append(bindings, descriptor.Function)
		} else if ref.Kind == platform.AssetFlow {
			var flow platform.FlowReleaseDescriptor
			if err := json.Unmarshal(asset.Body, &flow); err != nil {
				return err
			}
			bindings = append(slices.Clone(flow.Functions), flow.Operations...)
			bindings = append(bindings, flow.Queries...)
		} else if ref.Kind == platform.AssetApp {
			var app platform.Application
			if err := json.Unmarshal(asset.Body, &app); err != nil {
				return err
			}
			for _, q := range app.Queries {
				if q.Query != nil {
					bindings = append(bindings, *q.Query)
				}
			}
		} else if ref.Kind == platform.AssetPage {
			var page platform.Page
			if err := json.Unmarshal(asset.Body, &page); err != nil {
				return err
			}
			projected, err := page.Safety()
			if err != nil {
				return err
			}
			for _, binding := range projected.DataBindings {
				if binding.Query != nil {
					bindings = append(bindings, *binding.Query)
				}
				for _, model := range binding.ModelBindings() {
					if model.Set != nil && model.Set.Query != nil {
						bindings = append(bindings, *model.Set.Query)
					}
					if model.Options != nil && model.Options.Set != nil && model.Options.Set.Query != nil {
						bindings = append(bindings, *model.Options.Set.Query)
					}
				}
				for _, e := range binding.Resources {
					if e.Kind == "page" && e.Asset != nil {
						ref := e.Asset.Ref
						if prior := contentPins[ref]; prior != "" && prior != e.ContentVersion {
							return fmt.Errorf("release binds conflicting contents of page %s", ref)
						}
						if prior := pins[ref]; prior != "" && prior != e.Asset.SourceVersion {
							return fmt.Errorf("release binds conflicting source versions of page %s", ref)
						}
						contentPins[ref], pins[ref] = e.ContentVersion, e.Asset.SourceVersion
						index, ok := indices[ref]
						if !ok {
							return fmt.Errorf("embedded page %s is unavailable", ref)
						}
						var current platform.Page
						if json.Unmarshal(assets[index].Body, &current) != nil {
							return fmt.Errorf("embedded page descriptor is invalid")
						}
						digest, err := platform.PageContentVersion(current)
						if err != nil {
							return err
						}
						if digest != e.ContentVersion || assets[index].SourceVersion != e.Asset.SourceVersion {
							if slices.Contains(fixed, ref) {
								return fmt.Errorf("embedded page %s differs from its explicit release root", ref)
							}
							asset, err := resolve(ref, e.Asset.SourceVersion, e.ContentVersion)
							if err != nil {
								return err
							}
							if assets[index].SourceVersion != asset.SourceVersion {
								return fmt.Errorf("embedded page source version is incompatible")
							}
							assets[index] = asset
							return restart
						}
					}
					if e.Asset != nil && e.Asset.SourceVersion != "" {
						bindings = append(bindings, *e.Asset)
					}
				}
			}
			for _, trigger := range projected.ActionTriggers {
				if trigger.Function != nil {
					bindings = append(bindings, *trigger.Function)
				}
				if trigger.Operation != nil {
					bindings = append(bindings, *trigger.Operation)
				}
			}
			for _, q := range projected.InputSchema.Queries {
				if q.Query != nil {
					bindings = append(bindings, *q.Query)
				}
			}
		}
		for _, binding := range bindings {
			if binding.Ref.Check() != nil || binding.SourceVersion == "" {
				return fmt.Errorf("%s has an invalid version binding", ref)
			}
			if prior := pins[binding.Ref]; prior != "" && prior != binding.SourceVersion {
				return fmt.Errorf("release binds conflicting versions of asset %s", binding.Ref)
			}
			pins[binding.Ref] = binding.SourceVersion
		}
		for _, dep := range asset.Requires {
			if err := visit(dep); err != nil {
				return err
			}
		}
		return nil
	}
	for pass := 0; ; pass++ {
		clear(visited)
		clear(pins)
		clear(contentPins)
		var err error
		for _, root := range roots {
			if err = visit(root); err != nil {
				break
			}
		}
		if err == nil {
			break
		}
		if !errors.Is(err, restart) {
			return platform.ReleaseCandidate{}, err
		}
		if pass >= 4*len(assets) {
			return platform.ReleaseCandidate{}, fmt.Errorf("page content bindings do not converge")
		}
	}
	for _, ref := range slices.SortedFunc(maps.Keys(pins), func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) }) {
		version := pins[ref]
		i, exists := indices[ref]
		if !exists {
			return platform.ReleaseCandidate{}, fmt.Errorf("release requires missing bound asset %s", ref)
		}
		if assets[i].SourceVersion == version {
			continue
		}
		if slices.Contains(fixed, ref) {
			return platform.ReleaseCandidate{}, fmt.Errorf("asset %s differs from the version pinned by its dependents", ref)
		}
		asset, err := resolve(ref, version)
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		if asset.Ref != ref || asset.SourceVersion != version {
			return platform.ReleaseCandidate{}, fmt.Errorf("asset %s returned a different retained version", ref)
		}
		assets[i] = asset
	}
	return platform.Candidate(roots, assets)
}

func CodeReleaseAssets(manifest platform.Manifest, definitions []platform.Definition, special map[platform.AssetRef]platform.ReleaseAsset) ([]platform.ReleaseAsset, error) {
	var available []platform.ReleaseAsset
	for _, def := range definitions {
		if def.Ref.App != manifest.ID || def.Source != "code" {
			continue
		}
		switch def.Ref.Kind {
		case platform.AssetInterface, platform.AssetProtocol, platform.AssetObservation:
			available = append(available, special[def.Ref])
		case platform.AssetObject:
			i := slices.IndexFunc(manifest.Entities, func(e platform.Entity) bool { return e.Type == def.Ref.Name })
			if i < 0 || def.Entity == nil {
				return nil, fmt.Errorf("code object %s has no owner declaration", def.Ref)
			}
			body, err := json.Marshal(CodeObjectDescriptor(manifest.Entities[i], *def.Entity))
			if err != nil {
				return nil, fmt.Errorf("encode code object %s: %w", def.Ref, err)
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
				SourceVersion: manifest.Version, Body: body})
		case platform.AssetAction:
			action, ok := manifest.Actions.Action(def.Ref.Name)
			if !ok {
				return nil, fmt.Errorf("code action %s has no owner declaration", def.Ref)
			}
			body, err := json.Marshal(CodeActionDescriptor(action))
			if err != nil {
				return nil, fmt.Errorf("encode code action %s: %w", def.Ref, err)
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
				SourceVersion: manifest.Version, Body: body})
		case platform.AssetPropertyType:
			if def.PropertyType == nil {
				return nil, fmt.Errorf("property has no descriptor")
			}
			body, err := json.Marshal(def.PropertyType)
			if err != nil {
				return nil, err
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, SourceVersion: manifest.Version, ContractVersion: def.ContractVersion, Body: body})
		case platform.AssetLinkType:
			if def.LinkType == nil {
				return nil, fmt.Errorf("code link type has no descriptor")
			}
			body, err := json.Marshal(def.LinkType)
			if err != nil {
				return nil, err
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, SourceVersion: manifest.Version, ContractVersion: def.ContractVersion, Body: body})
		case platform.AssetQuery:
			if def.Query == nil {
				return nil, fmt.Errorf("code query %s has no owner declaration", def.Ref)
			}
			body, err := json.Marshal(def.Query)
			if err != nil {
				return nil, fmt.Errorf("encode code query %s: %w", def.Ref, err)
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
				SourceVersion: manifest.Version, Body: body})
		case platform.AssetFlow:
			if def.Flow == nil {
				return nil, fmt.Errorf("native flow has no owner descriptor")
			}
			available = append(available, special[def.Ref])
		case platform.AssetFunction:
			if def.Function == nil {
				return nil, fmt.Errorf("code function %s has no owner declaration", def.Ref)
			}
			body, err := json.Marshal(def.Function)
			if err != nil {
				return nil, err
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
				SourceVersion: manifest.Version, Body: body})
		case platform.AssetAgent:
			if def.Agent == nil {
				return nil, fmt.Errorf("code agent has no owner descriptor")
			}
			body, err := json.Marshal(def.Agent)
			if err != nil {
				return nil, err
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion, SourceVersion: manifest.Version, Body: body})
		case platform.AssetCompute:
			if def.Operation == nil {
				return nil, fmt.Errorf("compute %s has no owner declaration", def.Ref)
			}
			body, err := json.Marshal(def.Operation)
			if err != nil {
				return nil, err
			}
			available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion, SourceVersion: manifest.Version, Body: body})
		case platform.AssetPage:
			if def.Page == nil {
				return nil, fmt.Errorf("code page %s has no owner declaration", def.Ref)
			}
			asset, err := platform.PageReleaseAsset(manifest.ID, manifest.Version, *def.Page)
			if err != nil {
				return nil, err
			}
			available = append(available, asset)
		}
	}
	pages := make([]string, len(manifest.Pages))
	for i, page := range manifest.Pages {
		pages[i] = page.Name
	}
	resources := []platform.AssetRef{}
	for _, asset := range available {
		if asset.Ref.Kind != platform.AssetPage && asset.Ref.Kind != platform.AssetApp {
			resources = append(resources, asset.Ref)
		}
	}
	asset, err := platform.ApplicationReleaseAsset(manifest.ID, manifest.Version,
		platform.Application{Name: manifest.ID, Title: manifest.Title, Pages: pages, Resources: UniqueRefs(resources)})
	if err != nil {
		return nil, err
	}
	available = append(available, asset)
	return available, nil
}
