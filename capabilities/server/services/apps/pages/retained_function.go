package pages

import (
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
