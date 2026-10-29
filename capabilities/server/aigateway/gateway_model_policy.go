package aigateway

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"platformserver/aigateway/providers"
	"platformserver/platform"
	"slices"
	"strings"
	"sync"
	"time"
)

var catalogs = struct {
	sync.Mutex
	byKey map[string]cachedCatalog
}{byKey: map[string]cachedCatalog{}}

type cachedCatalog struct {
	at     time.Time
	models []platform.CatalogModel
}

func ReadModels(tenant string, pv platform.AIProvider, secret []byte, version int, refresh bool, client providers.Client) ([]platform.CatalogModel, *platform.AIError) {
	config, _ := json.Marshal(pv)
	key := fmt.Sprintf("%s/%x/%d/%x", tenant, sha256.Sum256(config), version, sha256.Sum256(secret))
	catalogs.Lock()
	cached, ok := catalogs.byKey[key]
	catalogs.Unlock()
	if ok && !refresh && time.Since(cached.at) < 10*time.Minute {
		return cached.models, nil
	}
	entries, failure := client.Models(pv)
	if failure != nil {
		return nil, failure
	}
	models := []platform.CatalogModel{}
	for _, entry := range entries {
		model, err := CatalogModel(pv, entry, time.Now().UTC())
		if err != nil {
			return nil, &platform.AIError{Status: 502, Detail: err.Error()}
		}
		models = append(models, model)
	}

	slices.SortFunc(models, func(a, b platform.CatalogModel) int { return strings.Compare(a.ID, b.ID) })
	catalogs.Lock()
	catalogs.byKey[key] = cachedCatalog{at: time.Now(), models: models}
	catalogs.Unlock()
	return models, nil
}
func CatalogModel(pv platform.AIProvider, raw json.RawMessage, at time.Time) (platform.CatalogModel, error) {
	var d struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Context     *int   `json:"context_length"`
		Window      *int   `json:"context_window"`
		Input       *int   `json:"max_input_tokens"`
		Output      *int   `json:"max_output_tokens"`
		MaxTokens   *int   `json:"max_tokens"`
		TopProvider struct {
			Output *int `json:"max_completion_tokens"`
		} `json:"top_provider"`
		Pricing *struct {
			Prompt     *string `json:"prompt"`
			Completion *string `json:"completion"`
		} `json:"pricing"`
		Effort struct {
			Levels []string `json:"supported_levels"`
		} `json:"effort"`
		API          map[string]json.RawMessage `json:"api_capabilities"`
		Capabilities struct {
			Thinking struct {
				Types struct {
					Enabled struct {
						Supported bool `json:"supported"`
					} `json:"enabled"`
					Adaptive struct {
						Supported bool `json:"supported"`
					} `json:"adaptive"`
				} `json:"types"`
			} `json:"thinking"`
			Effort map[string]struct {
				Supported bool `json:"supported"`
			} `json:"effort"`
			Structured *struct {
				Supported bool `json:"supported"`
			} `json:"structured_outputs"`
		} `json:"capabilities"`
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
		Parameters       []string `json:"supported_parameters"`
		Architecture     struct {
			Input  []string `json:"input_modalities"`
			Output []string `json:"output_modalities"`
		} `json:"architecture"`
	}
	if json.Unmarshal(raw, &d) != nil || d.ID == "" {
		return platform.CatalogModel{}, fmt.Errorf("The provider returned an invalid model")
	}
	c := &platform.ModelCapabilities{Source: pv.BaseURL + "/models", ObservedAt: at, InputTokens: d.Input, OutputTokens: d.Output, ContextTokens: d.Context, Parameters: d.Parameters, InputModalities: d.InputModalities, OutputModalities: d.OutputModalities}
	if c.ContextTokens == nil {
		c.ContextTokens = d.Window
	}
	if c.OutputTokens == nil {
		c.OutputTokens = d.TopProvider.Output
	}
	if c.OutputTokens == nil {
		c.OutputTokens = d.MaxTokens
	}
	if len(c.InputModalities) == 0 {
		c.InputModalities = d.Architecture.Input
	}
	if len(c.OutputModalities) == 0 {
		c.OutputModalities = d.Architecture.Output
	}
	if len(d.Parameters) > 0 {
		tools := slices.Contains(d.Parameters, "tools")
		c.Tools = &tools
		structured := slices.Contains(d.Parameters, "response_format") || slices.Contains(d.Parameters, "structured_outputs")
		c.Structured = &structured
	}
	if slices.Contains(d.Architecture.Output, "embeddings") {
		c.Purposes = []string{"embedding"}
	}
	if slices.Contains(d.Architecture.Output, "text") || slices.Contains(d.OutputModalities, "text") {
		c.Purposes = []string{"generation", "agent"}
	}
	if pv.Vendor == "deepseek" && len(d.Effort.Levels) > 0 {
		c.Adapter = "deepseek"
		c.ThinkingModes = []string{"enabled", "disabled"}
		c.DeclaredThinkingModes = []string{"enabled", "disabled"}
		c.EffortLevels = d.Effort.Levels
		c.Purposes = []string{"generation", "agent"}
	}
	if pv.Wire == "anthropic" {
		c.Purposes = []string{"generation", "agent"}
		if d.Capabilities.Structured != nil {
			c.Structured = &d.Capabilities.Structured.Supported
		}
		if d.Capabilities.Thinking.Types.Enabled.Supported {
			c.DeclaredThinkingModes = append(c.DeclaredThinkingModes, "enabled")
		}
		if d.Capabilities.Thinking.Types.Adaptive.Supported {
			c.DeclaredThinkingModes = append(c.DeclaredThinkingModes, "adaptive")
			c.Adapter = "anthropic-adaptive"
			c.ThinkingModes = []string{"adaptive", "disabled"}
			for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
				if d.Capabilities.Effort[level].Supported {
					c.EffortLevels = append(c.EffortLevels, level)
				}
			}
		}
	}
	for _, limit := range []**int{&c.InputTokens, &c.OutputTokens, &c.ContextTokens} {
		if *limit != nil && **limit <= 0 {
			*limit = nil
		}
	}
	m := platform.CatalogModel{ID: d.ID, Name: d.Name, Capabilities: c}
	if m.Name == "" {
		m.Name = d.DisplayName
	}
	if c.ContextTokens != nil {
		m.Context = *c.ContextTokens
	}
	if d.Pricing != nil && d.Pricing.Prompt != nil && d.Pricing.Completion != nil {
		m.PriceKnown = true
		m.Free = *d.Pricing.Prompt == "0" && *d.Pricing.Completion == "0"
	}
	return m, nil
}

func PurposeSetting(purpose string) (string, string) {
	switch purpose {
	case "agent":
		return platform.AgentApp, platform.SettingAgentModel
	case "embedding":
		return platform.KnowledgeApp, platform.SettingKnowledgeEmbeddingModel
	default:
		return platform.AIApp, platform.AISettingAppModel
	}
}

func ConfigurationDigest(model platform.AIModel, provider platform.AIProvider, binding *platform.AIModelBinding) (string, error) {
	identity := []any{model, provider}
	if binding != nil {
		identity = append(identity, *binding)
	}
	return platform.CanonicalDigest(identity)
}
