package build

import (
	"platformserver/platform"
	"strings"
	"testing"
)

func TestAgentModelConfigurationSurvivesReleaseInstallation(t *testing.T) {
	original := Agent{Name: "assistant", Title: "Assistant", Description: "Review input", Instructions: "Check the supplied input", Version: 1, Model: "provider/reviewer", ModelOptions: platform.ModelOptions{MaxTokens: 256, Thinking: "enabled", Effort: "high"}}
	asset, err := agentReleaseAsset(original, "1")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeAgentAsset(asset)
	if err != nil {
		t.Fatal(err)
	}
	runtime := decoded.Definition()
	if runtime.Model != original.Model || runtime.ModelOptions != original.ModelOptions {
		t.Fatal("published agent lost its model binding", runtime.Model, runtime.ModelOptions)
	}
	original.Model, original.ModelOptions = "", platform.ModelOptions{}
	legacy, err := agentReleaseAsset(original, "1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy.Body), `"model"`) || strings.Contains(string(legacy.Body), `"modelOptions"`) {
		t.Fatal("empty optional configuration changed retained agent bodies")
	}
}
