package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"platformserver/aigateway"
	"platformserver/platform"
	"platformserver/platform/authz"
	"strings"
)

// RuntimeCapabilities binds trusted pure implementations for one tenant instance.
// Rebuilding an instance carries this selection; callers never mutate it live.
type RuntimeCapabilities struct {
	NewTransactionExecutor func(platform.BusinessRuntimeContext) platform.TransactionExecutor
	NewAgentEvaluator      func() platform.AgentEvaluator
	BindOwner              func(platform.App, platform.OwnerBindings)
	AIInference            *platform.AIInferencePolicy
	Rules                  platform.RuleEngine
	Authorization          authz.Engine
	Kernel                 platform.KernelFactory
	RuleModule             platform.CapabilityModule
	AuthModule             platform.CapabilityModule
	KernelModule           platform.CapabilityModule
	StorageModule          platform.CapabilityModule
}

func (r runtime) AuthorizationEngine() authz.Engine { return r.t.services.Authorization }
func (d *stagedDecision) AuthorizationEngine() authz.Engine {
	return d.tenant.services.Authorization
}

func DefaultRuntimeCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{
		Rules: platform.NativeRules{}, Authorization: authz.Standard{}, Kernel: platform.NativeKernel{}, StorageModule: MemoryStorageModule(),
		KernelModule: platform.CapabilityModule{ID: "platform.kernel.native", Version: "1", Title: "Kernel", Scope: "tenant", Replacement: "restart", StateFormat: "platform.kernel.v1", Provides: []platform.CapabilityContract{{ID: "platform.kernel", Version: "1", Semantics: "1"}}},
		RuleModule:   platform.CapabilityModule{ID: "platform.rules.native", Version: "1", Title: "Business rules", Scope: "tenant", Replacement: "restart", Provides: []platform.CapabilityContract{{ID: "platform.rules", Version: "1", Semantics: fmt.Sprint(platform.ExprVersion)}}},
		AuthModule:   platform.CapabilityModule{ID: "platform.authorization.standard", Version: "1", Title: "Authorization", Scope: "tenant", Replacement: "restart", Provides: []platform.CapabilityContract{{ID: "platform.authorization", Version: "1", Semantics: "1"}}},
	}
}

func (c RuntimeCapabilities) Check() error {
	if c.Rules == nil || c.Authorization == nil || c.Kernel == nil {
		return fmt.Errorf("runtime needs explicit rule and authorization providers")
	}
	plan, err := platform.ResolveComposition([]platform.CapabilityModule{c.RuleModule, c.AuthModule, c.KernelModule}, nil)
	if err != nil {
		return err
	}
	for _, expected := range []platform.CapabilityContract{{ID: "platform.rules", Version: "1", Semantics: fmt.Sprint(platform.ExprVersion)}, {ID: "platform.authorization", Version: "1", Semantics: "1"}, {ID: "platform.kernel", Version: "1", Semantics: "1"}} {
		found := false
		for _, binding := range plan.Bindings {
			found = found || binding.CapabilityContract == expected
		}
		if !found {
			return fmt.Errorf("runtime provider does not implement %s", expected.ID)
		}
	}
	return nil
}

func (r runtime) Rules() platform.RuleEngine             { return r.t.services.Rules }
func (d *stagedDecision) Rules() platform.RuleEngine     { return d.tenant.services.Rules }
func (r runtime) Kernel() platform.KernelFactory         { return r.t.services.Kernel }
func (d *stagedDecision) Kernel() platform.KernelFactory { return d.tenant.services.Kernel }

func (t *Tenant) RuntimeCapabilities() RuntimeCapabilities { return t.services }

// Inference approvals are static deployment input, never tenant model metadata.
func bindAIInferencePolicy(services *RuntimeCapabilities) error {
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
	if err := aigateway.CheckPolicy(*services.AIInference); err != nil {
		return err
	}
	// Own a private copy: callers cannot mutate an instance's deployment approval.
	policy := *services.AIInference
	policy.Runtime = append([]platform.AIInferenceSite{}, policy.Runtime...)
	services.AIInference = &policy
	return nil
}
