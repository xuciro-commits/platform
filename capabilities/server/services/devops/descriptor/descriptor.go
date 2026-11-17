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
				!strings.HasPrefix(step.Schema, step.App+".") || step.Target.GetType() == "" || step.Target.GetId() == "" ||
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
	for _, asset := range b.Assets {
		if seen[asset.Ref] || asset.Ref.App != "build" || asset.ContractVersion < 1 || asset.SourceVersion == "" {
			return fmt.Errorf("blueprint owner definitions need unique pinned identities")
		}
		seen[asset.Ref] = true
		if _, err := withDependencies(asset); err != nil {
			return err
		}
	}
	return nil
}
