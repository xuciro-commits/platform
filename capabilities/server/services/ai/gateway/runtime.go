package aigateway

import (
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/ai/gateway/providers"
)

type Runtime struct {
	Tenant          string
	Models          Models
	Inference       platform.AIInferencePolicy
	Settings        func(string, string) string
	Resolver        Resolver
	Suspended       func(string) bool
	DesignAllowed   func(platform.Member) *kernel.Error
	ModelAllowed    func(platform.Member, string) *kernel.Error
	FunctionAllowed func(string, ModelAsk, time.Time) bool
	Available       func(string, time.Time, bool) bool
	Report          func(string, bool, time.Time)
	Provider        func(platform.AIProvider, func()) providers.Client
	Transcribe      func(time.Time, string, string, string, []byte, []byte, string)
	Reserve         func(platform.Member, platform.AIModel, platform.ChatRequest, int, int, time.Time) *platform.AIError
	Release         func(string, time.Time) *kernel.Error
	Meter           func(platform.Member, platform.AIUsage) *kernel.Error
	Call            func(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	Queued          func(func(platform.Effect) bool) int
	ProviderOf      func(string) platform.AIProvider
	Simulation      bool
	App             func(string) platform.App
	Authority       func(string) string
	Read            func(func(Runtime) *kernel.Error) *kernel.Error
}

func (r Runtime) Limits() platform.AIDefaults {
	return Limits(func(app, name string) string { return r.Settings(app, name) })
}

func (r Runtime) Chat(m platform.Member, req platform.ChatRequest, stage string, now time.Time, delta ...func(string)) (platform.ChatAnswer, *kernel.Error, *platform.AIError) {
	return ChatRun{Available: r.Models != nil, Call: r.Call, Meter: r.Meter,
		Setup: func(req platform.ChatRequest) (model platform.AIModel, pv platform.AIProvider, prepared platform.ChatRequest, refusal *kernel.Error) {
			read := r.Read(func(current Runtime) *kernel.Error {
				model, pv, prepared, refusal = ChatSetup{
					Design: func() *kernel.Error {
						if stage != platform.AIStageDesign {
							return nil
						}
						return current.DesignAllowed(m)
					},
					Suspended: current.Suspended,
					Callable:  current.Models.Callable,
					Authorize: func(name string) *kernel.Error {
						return current.ModelAllowed(m, name)
					},
					Resolver: current.Resolver,
				}.Prepare(m, req, stage)
				return refusal
			})
			if refusal == nil {
				refusal = read
			}
			return
		}}.Run(m, req, now, delta...)
}

func (r Runtime) Calls(mostTokens func() int) Calls {
	return Calls{Inference: r.Inference, MostTokens: mostTokens, Reserve: r.Reserve, Release: r.Release, Meter: r.Meter,
		Available: r.Available, Report: r.Report, Provider: r.Provider, Transcribe: r.Transcribe}
}

func (r Runtime) Embed(member platform.Member, name string, model platform.AIModel, pv platform.AIProvider, input []string, now time.Time) ([][]float32, error) {
	return r.Calls(nil).Embed(member, name, model, pv, input, now)
}

func (r Runtime) Plan(c platform.Caller, rec *pb.ChangeRecord, q platform.Request, pending []platform.Effect) platform.Effect {
	return ModelPlanning{
		Resolver:   r.Resolver,
		Provider:   r.ProviderOf,
		Default:    func() string { return r.Settings(platform.AIApp, platform.AISettingAppModel) },
		Simulation: r.Simulation,
		Queued:     r.Queued,
	}.Effect(r.Tenant, c, rec, q, pending, acceptedformat.ModelEndpoint)
}

func (r Runtime) Send(x platform.Effect, now time.Time) (platform.Outcome, *platform.AIUsage) {
	return ModelDispatch{Call: r.Call,
		Gate: func(ask ModelAsk, m platform.Member, out platform.Outcome) (platform.Outcome, platform.AIModel, platform.AIProvider, bool, *kernel.Error) {
			var gated platform.Outcome
			var enabled platform.AIModel
			var pv platform.AIProvider
			var ready bool
			refusal := r.Read(func(current Runtime) *kernel.Error {
				gated, enabled, pv, ready = ask.Ready(out, ModelSend{
					Allowed:   func() bool { return current.FunctionAllowed(x.App, ask, now) },
					Available: current.Models != nil,
					Model:     current.Models.Model,
					Authorize: func(name string) *kernel.Error {
						return current.ModelAllowed(m, name)
					},
					Resolver: current.Resolver,
				})
				return nil
			})
			return gated, enabled, pv, ready, refusal
		}}.Send(r.Tenant, x, now)
}

func (r Runtime) Answer(x platform.Effect, o platform.Outcome, usage *platform.AIUsage) (platform.App, *pb.Submission) {
	return AnswerSubmission(r.Tenant, x, o, usage, r.App, r.Authority)
}

type AgentCall struct {
	Authorize func(platform.Member, platform.ChatRequest) *platform.AIError
	Call      func(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	Answer    func(platform.ChatRequest, platform.ChatAnswer) platform.ChatAnswer
}

func (c AgentCall) Run(provider platform.AIProvider, model platform.AIModel, member platform.Member, request platform.ChatRequest, now time.Time) (platform.ChatAnswer, *platform.AIError) {
	if failure := c.Authorize(member, request); failure != nil {
		return platform.ChatAnswer{}, failure
	}
	answer, failure := c.Call(provider, model, member, request, now)
	return c.Answer(request, answer), failure
}
