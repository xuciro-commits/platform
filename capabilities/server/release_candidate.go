package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

// releaseCandidateLocked constructs a read-only release from actual owner
// declarations. It never uses Definitions(member): that discovery surface
// removes inaccessible fields and actions, so its bytes are not a contract.
// The caller holds the tenant lock while collecting builder and code assets.
func (t *Tenant) ReleaseCandidate(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.releaseCandidateLocked(roots)
}

func (t *Tenant) releaseCandidateLocked(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	var available []platform.ReleaseAsset
	for _, app := range t.apps {
		manifest := app.Manifest()
		if source, ok := app.(interface {
			ReleaseAssets() ([]platform.ReleaseAsset, error)
		}); ok {
			owned, err := source.ReleaseAssets()
			if err != nil {
				return platform.ReleaseCandidate{}, fmt.Errorf("%s release assets: %w", manifest.ID, err)
			}
			available = append(available, owned...)
		}
		for _, def := range t.definitions {
			if def.Ref.App != manifest.ID || def.Source != "code" {
				continue
			}
			switch def.Ref.Kind {
			case platform.AssetObject:
				i := slices.IndexFunc(manifest.Entities, func(e platform.Entity) bool { return e.Type == def.Ref.Name })
				if i < 0 || def.Entity == nil {
					return platform.ReleaseCandidate{}, fmt.Errorf("code object %s has no owner declaration", def.Ref)
				}
				body, err := json.Marshal(codeObjectDescriptor(manifest.Entities[i], *def.Entity))
				if err != nil {
					return platform.ReleaseCandidate{}, fmt.Errorf("encode code object %s: %w", def.Ref, err)
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
					SourceVersion: manifest.Version, Requires: def.Requires, Body: body})
			case platform.AssetAction:
				action, ok := manifest.Actions.Action(def.Ref.Name)
				if !ok {
					return platform.ReleaseCandidate{}, fmt.Errorf("code action %s has no owner declaration", def.Ref)
				}
				body, err := json.Marshal(codeActionDescriptor(action))
				if err != nil {
					return platform.ReleaseCandidate{}, fmt.Errorf("encode code action %s: %w", def.Ref, err)
				}
				available = append(available, platform.ReleaseAsset{Ref: def.Ref, ContractVersion: def.ContractVersion,
					SourceVersion: manifest.Version, Requires: def.Requires, Body: body})
			case platform.AssetPage:
				if def.Page == nil {
					return platform.ReleaseCandidate{}, fmt.Errorf("code page %s has no owner declaration", def.Ref)
				}
				asset, err := platform.PageReleaseAsset(manifest.ID, manifest.Version, *def.Page)
				if err != nil {
					return platform.ReleaseCandidate{}, err
				}
				available = append(available, asset)
			}
		}
		if len(manifest.Pages) > 0 {
			pages := make([]string, len(manifest.Pages))
			for i, page := range manifest.Pages {
				pages[i] = page.Name
			}
			asset, err := platform.ApplicationReleaseAsset(manifest.ID, manifest.Version,
				platform.Application{Name: manifest.ID, Title: manifest.Title, Pages: pages})
			if err != nil {
				return platform.ReleaseCandidate{}, err
			}
			available = append(available, asset)
		}
	}
	return platform.Candidate(roots, available)
}

// Code behavior is pinned by Manifest.Version rather than serialized Go
// callbacks (ADR-0039 D3). This body preserves the complete declarative
// security and lifecycle contract that JSON discovery intentionally omits.
func codeObjectDescriptor(entity platform.Entity, info platform.EntityInfo) any {
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

func codeActionDescriptor(action platform.Action) any {
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
