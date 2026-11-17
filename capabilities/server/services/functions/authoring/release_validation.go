package authoring

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"platformserver/platform"
)

func AssetDefinition(asset platform.ReleaseAsset) (platform.AIFunction, int, error) {
	var definition platform.AIFunction
	if err := json.Unmarshal(asset.Body, &definition); err != nil {
		return definition, 0, err
	}
	version := 1
	if _, ordinal, found := strings.Cut(asset.SourceVersion, ".function-"); found {
		parsed, err := strconv.Atoi(ordinal)
		if err != nil || parsed < 1 {
			return definition, 0, fmt.Errorf("Invalid function asset version")
		}
		version = parsed
	}
	return definition, version, definition.Check()
}
