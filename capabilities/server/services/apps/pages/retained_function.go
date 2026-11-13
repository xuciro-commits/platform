package pages

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"platformserver/platform"
)

func RetainedFunction(app platform.App, binding platform.AssetBinding) (platform.AIFunction, bool) {
	owner, ok := app.(platform.FunctionDefinitions)
	if !ok {
		return platform.AIFunction{}, false
	}
	text, ok := strings.CutPrefix(binding.SourceVersion, app.Manifest().Version+".function-")
	version, err := strconv.Atoi(text)
	if !ok || err != nil {
		return platform.AIFunction{}, false
	}
	value, _, exists := owner.FunctionDefinition(binding.Ref.Name, version)
	return value, exists
}

type PinnedOperation struct {
	App        func(string) platform.App
	Definition func(app platform.App, name string, version int) (platform.Operation, int, bool)
	Registry   func() (*platform.AssetRegistry, error)
}

func (p PinnedOperation) Resolve(binding *platform.AssetBinding) (platform.Operation, int, error) {
	if binding == nil || binding.Ref.Kind != platform.AssetCompute || binding.SourceVersion == "" {
		return platform.Operation{}, 0, fmt.Errorf("choose an exact published code function")
	}
	app := p.App(binding.Ref.App)
	if app != nil && binding.SourceVersion == app.Manifest().Version {
		if op, version, exists := p.Definition(app, binding.Ref.Name, 0); exists && version == 0 && op.Binding.Kind == "native" {
			return op, 0, nil
		}
	}
	registry, err := p.Registry()
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
