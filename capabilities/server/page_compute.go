package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"

	"platformserver/platform"
)

func (t *Tenant) pageOperation(binding *platform.AssetBinding) (platform.Operation, int, error) {
	if binding == nil || binding.Ref.Kind != platform.AssetCompute || binding.SourceVersion == "" {
		return platform.Operation{}, 0, fmt.Errorf("choose an exact published code function")
	}
	app := t.app(binding.Ref.App)
	if app != nil && binding.SourceVersion == app.Manifest().Version {
		if op, version, exists := operationDefinition(app, binding.Ref.Name, 0); exists && version == 0 && op.Binding.Kind == "native" {
			return op, 0, nil
		}
	}
	registry, err := t.assetRegistryLocked()
	if err != nil {
		return platform.Operation{}, 0, err
	}
	asset, err := registry.ResolvePinned(binding.Ref, binding.SourceVersion)
	var op platform.Operation
	version := 0
	_, ordinal, retained := strings.Cut(binding.SourceVersion, ".compute-")
	if retained {
		_, _ = fmt.Sscan(ordinal, &version)
	}
	if err != nil || asset.Ref != binding.Ref || asset.SourceVersion != binding.SourceVersion || json.Unmarshal(asset.Body, &op) != nil || op.Check() != nil || retained && version < 1 {
		return op, version, fmt.Errorf("compute binding does not match its retained schema")
	}
	return op, version, nil
}
