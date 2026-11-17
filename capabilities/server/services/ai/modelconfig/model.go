package modelconfig

import (
	"fmt"
	"platformserver/platform"
	"slices"
)

func Resolve(m platform.AIModel, purpose string, override platform.ModelOptions, ceiling int) (platform.AIModelBinding, error) {
	o := m.Defaults.Override(override)
	b := platform.AIModelBinding{Purpose: purpose, Model: m.Name(), Options: o, Revision: m.Revision, Capabilities: m.Capabilities}
	if o.InputTokens < 0 || o.MaxTokens < 0 {
		return b, fmt.Errorf("Token budgets must be positive")
	}
	if o.MaxTokens == 0 {
		o.MaxTokens = ceiling
		if c := m.Capabilities; c != nil {
			if c.OutputTokens != nil && (o.MaxTokens == 0 || o.MaxTokens > *c.OutputTokens) {
				o.MaxTokens = *c.OutputTokens
			}
			if c.ContextTokens != nil && (o.MaxTokens == 0 || o.MaxTokens > *c.ContextTokens-o.InputTokens) {
				o.MaxTokens = max(0, *c.ContextTokens-o.InputTokens)
			}
		}
		b.Options.MaxTokens = o.MaxTokens
	}
	if ceiling > 0 && o.MaxTokens > ceiling {
		return b, fmt.Errorf("Generation budget exceeds the organization limit (%d tokens)", ceiling)
	}

	c := m.Capabilities
	if c == nil {
		if o.Thinking != "" || o.Effort != "" {
			return b, fmt.Errorf("Refresh model capabilities before configuring thinking")
		}
		return b, nil
	}
	if (purpose == "generation" || purpose == "agent") && len(c.OutputModalities) > 0 && !slices.Contains(c.OutputModalities, "text") {
		return b, fmt.Errorf("This model does not support the selected purpose")
	}
	if purpose == "agent" && c.Tools != nil && !*c.Tools {
		return b, fmt.Errorf("This model does not support agent tools")
	}
	if purpose != "" && len(c.Purposes) > 0 && !slices.Contains(c.Purposes, purpose) {
		return b, fmt.Errorf("This model does not support the selected purpose")
	}
	for _, n := range []*int{c.InputTokens, c.OutputTokens, c.ContextTokens} {
		if n != nil && *n <= 0 {
			return b, fmt.Errorf("Provider token limits must be positive or unknown")
		}
	}
	if c.InputTokens != nil && o.InputTokens > *c.InputTokens {
		return b, fmt.Errorf("Input budget exceeds the model limit (%d tokens)", *c.InputTokens)
	}
	if c.OutputTokens != nil && o.MaxTokens > *c.OutputTokens {
		return b, fmt.Errorf("Generation budget exceeds the model limit (%d tokens)", *c.OutputTokens)
	}
	if c.ContextTokens != nil && o.InputTokens+o.MaxTokens > *c.ContextTokens {
		return b, fmt.Errorf("Input and generation budgets exceed the shared context window (%d tokens)", *c.ContextTokens)
	}
	if o.Thinking != "" && !slices.Contains(c.ThinkingModes, o.Thinking) {
		return b, fmt.Errorf("This thinking mode is not supported by the model and adapter")
	}
	if o.Effort != "" && (!slices.Contains(c.EffortLevels, o.Effort) || (o.Thinking != "enabled" && o.Thinking != "adaptive")) {
		return b, fmt.Errorf("This thinking level is not supported by the selected mode")
	}
	if (o.Thinking != "" || o.Effort != "") && c.Adapter != "deepseek" && c.Adapter != "anthropic-adaptive" {
		return b, fmt.Errorf("Thinking configuration is not implemented by this adapter")
	}
	return b, nil
}
