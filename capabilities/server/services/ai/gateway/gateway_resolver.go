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
	"platformkernel/kernel"
	"platformserver/platform"
)

type Models interface {
	platform.AcceptedInputApp
	Snapshot() (json.RawMessage, error)
	Callable(m platform.Member, name string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	Model(name string) (platform.AIModel, platform.AIProvider, *kernel.Error)
	Provider(id string) (platform.AIProvider, bool)
	Meter(u platform.AIUsage)
	Spent(member string, now time.Time) int
	Reservation(string) (platform.AIReservation, bool)
	Usage() []platform.AIUsage
	Quota(platform.Member, platform.AIDefaults, time.Time) platform.AIQuota
}

func Limits(setting func(app, name string) string) platform.AIDefaults {
	number := func(app, name string) int {
		n, _ := strconv.Atoi(setting(app, name))
		return n
	}
	return platform.AIDefaults{People: number(platform.AIApp, platform.AISettingDailyTokens), Agents: number(platform.AgentApp, platform.SettingAgentDaily),
		PerMinute: number(platform.AIApp, platform.AISettingPerMinute)}
}

type Registry interface {
	Model(name string) (platform.AIModel, platform.AIProvider, *kernel.Error)
}

type Resolver struct {
	Registry Registry
	Setting  func(app, key string) string
	Policy   platform.AIInferencePolicy
}

func ResolverFor(setting func(string, string) string, policy *platform.AIInferencePolicy, registry Registry) Resolver {
	r := Resolver{Setting: setting, Registry: registry}
	if policy != nil {
		r.Policy = *policy
	}
	return r
}

func (r Resolver) MostTokens() int {
	n, _ := strconv.Atoi(r.Setting(platform.AIApp, platform.AISettingMaxTokens))
	return n
}

func (r Resolver) Resolve(purpose, name string, override platform.ModelOptions) (platform.AIModelBinding, platform.AIProvider, *kernel.Error) {
	app, key := PurposeSetting(purpose)
	if name == "" {
		name = r.Setting(app, key)
	}
	if r.Registry == nil || name == "" {
		return platform.AIModelBinding{}, platform.AIProvider{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Choose a default model in AI configuration")
	}
	if purpose == "agent" && name == "@generation" {
		name = r.Setting(platform.AIApp, platform.AISettingAppModel)
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
	b, problem := model.Resolve(purpose, defaults.Override(override), r.MostTokens())
	b.Provider = &pv
	if problem != nil {
		return b, pv, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
	}
	return b, pv, nil
}

func (r Resolver) CheckSetting(id, value string) *kernel.Error {
	if id == platform.AIApp+"/"+platform.AISettingMaxTokens || id == platform.AIApp+"/"+platform.AISettingDailyTokens || id == platform.AIApp+"/"+platform.AISettingPerMinute || id == platform.AgentApp+"/"+platform.SettingAgentDaily {
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
				name = r.Setting(platform.AIApp, platform.AISettingAppModel)
			}
			model, _, err := r.Registry.Model(name)
			if err != nil {
				return err
			}
			if _, problem := model.Resolve(purpose, options, r.MostTokens()); problem != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
			}
			return nil
		}
		_, _, err := r.Resolve(purpose, name, options)
		return err
	}
	return nil
}

func (r Resolver) Stage(binding platform.AIModelBinding, provider platform.AIProvider, stage string) (platform.AIModelBinding, *kernel.Error) {
	pinned, err := Bind(r.Policy, provider, binding, stage)
	if err != nil {
		return pinned, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
	}
	return pinned, nil
}

func BindPolicy(services *platform.RuntimeCapabilities) error {
	if services.AIInference == nil {
		policy := platform.AIInferencePolicy{}
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
	policy.Runtime = append([]platform.AIInferenceSite{}, policy.Runtime...)
	services.AIInference = &policy
	return nil
}

func (r Resolver) ResolveStaged(purpose, name string, override platform.ModelOptions, stage string) (platform.AIModelBinding, platform.AIProvider, *kernel.Error) {
	binding, provider, refusal := r.Resolve(purpose, name, override)
	if refusal == nil {
		binding, refusal = r.Stage(binding, provider, stage)
	}
	return binding, provider, refusal
}

func (r Resolver) Describe(purpose, name string, override platform.ModelOptions) (string, string, bool) {
	binding, pv, err := r.Resolve(purpose, name, override)
	if err != nil {
		return "", "", false
	}
	model, _, _ := r.Registry.Model(binding.Model)
	config := ""
	if pinned, refusal := r.Stage(binding, pv, platform.AIStageDesign); refusal == nil {
		config, _ = ConfigurationDigest(model, pv, &pinned)
	}
	return binding.Model, config, true
}
