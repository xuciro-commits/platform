package platformserver

import (
	"encoding/json"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

func (h hostView) ProtocolActions(protocol string) []platform.Action {
	return h.t.protocolActions(protocol)
}
func (h hostView) ProtocolProvider(schema string) (platform.App, string, bool) {
	return h.t.provider(schema)
}
func (h hostView) InvokeDecision(c platform.Caller, protocol, action, id string, raw []byte, key, correlation string, now time.Time) (*pb.EntityRef, *pb.ChangeRecord, *kernel.Error) {
	return h.t.invoke(c, protocol, action, id, raw, key, correlation, now)
}
func (h hostView) AgentContext(reader *platform.Member, typ, id string, now time.Time) (host.AgentObservation, *kernel.Error) {
	view, err := h.t.Context(reader, typ, id, now)
	if err != nil {
		return host.AgentObservation{}, err
	}
	raw, _ := json.Marshal(view)
	return host.AgentObservation{Body: raw, Sources: h.t.refsIn(view)}, nil
}
func (h hostView) AgentSearch(reader *platform.Member, q string, now time.Time) host.AgentObservation {
	found := h.t.Search(reader, q, now)
	out := host.AgentObservation{}
	for _, hit := range found {
		out.Sources = append(out.Sources, hit.Type+"/"+hit.ID)
	}
	out.Body, _ = json.Marshal(found)
	return out
}
func (h hostView) AgentQuery(reader platform.Member, app, name, of string, now time.Time) (host.AgentObservation, *kernel.Error) {
	page, err := h.t.RunQuery(reader, app, name, of, now)
	if err != nil {
		return host.AgentObservation{}, err
	}
	query, _ := h.t.namedQuery(app, name)
	raw, _ := json.Marshal(page.Records)
	var ids []struct{ ID string }
	_ = json.Unmarshal(raw, &ids)
	out := host.AgentObservation{}
	for _, row := range ids {
		out.Sources = append(out.Sources, query.Object+"/"+row.ID)
	}
	out.Body, _ = json.Marshal(page)
	return out, nil
}
func (h hostView) AgentRead(reader platform.Member, name string) (host.AgentObservation, *kernel.Error) {
	value, err := h.t.Read(reader, name)
	if err != nil {
		return host.AgentObservation{}, err
	}
	raw, _ := json.Marshal(value)
	return host.AgentObservation{Body: raw, Sources: h.t.refsIn(value)}, nil
}
func (h hostView) AgentSources(value any) []string { return h.t.refsIn(value) }
func (h hostView) AgentKnowledge(reader *platform.Member, app, q string, limit int, now time.Time) json.RawMessage {
	found := h.t.Knowledge(reader, app, q, limit, now)
	for i := range found {
		found[i].Text = clip(found[i].Text, 700)
	}
	raw, _ := json.Marshal(found)
	return raw
}
func (h hostView) AgentAppReader(app string) *platform.Member { return h.t.appReader(app) }
func (h hostView) AgentMeaning(app string) string             { return h.t.meaning(app) }
func (h hostView) AgentGlossary(app string) string            { return h.t.glossary(app) }
func (h hostView) AgentLanguage(language string) string       { return languageNames[language] }
func (h hostView) AgentSuspended(member string) bool          { return h.t.suspended(member) }
func (h hostView) ExternalAgentMembers() []platform.Member {
	owner, ok := h.t.app(PlatformApp).(host.IdentityDirectory)
	if !ok {
		return nil
	}
	return owner.AgentMembers()
}
func (h hostView) AgentDecisionMembers() []string {
	var out []string
	for _, entry := range h.t.Audit() {
		out = append(out, entry.Member)
	}
	return out
}
func (h hostView) AgentUsage() []platform.AIUsage {
	if h.t.ai == nil {
		return nil
	}
	return h.t.ai.Usage()
}
func (h hostView) BeginAgentRead() func() { h.t.mu.Lock(); return h.t.mu.Unlock }
func (h hostView) ScopeAgentRun(run string) func() {
	previous := h.t.agentRun
	h.t.agentRun = run
	return func() { h.t.agentRun = previous }
}
func (h hostView) AgentModel(name string) (platform.AIModel, platform.AIProvider, *kernel.Error) {
	if h.t.ai == nil {
		return platform.AIModel{}, platform.AIProvider{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "AI capability unavailable")
	}
	return h.t.ai.Model(name)
}
func (h hostView) ReserveAgentModel(member platform.Member, model platform.AIModel, now time.Time) string {
	return h.t.reserve(member, model, now)
}
func (h hostView) AgentProviderAvailable(provider string, now time.Time) bool {
	return h.t.breakers.allow("ai:"+provider, now)
}
func (h hostView) CallAgentModel(provider platform.AIProvider, model platform.AIModel, member platform.Member, request platform.ChatRequest, now time.Time) (platform.ChatAnswer, *platform.AIError) {
	return h.t.call(provider, model, member, request, now)
}
func (h hostView) MeterAgentUsage(member platform.Member, usage platform.AIUsage) *kernel.Error {
	return h.t.meter(member, usage)
}
func (h hostView) ReplayAgentUsage(usage platform.AIUsage) {
	if h.t.ai != nil && usage.Model != "" {
		h.t.ai.Meter(usage)
	}
}

var _ host.AgentHost = hostView{}

func (h hostView) AgentTasks() host.TaskClosure         { return h.Tasks() }
func (h hostView) AgentFlows() host.AgentFlowCompletion { return h.Processes() }
