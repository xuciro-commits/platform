package descriptor

import (
	"encoding/json"
	"fmt"
	"strings"

	"platformserver/platform"
)

func Validate(p platform.PackageDescriptor, withDependencies func(platform.ReleaseAsset) (platform.ReleaseAsset, error)) error {
	if p.ID == "" || p.Version == "" || p.Namespace == "" {
		return fmt.Errorf("a package needs an ID, a version and a namespace")
	}
	for _, value := range []string{p.ID, p.Namespace, p.Version} {
		if len(value) > 200 || value == "." || value == ".." || strings.ContainsAny(value, "/\\") || strings.IndexFunc(value, func(r rune) bool { return r <= ' ' || r == 127 }) >= 0 {
			return fmt.Errorf("package ID, namespace and version must be safe path segments")
		}
	}
	if _, err := ParsePackageVersion(p.Version); err != nil {
		return err
	}
	for _, need := range p.Requires {
		id, constraint := PackageDependency(need)
		if id == "" || id == p.ID {
			return fmt.Errorf("a product dependency names another product")
		}
		if constraint != "" && constraint != "*" {
			for _, alternative := range strings.Split(constraint, "||") {
				for _, term := range strings.Fields(alternative) {
					if _, err := ParsePackageVersion(strings.TrimLeft(term, "<>=^~")); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, c := range p.Contributions {
		if c.View == "" {
			return fmt.Errorf("package %s: a contribution needs a view", p.ID)
		}
	}
	for _, need := range p.Capabilities {
		if err := need.Check(); err != nil {
			return err
		}
	}
	if p.Blueprint == nil {
		return nil
	}
	b := p.Blueprint
	if len(b.Roots) == 0 || len(b.Assets) == 0 || len(b.Assets) > 1000 {
		return fmt.Errorf("a blueprint needs roots and at most 1000 owner definitions")
	}
	if b.Setup != nil {
		setup := b.Setup
		if len(setup.Steps) == 0 || len(setup.Steps) > 128 || len(setup.Roles) > 32 {
			return fmt.Errorf("a setup plan needs 1–128 requests and at most 32 role requirements")
		}
		for _, role := range setup.Roles {
			if role.App == "" || role.Role == "" || len(role.App) > 128 || len(role.Role) > 128 {
				return fmt.Errorf("setup role requirements need an app and role")
			}
		}
		seen := map[string]bool{}
		for _, step := range setup.Steps {
			if step.Name == "" || len(step.Name) > 128 || seen[step.Name] || step.App == "" ||
				platform.OwnerOfType(step.Schema) != step.App || step.Target.GetType() == "" || step.Target.GetId() == "" ||
				len(step.Payload) > 64<<10 || !json.Valid([]byte(step.Payload)) {
				return fmt.Errorf("setup requests need unique names, an owner, schema, target and bounded JSON payload")
			}
			seen[step.Name] = true
		}
	}
	for _, root := range b.Roots {
		if root.Check() != nil || root.App != "build" {
			return fmt.Errorf("a package blueprint has only Build-owned roots")
		}
	}
	seen := map[platform.AssetRef]bool{}
	objects := map[string]bool{}
	for _, asset := range b.Assets {
		if seen[asset.Ref] || asset.Ref.App != "build" || asset.ContractVersion < 1 || asset.SourceVersion == "" {
			return fmt.Errorf("blueprint owner definitions need unique pinned identities")
		}
		seen[asset.Ref] = true
		if asset.Ref.Kind == platform.AssetObject {
			objects[asset.Ref.Name] = true
		}
		if _, err := withDependencies(asset); err != nil {
			return err
		}
	}
	if len(b.Seed) > 200 {
		return fmt.Errorf("a package ships at most 200 demo seed records")
	}
	for _, need := range p.Requires {
		required, _, _ := strings.Cut(need, "@")
		objects[required+".*"] = true
	}
	seeded := map[string]bool{}
	rows := 0
	for _, raw := range b.Seed {
		var seed struct {
			Kind   string           `json:"kind"`
			Type   string           `json:"type"`
			ID     string           `json:"id"`
			Values map[string]any   `json:"values"`
			Rows   []map[string]any `json:"rows"`
		}
		known := json.Unmarshal(raw, &seed) == nil && (objects[seed.Type] || objects[strings.SplitN(seed.Type, ".", 2)[0]+".*"])
		if seed.Kind == "data" {
			rows += len(seed.Rows)
			if len(raw) > 64<<10 || !known || len(seed.Rows) == 0 || seed.ID != "" || seed.Values != nil || rows > 20000 {
				return fmt.Errorf("package data comes in chunks of at most 64 KiB for one object type, with at most 20000 rows in all")
			}
			for _, row := range seed.Rows {
				id, _ := row["id"].(string)
				if id == "" || seeded[seed.Type+"/"+id] {
					return fmt.Errorf("package data rows need unique ids")
				}
				seeded[seed.Type+"/"+id] = true
			}
			continue
		}
		if len(raw) > 64<<10 || !known || seed.Kind != "" || seed.ID == "" || seed.Values == nil {
			return fmt.Errorf("seed records need unique object type and id pairs with bounded JSON object values")
		}
		key := seed.Type + "/" + seed.ID
		if seeded[key] {
			return fmt.Errorf("seed records need unique object type and id pairs with bounded JSON object values")
		}
		seeded[key] = true
	}
	return nil
}
