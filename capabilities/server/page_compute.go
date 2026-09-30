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
	owner, ok := app.(interface {
		OperationReleaseAsset(string, string) (platform.ReleaseAsset, error)
	})
	if !ok {
		return platform.Operation{}, 0, fmt.Errorf("compute has no retained owner")
	}
	asset, err := owner.OperationReleaseAsset(binding.Ref.Name, binding.SourceVersion)
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

func (t *Tenant) checkPageOperation(s platform.Section, page platform.EntityInfo) error {
	op, _, err := t.pageOperation(s.Operation)
	if err != nil {
		return err
	}
	if len(s.Inputs) == 0 {
		return nil // operator supplies the complete typed value
	}
	if op.Input.Type != "object" || len(s.Inputs) > 64 {
		return fmt.Errorf("field bindings need an object input schema")
	}
	for _, name := range op.Input.Required {
		if _, exists := s.Inputs[name]; !exists {
			return fmt.Errorf("input %s needs a binding", name)
		}
	}
	for name, binding := range s.Inputs {
		field, exists := op.Input.Properties[name]
		if !exists || binding.Check() != nil || binding.Source != "literal" && binding.Source != "input" && binding.Source != "subject" {
			return fmt.Errorf("input %s needs a supported typed binding", name)
		}
		if binding.Source == "literal" && field.Validate(binding.Value, 64<<10) != nil {
			return fmt.Errorf("input %s does not match its schema", name)
		}
		if binding.Source == "subject" && len(binding.Path) > 0 {
			if _, exists := page.Field(binding.Path[0]); !exists {
				return fmt.Errorf("input %s references an unknown source field", name)
			}
		}
	}
	return nil
}
