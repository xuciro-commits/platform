package aigateway

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ai/modelconfig"
)

type Models interface {
	platform.AcceptedInputApp
	Snapshot() (json.RawMessage, error)
	Callable(m platform.Member, name string) (platform.AIModel, platform.AIProvider, *platform.Error)
	Model(name string) (platform.AIModel, platform.AIProvider, *platform.Error)
	Provider(id string) (platform.AIProvider, bool)
	Meter(u platform.AIUsage)
	Spent(member string, now time.Time) int
	Reservation(string) (runtimeprotocol.AIReservation, bool)
	Usage() []platform.AIUsage
	Quota(platform.Member, runtimeprotocol.AIDefaults, time.Time) platform.AIQuota
}

func Limits(setting func(app, name string) string) runtimeprotocol.AIDefaults {
	number := func(app, name string) int {
		n, _ := strconv.Atoi(setting(app, name))
		return n
	}
	return runtimeprotocol.AIDefaults{People: number(runtimeprotocol.AIApp, runtimeprotocol.AISettingDailyTokens), Agents: number(runtimeprotocol.AgentApp, runtimeprotocol.SettingAgentDaily),
		PerMinute: number(runtimeprotocol.AIApp, runtimeprotocol.AISettingPerMinute)}
}

type Registry interface {
	Model(name string) (platform.AIModel, platform.AIProvider, *platform.Error)
}

type Resolver struct {
	Registry Registry
	Setting  func(app, key string) string
	Policy   runtimeprotocol.AIInferencePolicy
}

func ResolverFor(setting func(string, string) string, policy *runtimeprotocol.AIInferencePolicy, registry Registry) Resolver {
	r := Resolver{Setting: setting, Registry: registry}
	if policy != nil {
		r.Policy = *policy
	}
	return r
}

func (r Resolver) MostTokens() int {
	n, _ := strconv.Atoi(r.Setting(runtimeprotocol.AIApp, runtimeprotocol.AISettingMaxTokens))
	return n
}

func (r Resolver) Resolve(purpose, name string, override platform.ModelOptions) (platform.AIModelBinding, platform.AIProvider, *platform.Error) {
	app, key := PurposeSetting(purpose)
	if name == "" {
		name = r.Setting(app, key)
	}
	if r.Registry == nil || name == "" {
		return platform.AIModelBinding{}, platform.AIProvider{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Choose a default model in AI configuration")
	}
	if purpose == "agent" && name == "@generation" {
		name = r.Setting(runtimeprotocol.AIApp, runtimeprotocol.AISettingAppModel)
	}
	model, pv, err := r.Registry.Model(name)
	if err != nil {
		return platform.AIModelBinding{}, pv, err
	}
	var defaults platform.ModelOptions
	if raw := r.Setting(app, key+"-options"); raw != "" {
		if json.Unmarshal([]byte(raw), &defaults) != nil {
			return platform.AIModelBinding{}, pv, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The purpose model options are invalid")
		}
	}
	b, problem := modelconfig.Resolve(model, purpose, defaults.Override(override), r.MostTokens())
	b.Provider = &pv
	if problem != nil {
		return b, pv, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
	}
	return b, pv, nil
}

func (r Resolver) CheckSetting(id, value string) *platform.Error {
	if id == runtimeprotocol.AIApp+"/"+runtimeprotocol.AISettingMaxTokens || id == runtimeprotocol.AIApp+"/"+runtimeprotocol.AISettingDailyTokens || id == runtimeprotocol.AIApp+"/"+runtimeprotocol.AISettingPerMinute || id == runtimeprotocol.AgentApp+"/"+runtimeprotocol.SettingAgentDaily {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Token and call limits cannot be negative")
		}
	}
	for _, purpose := range []string{"generation", "agent", "embedding"} {
		app, key := PurposeSetting(purpose)
		if id != app+"/"+key && id != app+"/"+key+"-options" {
			continue
		}
		name := value
		var options platform.ModelOptions
		if id == app+"/"+key+"-options" {
			if value == "" {
				return nil
			}
			if value != "" && json.Unmarshal([]byte(value), &options) != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The model options are invalid")
			}
			name = r.Setting(app, key)
		}
		if name == "" {
			return nil
		}
		if id == app+"/"+key+"-options" {
			if purpose == "agent" && name == "@generation" {
				name = r.Setting(runtimeprotocol.AIApp, runtimeprotocol.AISettingAppModel)
			}
			model, _, err := r.Registry.Model(name)
			if err != nil {
				return err
			}
			if _, problem := modelconfig.Resolve(model, purpose, options, r.MostTokens()); problem != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
			}
			return nil
		}
		_, _, err := r.Resolve(purpose, name, options)
		return err
	}
	return nil
}

func (r Resolver) Stage(binding platform.AIModelBinding, provider platform.AIProvider, stage string) (platform.AIModelBinding, *platform.Error) {
	pinned, err := Bind(r.Policy, provider, binding, stage)
	if err != nil {
		return pinned, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
	}
	return pinned, nil
}

func BindPolicy(services *runtimeprotocol.RuntimeCapabilities) error {
	if services.AIInference == nil {
		policy := runtimeprotocol.AIInferencePolicy{}
		if raw := strings.TrimSpace(os.Getenv("PLATFORM_AI_INFERENCE_POLICY")); raw != "" {
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&policy); err != nil {
				return fmt.Errorf("PLATFORM_AI_INFERENCE_POLICY: %w", err)
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				return fmt.Errorf("PLATFORM_AI_INFERENCE_POLICY contains trailing data")
			}
		}
		services.AIInference = &policy
	}
	if err := CheckPolicy(*services.AIInference); err != nil {
		return err
	}
	policy := *services.AIInference
	policy.Runtime = append([]runtimeprotocol.AIInferenceSite{}, policy.Runtime...)
	services.AIInference = &policy
	return nil
}

func (r Resolver) ResolveStaged(purpose, name string, override platform.ModelOptions, stage string) (platform.AIModelBinding, platform.AIProvider, *platform.Error) {
	binding, provider, refusal := r.Resolve(purpose, name, override)
	if refusal == nil {
		binding, refusal = r.Stage(binding, provider, stage)
	}
	return binding, provider, refusal
}

func EvaluationModelConfig(name string, definition platform.AIFunction, lookup func(string) (platform.AIModel, platform.AIProvider, *platform.Error),
	resolver Resolver) (string, error) {
	model, provider, refusal := lookup(name)
	if refusal != nil {
		return "", fmt.Errorf("the plan model is not enabled")
	}
	binding, _, refusal := resolver.Resolve("generation", model.Name(), definition.ModelOptions.Override(platform.ModelOptions{MaxTokens: definition.MaxTokens}))
	if refusal != nil {
		return "", fmt.Errorf("model options: %s", refusal.Message)
	}
	binding, refusal = resolver.Stage(binding, provider, runtimeprotocol.AIStageDesign)
	if refusal != nil {
		return "", fmt.Errorf("model stage: %s", refusal.Message)
	}
	return ConfigurationDigest(model, provider, &binding)
}

func (r Resolver) Describe(purpose, name string, override platform.ModelOptions) (string, string, bool) {
	binding, pv, err := r.Resolve(purpose, name, override)
	if err != nil {
		return "", "", false
	}
	model, _, _ := r.Registry.Model(binding.Model)
	config := ""
	if pinned, refusal := r.Stage(binding, pv, runtimeprotocol.AIStageDesign); refusal == nil {
		config, _ = ConfigurationDigest(model, pv, &pinned)
	}
	return binding.Model, config, true
}
