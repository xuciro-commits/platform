package acceptedformat

import (
	"fmt"

	"platformserver/platform"
)

func CheckApplicationBindings(bindings []platform.AssetBinding) error {
	if len(bindings) > 1000 {
		return fmt.Errorf("application bindings exceed the owner budget")
	}
	seen := map[platform.AssetRef]bool{}
	for _, binding := range bindings {
		if binding.Ref.Check() != nil || binding.Ref.Kind != platform.AssetApp || binding.SourceVersion == "" || seen[binding.Ref] {
			return fmt.Errorf("application binding is invalid or repeated")
		}
		seen[binding.Ref] = true
	}
	return nil
}
