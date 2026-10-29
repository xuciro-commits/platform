package platformserver

import automation "platformserver/engine/automation"

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	appowner "platformserver/engine/applications"
	"platformserver/internal/systemscope"
	"platformserver/journal"
	"platformserver/kernel/acceptedformat"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"platformserver/studio/build"
	releaseowner "platformserver/studio/releases"
	"slices"
	"strings"
	"time"
)

// An agent answer includes the tool decision, resulting run/evaluation record
// and measured spend in one accepted result; recovery never invokes the tool.
type acceptedAgent struct {
	Version     int                      `json:"version"`
	Kind        string                   `json:"kind"`
	Tenant      string                   `json:"tenant"`
	App         string                   `json:"app"`
	Member      string                   `json:"member"`
	Key         string                   `json:"key"`
	At          time.Time                `json:"at"`
	RequestHash string                   `json:"requestHash"`
	Digest      string                   `json:"digest"`
	Step        platform.AgentResultStep `json:"step"`
	UsageBefore string                   `json:"usageBefore,omitempty"`
	Changes     json.RawMessage          `json:"changes"`
}

func digestAgent(r acceptedAgent) (string, error) { r.Digest = ""; return platform.CanonicalDigest(r) }
func agentHash(r acceptedAgent) (string, error) {
	return platform.CanonicalDigest([]any{r.Tenant, r.Member, r.Step, r.UsageBefore})
}
func decodeAcceptedAgent(raw []byte) (acceptedAgent, error) {
	var r acceptedAgent
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return r, fmt.Errorf("agent result is empty or too large")
	}
	if err := platform.DecodeJSON(raw, &r, "agent result"); err != nil {
		return r, err
	}
	hash, err := agentHash(r)
	digest, digestErr := digestAgent(r)
	batch, receipt, batchErr := acceptedformat.DecodeBatch(r.Changes)
	if err != nil || digestErr != nil || batchErr != nil || r.Version != 1 || r.Kind != "agent-result" || r.Tenant == "" || r.App != platform.AgentApp || r.Member == "" || r.At.IsZero() || r.Step.Run == "" || r.RequestHash != hash || r.Digest != digest ||
		batch.Tenant != r.Tenant || batch.App != platform.AgentApp || !batch.At.Equal(r.At) || receipt.GetSubmission().GetPrincipalId() != r.Member || r.Key != "agent:"+receipt.GetSubmission().GetIdempotencyKey() {
		return r, fmt.Errorf("agent result has inconsistent identity or changes")
	}
	if r.Step.Evaluation == nil && r.Step.Usage.Model != "" {
		if !acceptedformat.ValidUsage(r.Step.Usage) || r.UsageBefore == "" || r.Step.Usage.Member != r.Member {
			return r, fmt.Errorf("agent result has invalid usage")
		}
	} else if r.UsageBefore != "" {
		return r, fmt.Errorf("agent result has unexpected usage")
	}
	return r, nil
}

func (t *Tenant) acceptAgentStep(b platform.AgentResultStep, at time.Time) {
	defer func() {
		if p := recover(); p != nil {
			t.quarantine(fmt.Errorf("agent answer cannot stage: %v", p))
		}
	}()
	draft := t.newStagedDecision()
	draft.at = at
	agent := draft.stateApp(t.agents).(platform.AgentRuntime)
	// Authorization and source fences have been rechecked by agentStep while
	// holding the tenant lock, after the outside model call has completed.
	run, _ := platform.Get[platform.AgentRunRecord](t.automation(platform.AgentApp, false), b.Run)
	if b.Evaluation != nil {
		run.Agent = b.Evaluation.Agent
	}
	member := agent.AgentPrincipal(run.Agent)
	if b.Evaluation != nil && b.Evaluation.Candidate != "" {
		// A candidate's agent need not be installed as a principal yet. Only
		// its original owner records the dry report; it executes no AI tool.
		member = platform.Member{ID: "app:" + platform.AgentApp, Tenant: t.ID}
	}
	c := platform.NewCaller(draft, member, platform.AgentApp, false, true)
	r := acceptedAgent{Version: 1, Kind: "agent-result", Tenant: t.ID, App: platform.AgentApp, Member: member.ID, At: at.UTC(), Step: b}
	var err error
	if b.Evaluation == nil && b.Usage.Model != "" {
		r.UsageBefore, err = t.usagePredecessor()
		if err != nil {
			t.quarantine(err)
			return
		}
	}
	refusal := agent.ApplyAgentStepCaller(c, b, at)
	if refusal == nil {
		refusal = draft.failure
	}
	if refusal == nil {
		refusal = draft.answerRequests(at)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	if refusal != nil || len(draft.events) == 0 {
		t.quarantine(fmt.Errorf("agent answer has no complete decision: %v", refusal))
		return
	}
	last := draft.events[len(draft.events)-1]
	r.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, at)
	if err != nil {
		t.quarantine(err)
		return
	}
	r.Key = "agent:" + last.Record.GetSubmission().GetIdempotencyKey()
	r.RequestHash, err = agentHash(r)
	if err == nil {
		r.Digest, err = digestAgent(r)
	}
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(r)
	}
	if err == nil {
		_, err = decodeAcceptedAgent(raw)
	}
	if err != nil {
		t.quarantine(err)
		return
	}
	principal, _ := json.Marshal(member)
	committed, err := t.acceptResult(Entry{App: platform.AgentApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: draft.flowVersions()}, r.Key, r.RequestHash)
	if err != nil {
		return
	}
	saved, err := decodeAcceptedAgent(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed agent answer differs: %v", err))
		return
	}
	if _, err = t.applyAcceptedAgent(committed); err != nil {
		t.quarantine(err)
		return
	}
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	t.changedOwner(platform.AgentApp)
}

func (t *Tenant) applyAcceptedAgent(raw []byte) (acceptedAgent, error) {
	r, err := decodeAcceptedAgent(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != t.ID || t.agents == nil {
		return r, fmt.Errorf("agent result belongs to another tenant")
	}
	if r.UsageBefore != "" {
		before, err := t.usagePredecessor()
		if err != nil || before != r.UsageBefore {
			return r, fmt.Errorf("agent usage predecessor differs: %v", err)
		}
	}
	batch, _, _ := acceptedformat.DecodeBatch(r.Changes)
	if applied, err := t.applyAcceptedBatch(t.agents.AcceptedLedger(), r.Changes); err != nil || !applied {
		return r, fmt.Errorf("agent result application: %v (applied=%t)", err, applied)
	}
	if r.UsageBefore != "" {
		t.ai.Meter(r.Step.Usage)
	}
	t.publishAcceptedBatch(batch)
	return r, nil
}

// applyAcceptedEffect checks every predecessor before promoting an effect
// outcome. Neither the application callback nor the external send is run here.
func (t *Tenant) applyAcceptedEffect(raw []byte) (acceptedformat.Effect, bool, error) {
	result, err := acceptedformat.DecodeEffect(raw)
	if err != nil {
		return result, false, err
	}
	if result.Tenant != t.ID {
		return result, false, fmt.Errorf("effect result belongs to another tenant")
	}
	t.opsMu.Lock()
	var current *effect
	for _, x := range t.outbound {
		if x.ID == result.Before.ID {
			current = x
			break
		}
	}
	var before acceptedformat.EffectState
	if current != nil {
		before = acceptedformat.EffectState{Effect: current.Effect, Since: current.since}
	}
	t.opsMu.Unlock()
	beforeHash, err := platform.CanonicalDigest(before)
	savedHash, savedErr := platform.CanonicalDigest(result.Before)
	if current == nil || err != nil || savedErr != nil || beforeHash != savedHash {
		return result, false, fmt.Errorf("effect result predecessor differs")
	}
	works, priorWork, savedWork, err := t.prepareOperationFinish(result.Before.Effect, result.Outcome)
	if err != nil {
		return result, false, err
	}
	if works != nil {
		priorHash, _ := platform.CanonicalDigest(priorWork)
		expectedPrior, _ := platform.CanonicalDigest(result.WorkBefore)
		workHash, _ := platform.CanonicalDigest(savedWork)
		expectedWork, _ := platform.CanonicalDigest(result.Work)
		if priorHash != expectedPrior || workHash != expectedWork {
			return result, false, fmt.Errorf("operation result ownership differs")
		}
	}
	if result.Usage != nil {
		if t.ai == nil {
			return result, false, fmt.Errorf("effect result needs the AI meter")
		}
		state, err := t.ai.Snapshot()
		currentHash, hashErr := platform.CanonicalDigest(state)
		if err != nil || hashErr != nil || currentHash != result.UsageBefore {
			return result, false, fmt.Errorf("effect result model usage predecessor differs")
		}
	}
	prepared, err := t.prepareAcceptedChanges(result.ChangePlan())
	if err != nil {
		return result, false, err
	}
	if err := prepared.apply(t); err != nil {
		return result, false, err
	}
	t.opsMu.Lock()
	current.Effect, current.since, current.sending = result.After.Effect, result.After.Since, false
	t.opsMu.Unlock()
	if works != nil {
		// The child may have cancelled other computation generations. Promote
		// only this work's frozen completion, never overwrite that child state.
		finished, _ := works.Get(result.Before.ID)
		t.installAcceptedWork(finished)
	}
	if result.Usage != nil {
		t.ai.Meter(*result.Usage)
	}
	prepared.publish(t)
	return result, true, nil
}

// settleAccepted stages the observed outcome and its callbacks before one
// append. A failed append releases the transient in-flight flag so the same
// outbound ID can be retried; neither the outcome nor callback becomes visible.
func (t *Tenant) settleAccepted(id string, out platform.Outcome, usage *platform.AIUsage, at time.Time) {
	t.opsMu.Lock()
	var pending *effect
	for _, x := range t.outbound {
		if x.ID == id {
			pending = x
			break
		}
	}
	var before acceptedformat.EffectState
	if pending != nil {
		before = acceptedformat.EffectState{Effect: pending.Effect, Since: pending.since}
	}
	t.opsMu.Unlock()
	if pending == nil || out.Effect != id {
		t.quarantine(fmt.Errorf("external answer for an unknown or different effect %s", id))
		return
	}
	if settled(before.State) && before.State != "discarded" {
		return // A late transport answer cannot replace an accepted human/source resolution.
	}
	if usage != nil {
		copy := *usage
		copy.Applications = slices.Clone(before.Effect.Applications)
		copy.Run = before.Effect.ID
		if before.Effect.Endpoint == acceptedformat.ModelEndpoint {
			var ask modelAsk
			if json.Unmarshal([]byte(before.Effect.Body), &ask) == nil && ask.Function != nil {
				owner := ask.Function.Owner
				if owner == "" {
					owner = before.Effect.App
				}
				ref := platform.AssetRef{App: owner, Kind: platform.AssetFunction, Name: ask.Function.Definition.Name}
				copy.Resource = &ref
			}
		}
		usage = &copy
	}
	if before.Endpoint == acceptedformat.ModelEndpoint && (out.Result == "rejected" || out.Result == "unknown") && (t.tasks == nil || len((hostView{t: t}).Holding(controlowner.PlatformApp, controlowner.Admin)) == 0) {
		out.PublicMessage = "The model call stopped; no eligible reviewer is configured"
	}
	// New attempts always retain a safe public message when they retain an
	// error. An empty message on an old effect remains an explicit legacy case.
	if out.Detail != "" && out.PublicMessage == "" {
		out.PublicMessage = "The last delivery attempt failed"
	}
	after := &effect{Effect: before.Effect, since: before.Since}
	markEffect(after, out, at)
	result := acceptedformat.Effect{Version: 5, Kind: "effect-result", Tenant: t.ID, App: controlowner.PlatformApp,
		Key: acceptedformat.EffectResultKey(before), At: at.UTC(), Before: before,
		After: acceptedformat.EffectState{Effect: after.Effect, Since: after.since}, Outcome: out, Usage: usage}
	var err error
	_, result.WorkBefore, result.Work, err = t.prepareOperationFinish(before.Effect, out)
	if err != nil {
		return
	}
	if usage != nil {
		if t.ai == nil {
			t.quarantine(fmt.Errorf("effect %s has usage without an AI meter", id))
			return
		}
		state, snapshotErr := t.ai.Snapshot()
		if snapshotErr == nil {
			result.UsageBefore, snapshotErr = platform.CanonicalDigest(state)
		}
		if snapshotErr != nil {
			t.quarantine(fmt.Errorf("effect %s usage checkpoint: %w", id, snapshotErr))
			return
		}
	}
	result.RequestHash, err = acceptedformat.EffectResultHash(before, out, result.At)
	if err != nil {
		t.quarantine(err)
		return
	}
	draft := t.newStagedDecision()
	draft.at = at
	if after.State == "failed" && t.app(controlowner.PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + controlowner.PlatformApp, Tenant: t.ID}, controlowner.PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Effect to " + after.Endpoint + " failed: " + after.Event,
			Body: after.Error + ". Retry it in Settings → Automation once the cause is fixed.",
			Key:  "failed:" + after.ID}, at, platform.Recipient{AppRole: controlowner.Admin})
	}
	if (settled(after.State) || after.State == "unknown") && before.State != "discarded" {
		if err := t.stageEffectAnswer(draft, after.Effect, out, usage, at); err != nil {
			if t.quarantined() {
				return
			}
			// A business refusal is itself a durable callback outcome. Drop its
			// private writes; an unsupported staging operation is not a
			// business refusal and instead isolates this tenant.
			result.CallbackError = err.Error()
			result.Authorizations = slices.Clone(draft.authorizations)
			draft = t.newStagedDecision()
			draft.at = at
		}
	}
	if before.Endpoint == acceptedformat.ModelEndpoint && (after.State == "rejected" || after.State == "failed" || after.State == "unknown") {
		// The original effect and receipt own one handoff. Model failures cannot
		// manufacture a second workflow or borrow an AI administrator's data grant.
		recipients := (hostView{t: t, app: t.app(before.App)}).Holding(controlowner.PlatformApp, controlowner.Admin)
		if len(recipients) > 0 && t.tasks != nil {
			change, _, _ := strings.Cut(before.Key, "#")
			var receipt *pb.ChangeRecord
			for _, saved := range t.app(before.App).AcceptedLedger().RecordsFor(t.ID) {
				if saved.GetChangeId() == change {
					receipt = saved
					break
				}
			}
			if receipt != nil {
				caller := platform.NewCaller(draft, platform.Member{ID: "app:" + before.App, Tenant: t.ID}, before.App, false, true)
				to := make([]platform.Recipient, 0, len(recipients))
				for _, member := range recipients {
					to = append(to, platform.Recipient{Member: member})
				}
				if failure := caller.Assign(receipt, platform.Assignment{Title: "Review the stopped model call", Body: out.Detail, Ref: "platform.effect/" + before.ID, Key: "ai-handoff:" + before.ID, To: to}); failure != nil {
					draft.failure = failure
				}
			}
		}
	}
	result.Authorizations = append(result.Authorizations, draft.authorizations...)
	result.After.CallbackError = result.CallbackError
	if draft.failure != nil {
		t.quarantine(fmt.Errorf("stage effect %s: %v", id, draft.failure))
		return
	}
	if len(draft.events) == 0 && (len(draft.deliveries) > 0 || len(draft.observations) > 0 || len(draft.allocated) > 0 ||
		len(draft.logs) > 0 || len(draft.requests) > 0) {
		err = fmt.Errorf("effect answer has unrepresented changes")
	} else {
		eventRows := map[string]bool{}
		for _, event := range draft.events {
			for _, ref := range event.Changed {
				eventRows[ref] = true
			}
		}
		result.Rows, err = captureDirectRows(draft, before.App, nil, eventRows, "effect answer")
		if len(result.Rows) > 0 {
			result.Member = "app:" + before.App
		}
		if err == nil && len(draft.events) > 0 {
			// A direct observation and a child decision may write disjoint
			// records in one callback. The child batch owns only its events;
			// direct rows retain their own preimages in the outer result.
			writes := draft.records.writes
			draft.records.writes = eventRows
			last := draft.events[len(draft.events)-1]
			result.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, at)
			draft.records.writes = writes
		} else if err == nil {
			var plan acceptedformat.ChangePlan
			plan, err = draft.savedChangePlan()
			result.Notices, result.Intents = plan.Notices, plan.Intents
			result.States, result.Control = plan.States, plan.Control
		}
	}
	if err != nil {
		t.quarantine(fmt.Errorf("stage effect %s: %w", id, err))
		return
	}
	result.Unique = draft.savedUniqueChanges()
	if _, err := t.prepareAcceptedChanges(result.ChangePlan()); err != nil {
		t.quarantine(fmt.Errorf("stage effect %s changes: %w", id, err))
		return
	}
	result.Digest, err = acceptedformat.DigestEffect(result)
	if err != nil {
		t.quarantine(err)
		return
	}
	raw, err := json.Marshal(result)
	if err == nil {
		_, err = acceptedformat.DecodeEffect(raw)
	}
	if err != nil {
		t.quarantine(fmt.Errorf("encode effect %s: %w", id, err))
		return
	}
	principal, _ := json.Marshal(t.automation(controlowner.PlatformApp, false).Member)
	committed, err := t.acceptResult(Entry{App: controlowner.PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: draft.flowVersions()}, result.Key, result.RequestHash)
	if err != nil {
		if errors.Is(err, errAcceptedConflict) {
			t.quarantine(fmt.Errorf("effect %s conflicts with a previously committed outcome", id))
			return
		}
		t.opsMu.Lock()
		pending.sending = false
		t.opsMu.Unlock()
		return
	}
	saved, applied, err := t.applyAcceptedEffect(committed)
	if err != nil || saved.Key != result.Key || saved.RequestHash != result.RequestHash || !applied {
		t.quarantine(fmt.Errorf("apply committed effect %s: %v (applied=%t)", id, err, applied))
		return
	}
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	if len(saved.Changes) == 0 && len(saved.Rows) == 0 && len(saved.States) == 0 && saved.Notices == nil {
		t.operationsChanged(saved.App)
	} else {
		t.changedOwner(saved.App)
	}
}

func (t *Tenant) stageEffectAnswer(draft *stagedDecision, x platform.Effect, out platform.Outcome, usage *platform.AIUsage, at time.Time) (err error) {
	defer func() {
		if p := recover(); p != nil {
			// An unsupported staged effect must never masquerade as a durable
			// business refusal and silently discard a received answer.
			t.quarantine(fmt.Errorf("effect callback cannot stage: %v", p))
			err = fmt.Errorf("unsupported effect callback")
		}
	}()
	if x.App != "" {
		c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
		if refusal := draft.authorizeNative(c, "effect:"+x.Endpoint, "action", nil, t.app(x.App) != nil); refusal != nil {
			return refusal
		}
	}
	if x.Endpoint == acceptedformat.ModelEndpoint {
		app, reply := t.modelAnswerSubmission(x, out, usage)
		if app == nil || reply == nil {
			return nil
		}
		c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
		_, refusal := draft.Decide(c, app, reply, at)
		if refusal != nil {
			return refusal
		}
		return nil
	}
	if x.App == "" {
		return nil
	}
	if x.Endpoint == acceptedformat.OperationEndpoint {
		draft.operationAnswers = map[string]platform.OperationResult{x.ID: operationResultOf(x)}
		processesOwner := t.procs
		if app, ok := t.procs.(platform.App); ok {
			processesOwner = draft.stateApp(app).(platform.Processes)
		}
		if processes, ok := processesOwner.(platform.OperationCompletion); ok {
			c := platform.NewCaller(draft, platform.Member{ID: "app:" + "flow", Tenant: t.ID}, "flow", false, true)
			if err := processes.OperationEnded(c, x.ID, at); err != nil {
				return err
			}
		}
	}
	c := platform.NewCaller(draft, platform.Member{ID: "app:" + x.App, Tenant: t.ID}, x.App, false, true)
	if app, ok := draft.stateApp(t.app(x.App)).(platform.Answerer); ok {
		var source platform.Effect
		t.opsMu.Lock()
		for _, effect := range t.outbound {
			if effect.ID == x.ID {
				source = effect.Effect
				break
			}
		}
		t.opsMu.Unlock()
		if source.ID == "" || source.App != x.App {
			return fmt.Errorf("effect callback has no accepted source")
		}
		previous := draft.callbackOrigin
		refusal := func() *kernel.Error {
			draft.callbackOrigin = &source
			defer func() { draft.callbackOrigin = previous }()
			return app.Answer(c, x, out, at)
		}()
		if refusal != nil {
			return refusal
		}
	}
	if t.agents != nil {
		draft.stateApp(t.agents).(platform.AgentRuntime).AgentEffectEnded(t.automated(c, platform.AgentApp), nil, x, out, at)
	}
	if draft.failure != nil {
		return draft.failure
	}
	if err := draft.answerRequests(at); err != nil {
		return err
	}
	if err := draft.stageObservers(); err != nil {
		return err
	}
	return nil
}

// The host console owns lifecycle, bounded support authority and migration
// manifests. These images contain no ordinary tenant records or execution state.
type hostControlImage struct {
	Lifecycle        string                        `json:"lifecycle"`
	Support          []SupportGrant                `json:"support"`
	Migrations       []MigrationManifest           `json:"migrations"`
	Requests         map[string]hostControlReceipt `json:"requests"`
	RuntimeSelection map[string]string             `json:"runtimeSelection,omitempty"`
	DisabledOwners   []string                      `json:"disabledOwners,omitempty"`
}
type hostControlReceipt struct {
	RequestHash string       `json:"requestHash"`
	Answer      SupportGrant `json:"answer,omitempty"`
}
type acceptedHostControl struct {
	Authorizations []acceptedformat.AuthorizationEvidence `json:"authorizations,omitempty"`
	Version        int                                    `json:"version"`
	Kind           string                                 `json:"kind"`
	Tenant         string                                 `json:"tenant"`
	App            string                                 `json:"app"`
	Member         string                                 `json:"member"`
	Key            string                                 `json:"key"`
	At             time.Time                              `json:"at"`
	Action         string                                 `json:"action"`
	Request        json.RawMessage                        `json:"request"`
	RequestHash    string                                 `json:"requestHash"`
	Digest         string                                 `json:"digest"`
	Before         string                                 `json:"before"`
	AuditBefore    string                                 `json:"auditBefore"`
	Image          hostControlImage                       `json:"image"`
	Audit          platform.AuditEntry                    `json:"audit"`
	Answer         SupportGrant                           `json:"answer,omitempty"`
}

func (h *hostControl) image() (hostControlImage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Encode the complete owned shape to isolate nested migration row diagnostics.
	raw, err := json.Marshal(hostControlImage{Lifecycle: h.state(), Support: h.support, Migrations: h.migrations, Requests: h.requests, RuntimeSelection: h.runtimeSelection, DisabledOwners: h.disabledOwners})
	var image hostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &image)
	}
	return image, err
}
func validateHostImage(tenant string, image hostControlImage) error {
	owners := map[string]bool{}
	for _, id := range image.DisabledOwners {
		if id == "" || len(id) > 128 || owners[id] {
			return fmt.Errorf("invalid inactive owner")
		}
		owners[id] = true
	}
	if image.RuntimeSelection != nil {
		if len(image.RuntimeSelection) != 3 {
			return fmt.Errorf("invalid runtime selection")
		}
		for _, id := range []string{"platform.rules", "platform.authorization", "platform.kernel"} {
			if _, found := image.RuntimeSelection[id]; !found {
				return fmt.Errorf("missing runtime contract %s", id)
			}
		}
	}
	if !slices.Contains([]string{"", "open", "suspend", "decommission", "suspended", "decommissioned"}, image.Lifecycle) {
		return fmt.Errorf("invalid host lifecycle")
	}
	ids := map[string]bool{}
	for _, grant := range image.Support {
		if grant.ID == "" || ids[grant.ID] || grant.Tenant != tenant || grant.Member == "" || grant.Reason == "" || grant.Opened == "" || grant.Expires.IsZero() || grant.Uses < 0 {
			return fmt.Errorf("invalid host support grant")
		}
		ids[grant.ID] = true
	}
	for _, m := range image.Migrations {
		if m.At.IsZero() || m.Key == "" || m.From == "" || m.To != tenant || m.From == m.To || m.Member == "" {
			return fmt.Errorf("invalid host migration manifest")
		}
		for _, part := range m.Types {
			if part.Type == "" || part.Rows < 0 || part.Written < 0 || part.Written > part.Rows {
				return fmt.Errorf("invalid host migration counts")
			}
		}
	}
	for key, receipt := range image.Requests {
		tag, _, found := strings.Cut(strings.TrimPrefix(key, "host:"), ":")
		if !strings.HasPrefix(key, "host:") || !found || !slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration", "composition"}, tag) || len(receipt.RequestHash) != 64 || !strings.HasSuffix(key, ":"+receipt.RequestHash) {
			return fmt.Errorf("invalid host receipt")
		}
		if tag == "support.open" || tag == "support.use" {
			i := slices.IndexFunc(image.Support, func(g SupportGrant) bool { return g.ID == receipt.Answer.ID })
			if i < 0 {
				return fmt.Errorf("host receipt names no support grant")
			}
			current := image.Support[i]
			if receipt.Answer.Uses < 0 || receipt.Answer.Uses > current.Uses {
				return fmt.Errorf("host receipt has an invalid support count")
			}
			current.Uses = receipt.Answer.Uses
			if current != receipt.Answer {
				return fmt.Errorf("host receipt rewrites its support authority")
			}
		} else if receipt.Answer != (SupportGrant{}) {
			return fmt.Errorf("host receipt has an unrelated answer")
		}
	}
	return nil
}
func hostRequestHash(r acceptedHostControl) (string, error) {
	return platform.CanonicalDigest([]any{r.Tenant, r.Member, r.Action, r.Request, r.At})
}
func hostResultDigest(r acceptedHostControl) (string, error) {
	r.Digest = ""
	return platform.CanonicalDigest(r)
}
func decodeAcceptedHostControl(raw []byte) (acceptedHostControl, error) {
	var r acceptedHostControl
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return r, fmt.Errorf("host result is empty or too large")
	}
	if err := platform.DecodeJSON(raw, &r, "host result"); err != nil {
		return r, err
	}
	if err := acceptedformat.ValidateAuthorizationEvidence(r.Tenant, r.Authorizations); err != nil {
		return r, err
	}
	hash, hashErr := hostRequestHash(r)
	digest, digestErr := hostResultDigest(r)
	if hashErr != nil || digestErr != nil || r.Version != 1 || r.Kind != "host-result" || r.Tenant == "" || r.App != controlowner.PlatformApp || r.Member == "" || r.At.IsZero() ||
		!slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration", "composition"}, r.Action) || r.Key != "host:"+r.Action+":"+hash || r.RequestHash != hash || r.Digest != digest || r.Before == "" || r.AuditBefore == "" ||
		r.Audit.Member != r.Member || r.Audit.App != controlowner.PlatformApp || !r.Audit.At.Equal(r.At) || r.Audit.Action == "" {
		return r, fmt.Errorf("host result identity differs")
	}
	if err := validateHostImage(r.Tenant, r.Image); err != nil {
		return r, err
	}
	receipt, ok := r.Image.Requests[r.Key]
	if !ok || receipt.RequestHash != hash || receipt.Answer != r.Answer {
		return r, fmt.Errorf("host result has no matching receipt")
	}
	return r, nil
}

// acceptHostControl holds Tenant.mu from private image capture through append
// and application. The accepted clock is part of identity because these APIs
// predate caller supplied keys; repeating that same request returns its receipt.
func (t *Tenant) acceptHostControl(action, member string, request any, now time.Time, audit platform.AuditEntry, decide func(*hostControlImage) (SupportGrant, error), evidence ...acceptedformat.AuthorizationEvidence) (SupportGrant, error) {
	r := acceptedHostControl{Version: 1, Kind: "host-result", Tenant: t.ID, App: controlowner.PlatformApp, Member: member, Action: action, At: now.UTC(), Request: platform.Raw(request)}
	r.Authorizations = slices.Clone(evidence)
	var err error
	r.RequestHash, err = hostRequestHash(r)
	if err != nil {
		return SupportGrant{}, err
	}
	r.Key = "host:" + action + ":" + r.RequestHash
	image, err := t.console.image()
	if err != nil {
		return SupportGrant{}, err
	}
	if prior, ok := image.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash {
			return SupportGrant{}, fmt.Errorf("host request key changed")
		}
		return prior.Answer, nil
	}
	r.Before, err = platform.CanonicalDigest(image)
	if err != nil {
		return SupportGrant{}, err
	}
	r.AuditBefore, err = platform.CanonicalDigest(t.audit.All())
	if err != nil {
		return SupportGrant{}, err
	}
	r.Answer, err = decide(&image)
	if err != nil {
		return SupportGrant{}, err
	}
	if image.Requests == nil {
		image.Requests = map[string]hostControlReceipt{}
	}
	image.Requests[r.Key] = hostControlReceipt{r.RequestHash, r.Answer}
	r.Image = image
	audit.At, audit.Member, audit.App = r.At, member, controlowner.PlatformApp
	r.Audit = audit
	r.Digest, err = hostResultDigest(r)
	if err != nil {
		return SupportGrant{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return SupportGrant{}, err
	}
	if _, err = decodeAcceptedHostControl(raw); err != nil {
		return SupportGrant{}, err
	}
	principal, _ := json.Marshal(platform.Member{ID: member, Tenant: t.ID})
	committed, err := t.acceptResult(Entry{App: controlowner.PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return SupportGrant{}, err
	}
	saved, err := decodeAcceptedHostControl(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed host result differs: %v", err))
		return SupportGrant{}, fmt.Errorf("committed host result differs: %v", err)
	}
	if _, err = t.applyAcceptedHostControl(committed); err != nil {
		t.quarantine(err)
		return SupportGrant{}, err
	}
	t.acceptedResultApplied()
	t.changed()
	return saved.Answer, nil
}
func (t *Tenant) applyAcceptedHostControl(raw []byte) (acceptedHostControl, error) {
	r, err := decodeAcceptedHostControl(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != t.ID {
		return r, fmt.Errorf("host result belongs to another tenant")
	}
	current, err := t.console.image()
	if err != nil {
		return r, err
	}
	if prior, ok := current.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash || prior.Answer != r.Answer {
			return r, fmt.Errorf("host result receipt differs")
		}
		return r, nil
	}
	before, err := platform.CanonicalDigest(current)
	auditBefore, auditErr := platform.CanonicalDigest(t.audit.All())
	if err != nil || auditErr != nil || before != r.Before || auditBefore != r.AuditBefore {
		return r, fmt.Errorf("host result predecessor differs")
	}
	if err := validateHostTransition(current, r); err != nil {
		return r, err
	}
	t.console.mu.Lock()
	t.console.setState(r.Image.Lifecycle)
	t.console.support, t.console.migrations, t.console.requests = r.Image.Support, r.Image.Migrations, maps.Clone(r.Image.Requests)
	t.console.runtimeSelection = maps.Clone(r.Image.RuntimeSelection)
	t.console.disabledOwners = slices.Clone(r.Image.DisabledOwners)
	t.console.mu.Unlock()
	t.audit.Remember(r.Audit)
	return r, nil
}

// A saved image may only perform the one requested host-owned transition. This
// checks frozen bytes and predecessors; it does not rerun current authorization.
func validateHostTransition(current hostControlImage, r acceptedHostControl) error {
	raw, err := json.Marshal(current)
	var expected hostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &expected)
	}
	if err != nil {
		return err
	}
	var answer SupportGrant
	audit := platform.AuditEntry{At: r.At, Member: r.Member, App: controlowner.PlatformApp}
	switch r.Action {
	case "composition":
		var body CompositionRequest
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if body.Reason == "" || body.At.IsZero() || !body.At.Equal(r.At) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("invalid runtime maintenance request")
		}
		expected.RuntimeSelection = maps.Clone(body.Selection)
		expected.DisabledOwners = slices.Clone(body.DisabledOwners)
		expected.Lifecycle = "suspend"
		audit.Action, audit.Target = "host.composition", r.Tenant+":"+body.Reason
	case "lifecycle":
		var body struct{ Action, Reason string }
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if !slices.Contains([]string{"suspend", "decommission", "resume", "open"}, body.Action) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host lifecycle request differs")
		}
		expected.Lifecycle = ""
		if body.Action == "suspend" || body.Action == "decommission" {
			expected.Lifecycle = body.Action
		}
		audit.Action, audit.Target = "host.lifecycle."+body.Action, r.Tenant+":"+body.Reason
	case "support.open":
		var body struct {
			Member, Reason string
			Minutes        int
		}
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if body.Member == "" || body.Reason == "" || body.Minutes < 1 || body.Minutes > 1440 || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host support request differs")
		}
		answer = r.Answer
		if answer.Tenant != r.Tenant || answer.Member != body.Member || answer.Reason != body.Reason || answer.Opened != strings.TrimPrefix(r.Member, "host:") || answer.Uses != 0 || !answer.Expires.Equal(r.At.Add(time.Duration(body.Minutes)*time.Minute)) {
			return fmt.Errorf("host support grant differs from its accepted request")
		}
		expected.Support = append(expected.Support, answer)
		audit.Action, audit.Target = "host.support.open", body.Member+":"+body.Reason
	case "support.use":
		var body struct{ ID string }
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		i := slices.IndexFunc(expected.Support, func(g SupportGrant) bool { return g.ID == body.ID })
		if i < 0 || !expected.Support[i].Expires.After(r.At) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host support use predecessor differs")
		}
		expected.Support[i].Uses++
		answer = expected.Support[i]
		audit.Action, audit.Target = "host.support.use", body.ID
	case "migration":
		var body MigrationResult
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		expected.Migrations = append(expected.Migrations, MigrationManifest{At: r.At, From: body.From, To: body.To, Key: body.Key, Member: r.Member, Types: body.Types})
		audit.Action, audit.Target = "host.migration", body.Key
	}
	if expected.Requests == nil {
		expected.Requests = map[string]hostControlReceipt{}
	}
	expected.Requests[r.Key] = hostControlReceipt{r.RequestHash, answer}
	want, err := platform.CanonicalDigest(expected)
	got, gotErr := platform.CanonicalDigest(r.Image)
	wantAudit, auditErr := platform.CanonicalDigest(audit)
	gotAudit, gotAuditErr := platform.CanonicalDigest(r.Audit)
	if err != nil || gotErr != nil || auditErr != nil || gotAuditErr != nil || want != got || answer != r.Answer || wantAudit != gotAudit {
		return fmt.Errorf("host result changes unrelated state or audit")
	}
	return nil
}

func (d *stagedDecision) inputResult(app, name string, m platform.Member, body []byte, answer any, at time.Time) ([]byte, error) {
	if d.failure != nil || len(d.requests) > 0 {
		return nil, fmt.Errorf("connector input contains effects not represented by its result")
	}
	hash, err := acceptedformat.InputHash(d.tenant.ID, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := acceptedformat.Input{Version: 2, Kind: "input-result", Tenant: d.tenant.ID, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Deliveries: slices.Clone(d.deliveries), Notices: d.savedNotices(), Authorizations: slices.Clone(d.authorizations)}
	if message, ok := answer.(proto.Message); ok {
		result.Answer, err = protojson.Marshal(message)
	} else {
		result.Answer, err = json.Marshal(answer)
	}
	if err != nil {
		return nil, err
	}
	if len(d.events) > 0 {
		receipt, ok := answer.(*pb.ChangeRecord)
		if !ok || receipt.GetSubmission().GetAuthority() != app {
			return nil, fmt.Errorf("connector input has decisions without a matching answer")
		}
		result.Changes, err = d.batchResult(app, receipt.GetSubmission(), receipt, at)
		if err != nil {
			return nil, err
		}
		// The child batch owns the entire staged state; do not apply its marks,
		// notices or record images twice from the enclosing input.
		result.Deliveries, result.Notices = nil, nil
		result.Digest, err = acceptedformat.DigestInput(result)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if _, err := acceptedformat.DecodeInput(raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
	if d.control != nil || len(d.intents) > 0 || len(d.observations) > 0 || len(d.allocated) > 0 || len(d.operationCancels) > 0 {
		return nil, fmt.Errorf("connector input contains effects without a decision")
	}
	// A legacy input retry may read its saved receipt through a private log.
	// Permit that read only when every receipt still equals the live ledger;
	// a newly accepted decision without a published event cannot disappear.
	for ledger, staged := range d.logs {
		if !proto.Equal(ledger.LastAccepted(d.tenant.ID), staged.Last(d.tenant.ID)) {
			return nil, fmt.Errorf("connector input contains an unpublished decision")
		}
	}
	plan, err := d.savedChangePlan()
	if err != nil {
		return nil, err
	}
	result.States, result.Deliveries, result.Notices, result.Unique = plan.States, plan.Deliveries, plan.Notices, plan.Unique
	result.Rows, err = captureDirectRows(d, app, nil, nil, "connector input")
	if err != nil {
		return nil, err
	}
	result.Digest, err = acceptedformat.DigestInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := acceptedformat.DecodeInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func encodeAcceptedInputRefusal(tenant, app, name string, m platform.Member, body []byte, at time.Time, refusal *kernel.Error) ([]byte, error) {
	hash, err := acceptedformat.InputHash(tenant, app, name, m.ID, body)
	if err != nil {
		return nil, err
	}
	result := acceptedformat.Input{Version: 2, Kind: "input-result", Tenant: tenant, App: app,
		Name: name, Member: m.ID, Key: "input:" + hash, At: at.UTC(), Body: slices.Clone(body),
		RequestHash: hash, Refusal: refusal}
	result.Digest, err = acceptedformat.DigestInput(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err := acceptedformat.DecodeInput(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (t *Tenant) inputAccepted(a platform.AcceptedInputApp, m platform.Member, name string, body []byte, now time.Time) (any, *kernel.Error) {
	if m.ID == "" || m.Tenant != t.ID || name == "" {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The connector input has an invalid identity")
	}
	hash, hashErr := acceptedformat.InputHash(t.ID, a.Manifest().ID, name, m.ID, body)
	if hashErr != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	key := "input:" + hash
	if saved := t.committed.Input(a.Manifest().ID + "/" + key); len(saved) != 0 {
		return t.answerAcceptedInput(a, saved)
	}
	draft := t.newStagedDecision()
	entry := t.authorizeEntry(m, a.Manifest().ID, "input:"+name, "action", now)
	draft.authorizations = append(draft.authorizations, acceptedformat.AuthorizationEvidence{Request: platform.AuthzRequest{Principal: m.ID, Tenant: m.Tenant, Kind: "action", Action: "input:" + name}, Decision: entry, Roles: slices.Clone(m.RolesIn(a.Manifest().ID))})
	answer, refusal, unsupported := decideAcceptedInput(a, draft, m, name, body, now)
	if unsupported {
		return nil, refusal
	}
	var raw []byte
	var err error
	if refusal != nil {
		raw, err = encodeAcceptedInputRefusal(t.ID, a.Manifest().ID, name, m, body, now, refusal)
	} else {
		raw, err = draft.inputResult(a.Manifest().ID, name, m, body, answer, now)
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input cannot produce a bounded accepted result")
	}
	result, err := acceptedformat.DecodeInput(raw)
	if err == nil {
		_, err = t.prepareAcceptedChanges(result.ChangePlan())
	}
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	principal, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = draft.flowVersions()
	}
	committed, err := t.acceptResult(Entry{App: a.Manifest().ID, Kind: "accepted-result",
		Principal: principal, Body: raw, At: now, Versions: versions}, key, hash)
	if err != nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return t.finishCommittedInput(a, m, name, key, hash, committed)
}

func (t *Tenant) finishCommittedInput(a platform.AcceptedInputApp, m platform.Member, name, key, hash string, committed []byte) (answer any, refusal *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("apply committed connector result: %v", failure))
			answer = nil
			refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}()
	result, applied, err := t.applyAcceptedInput(committed)
	if err != nil || result.Key != key || result.RequestHash != hash || result.Member != m.ID ||
		result.Name != name || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("committed connector input differs from request: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	t.acceptedResultApplied()
	if applied && result.Refusal == nil {
		t.audit.Remember(platform.AuditEntry{At: result.At, Member: m.ID, App: result.App, Action: "input:" + name})
		t.enqueue(result.At)
		t.changedOwner(result.App)
	}
	return t.answerAcceptedInput(a, committed)
}

func decideAcceptedInput(a platform.AcceptedInputApp, draft *stagedDecision, m platform.Member, name string, body []byte, at time.Time) (answer any, refusal *kernel.Error, unsupported bool) {
	defer func() {
		if p := recover(); p != nil {
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				panic(p)
			}
			answer = nil
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This input uses an effect outside the accepted-result boundary")
			unsupported = true
		}
	}()
	answer, refusal = inputApp(draft.stateApp(a), platform.NewCaller(draft, m, a.Manifest().ID, false, false),
		name, slices.Clone(body), at)
	return answer, refusal, false
}

func (t *Tenant) answerAcceptedInput(a platform.AcceptedInputApp, raw []byte) (any, *kernel.Error) {
	result, err := acceptedformat.DecodeInput(raw)
	if err != nil || result.Tenant != t.ID || result.App != a.Manifest().ID {
		t.quarantine(fmt.Errorf("saved connector answer is incompatible: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if result.Refusal != nil {
		return nil, result.Refusal
	}
	answer, err := a.DecodeAcceptedInput(result.Answer)
	if err != nil {
		t.quarantine(fmt.Errorf("saved connector answer cannot be decoded: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return answer, nil
}

func (t *Tenant) applyAcceptedInput(raw []byte) (acceptedformat.Input, bool, error) {
	result, err := acceptedformat.DecodeInput(raw)
	if err != nil {
		return result, false, err
	}
	app, ok := t.app(result.App).(platform.AcceptedInputApp)
	if result.Tenant != t.ID || !ok || !slices.Contains(app.AcceptedInputs(), result.Name) {
		return result, false, fmt.Errorf("connector result belongs to another input")
	}
	if result.Refusal == nil {
		if _, err := app.DecodeAcceptedInput(result.Answer); err != nil {
			return result, false, fmt.Errorf("connector result has an invalid answer: %w", err)
		}
	}
	if prior := t.committed.Input(result.App + "/" + result.Key); len(prior) > 0 {
		saved, err := acceptedformat.DecodeInput(prior)
		if err != nil || saved.Digest != result.Digest {
			return result, false, fmt.Errorf("connector input key belongs to another result")
		}
		return result, false, nil
	}
	prepared, err := t.prepareAcceptedChanges(result.ChangePlan())
	if err != nil {
		return result, false, err
	}
	if err := prepared.apply(t); err != nil {
		return result, false, err
	}
	prepared.publish(t)
	if result.Refusal != nil {
		t.refused(result.Member, result.Name, result.Refusal, result.At)
	}
	t.committed.SaveInput(result.App+"/"+result.Key, raw)
	return result, true, nil
}

// resultformat.Release commits immutable candidate bytes or an activation. Format 2
// carries frozen owner publication images beside the pointer; neither is
// reconstructed from a later mutable draft.

func (t *Tenant) applyAcceptedRelease(raw []byte) (acceptedformat.Release, error) {
	saved, err := acceptedformat.DecodeRelease(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID || t.app(build.ID) == nil {
		return saved, fmt.Errorf("release result belongs to another tenant or unavailable builder")
	}
	if prior := t.releases.Raw(saved.CandidateID); prior != nil && !bytes.Equal(prior, saved.Bytes) {
		return saved, fmt.Errorf("immutable release candidate %s changed", saved.CandidateID)
	}
	if prior, ok := t.releases.AppliedDigest(saved.Key); ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("release result idempotency key changed")
		}
		return saved, nil
	}
	if len(saved.Installations) > 0 {
		records := t.records
		var bootstrap *preparedAcceptedBatch
		if len(saved.Bootstrap) > 0 {
			bootstrap, err = t.prepareAcceptedBatch(t.app(saved.App).AcceptedLedger(), saved.Bootstrap)
			if err != nil || bootstrap == nil {
				return saved, fmt.Errorf("release bootstrap cannot prepare: %v", err)
			}
			records = bootstrap.records
		}
		view, err := t.assetInstallationView(records)
		if err != nil {
			return saved, err
		}
		view.records.current.SelectRelease(saved.CandidateID)
		draft, err := view.stageReleaseInstallationLocked(saved.Installations, false)
		if err != nil {
			return saved, err
		}
		if saved.Version == 4 {
			candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
			if err != nil {
				return saved, err
			}
			running, err := draft.acceptedInstallationReleaseLocked(candidate)
			if err != nil || running.ID != candidate.ID {
				return saved, fmt.Errorf("accepted installation differs from its frozen candidate: %v", err)
			}
		}
		if err := t.anchorReleaseRecords(draft.records); err != nil {
			return saved, err
		}
		if bootstrap != nil {
			bootstrap.records = draft.records
			if _, err := t.promoteAcceptedBatch(bootstrap); err != nil {
				return saved, err
			}
		} else if err := t.records.promoteRecords(draft.records); err != nil {
			return saved, err
		}
		t.setDefinitions(draft.definitions)
		for key, app := range draft.owner {
			draft.owner[key] = t.app(app.Manifest().ID)
		}
		t.owner = draft.owner
		registry, err := t.assetRegistryLocked()
		if err != nil {
			return saved, err
		}
		for _, installation := range saved.Installations {
			plan, err := releaseowner.InstallationPlan(registry, installation)
			if err != nil {
				return saved, err
			}
			if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{}); err != nil {
				return saved, err
			}
		}
		if bootstrap != nil {
			t.publishAcceptedBatch(bootstrap.result)
			t.enqueue(saved.At)
		}
	}
	if err := t.releases.Commit(saved.Key, saved.Digest, saved.CandidateID, saved.Bytes, saved.Active); err != nil {
		return saved, err
	}
	if saved.Active {
		t.records.current.SelectRelease(saved.CandidateID)
	}
	t.releases.RememberRequest(saved.Key, saved.RequestHash)
	if saved.Version >= 3 {
		t.committed.SaveAnswer("release:"+saved.Key, raw)
	}
	if saved.From != "" {
		t.releases.RememberOrigin(saved.CandidateID, platform.ReleaseOrigin{From: saved.From, Member: saved.Member, At: saved.At})
		t.audit.Remember(platform.AuditEntry{At: saved.At, Member: saved.Member, App: controlowner.PlatformApp, Action: "host.promotion",
			Target: saved.From + "→" + t.ID + ":" + saved.CandidateID})
	}
	return saved, nil
}

// SaveReleaseCandidate freezes a previously previewed saved draft. The
// builder's private definitions are read under the tenant lock; a stale
// preview cannot be persisted by submitting its ID alone. A retry of an
// already-saved ID remains possible even after the editor changes the draft.
func (t *Tenant) SaveReleaseCandidate(m platform.Member, kind platform.AssetKind, draftID, candidateID, key string, now time.Time) (string, error) {
	return t.SaveReleaseCandidates(m, []build.JointDraftRef{{Kind: kind, ID: draftID}}, candidateID, key, now)
}

// SaveReleaseCandidates persists one or several saved drafts as one immutable
// candidate. The bytes are recomputed under the tenant lock, so a joint
// selection cannot be frozen from a stale browser image.
func (t *Tenant) SaveReleaseCandidates(m platform.Member, drafts []build.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, build.Builder, build.Publisher) {
		return "", fmt.Errorf("builder or publisher role required")
	}
	if candidateID == "" || key == "" || len(key) > 200 || now.IsZero() {
		return "", fmt.Errorf("candidate ID and idempotency key are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	var candidateBytes []byte
	if prior := t.releases.Raw(candidateID); prior != nil {
		candidateBytes = slices.Clone(prior)
	} else {
		preview, candidate, err := t.previewReleaseLocked(drafts, m)
		if err != nil {
			return "", err
		}
		if preview.Diagnostic != "" {
			return "", fmt.Errorf("release candidate: %s", preview.Diagnostic)
		}
		if candidate.ID != candidateID {
			return "", fmt.Errorf("release candidate changed since preview")
		}
		candidateBytes = candidate.Bytes
	}
	saved := acceptedformat.Release{Version: 1, Kind: "release-result", Tenant: t.ID, App: build.ID,
		Member: m.ID, Key: "release:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: candidateBytes}
	// The idempotency key has a separate namespace from builder submissions.
	var err error
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(t.ID, m.ID, saved.Key, candidateID, false)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

// ActivateRelease installs the supported saved closure and moves its pointer
// in one committed result. Unsupported upgrades and changed code
// dependencies are refused before either state is exposed.
func (t *Tenant) ActivateRelease(m platform.Member, candidateID, key string, now time.Time) (string, error) {
	return t.ActivateReleaseWithUpgrade(m, candidateID, key, "", now)
}

const systemReleaseActivationDiagnostic = "System definitions activate through an approved Platform change. Publish business applications in a business workspace."

func (t *Tenant) ActivateReleaseWithUpgrade(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	if t.ID == systemscope.Tenant {
		return "", fmt.Errorf("%s", systemReleaseActivationDiagnostic)
	}
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, build.Builder, build.Publisher) {
		return "", fmt.Errorf("builder or publisher role required")
	}
	if candidateID == "" || key == "" || len(key) > 200 || now.IsZero() {
		return "", fmt.Errorf("candidate ID and idempotency key are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	if prior := t.committed.Answer("release:activate:" + key); prior != nil {
		saved, err := acceptedformat.DecodeRelease(prior)
		hash, hashErr := acceptedformat.ReleaseRequestHash(t.ID, m.ID, "activate:"+key, candidateID, true, upgradeID)
		if err != nil || hashErr != nil || hash != saved.RequestHash {
			return "", fmt.Errorf("activation key belongs to another request")
		}
		return saved.CandidateID, nil
	}
	raw := t.releases.Raw(candidateID)
	if raw == nil {
		return "", fmt.Errorf("release candidate %s is not saved", candidateID)
	}
	var installations []platform.ReleaseInstallation
	var bootstrap json.RawMessage
	if upgradeID != "" {
		candidate, err := platform.ReadCandidate(candidateID, raw)
		if err != nil {
			return "", err
		}
		plan, err := t.releaseUpgradePlanLocked(candidate, m)
		if err != nil {
			return "", err
		}
		if plan == nil || plan.ID != upgradeID {
			return "", fmt.Errorf("storage upgrade plan changed; refresh and review it again")
		}
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return "", err
	}
	installations, bootstrap, err = t.prepareReleaseLifecycleLocked(candidate, m, now, upgradeID != "")
	if err != nil {
		return "", err
	}
	saved := acceptedformat.Release{Version: 1, Kind: "release-result", Tenant: t.ID, App: build.ID,
		Member: m.ID, Key: "activate:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: slices.Clone(raw), Active: true, Installations: installations, Bootstrap: bootstrap}
	if len(installations) > 0 {
		saved.Version = 4
	}
	if upgradeID != "" {
		saved.Version, saved.UpgradeID = 4, upgradeID
	}
	saved.RequestHash, err = acceptedformat.ReleaseRequestHash(t.ID, m.ID, saved.Key, candidateID, true, upgradeID)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

func (t *Tenant) runningMatchesLocked(candidateID string, raw []byte) error {
	saved, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	running, err := t.runningReleaseLocked(saved)
	if err != nil {
		return fmt.Errorf("release differs from the running definitions: %w", err)
	}
	if running.ID == candidateID {
		return nil
	}
	added, removed, changed, err := platform.CandidateDiff(saved, running)
	if err != nil {
		return err
	}
	return fmt.Errorf("release differs from the running definitions: changed %v, missing %v, extra %v", changed, removed, added)
}

// Read-side status and activation compare the same owner-resolved closure.
func (t *Tenant) runningReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	// Rebuild the same closure from what runs now: equal content yields the
	// same canonical ID; anything else is reported by asset path.
	roots := make([]platform.AssetRef, 0, len(saved.Assets))
	installed := make(map[platform.AssetRef]bool, len(available))
	for _, asset := range available {
		installed[asset.Ref] = true
	}
	for _, asset := range saved.Assets {
		// A first release has no running counterpart for its new assets.
		// Keep them absent from the comparison so CandidateDiff reports them
		// as additions; missing dependencies of installed roots still refuse.
		if installed[asset.Ref] {
			roots = append(roots, asset.Ref)
		}
	}
	if len(roots) == 0 {
		return platform.ReleaseCandidate{}, nil
	}
	return t.candidateWithBindings(roots, available, nil)
}

// Recovery checks the saved installation's controlled assets, not today's
// native prerequisites. Native assets in a release are declarations, never
// installation images: keep their original bytes for this comparison only.
// New activation still requires runtimeAssetOwner.controlled's exact match;
// old Flow execution keeps its separate code-dependency admission gate.
func (t *Tenant) acceptedInstallationReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	code := map[platform.AssetRef]bool{}
	for _, app := range t.apps {
		assets, err := t.codeReleaseAssetsLocked(app)
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		for _, asset := range assets {
			code[asset.Ref] = true
		}
	}
	frozen := map[platform.AssetRef]platform.ReleaseAsset{}
	roots := []platform.AssetRef{}
	for _, asset := range saved.Assets {
		roots = append(roots, asset.Ref)
		if code[asset.Ref] {
			frozen[asset.Ref] = asset
		}
	}
	for index, asset := range available {
		if original, exists := frozen[asset.Ref]; exists {
			available[index] = original
		}
	}
	return t.candidateWithBindings(roots, available, nil)
}

// pendingWorkFitsLocked refuses an activation that would change the action a
// pending approval holds: the approval must run under the release it opened
// in. There is no migration; decide or withdraw it first.
func (t *Tenant) pendingWorkFitsLocked(candidateID string, raw []byte) error {
	if t.app(platform.WorkApp) == nil {
		return nil
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	actionIn := func(c platform.ReleaseCandidate, name string) []byte {
		for _, a := range c.Assets {
			if a.Ref.Kind == platform.AssetAction && a.Ref.Name == name {
				out, _ := json.Marshal(a)
				return out
			}
		}
		return nil
	}
	pending, _, _ := platform.Find[platform.ApprovalRequest](t.automation(platform.WorkApp, false),
		platform.Query{Domain: json.RawMessage(`[["state","=","pending"]]`), Sort: []string{"id"}})
	for _, request := range pending {
		next := actionIn(candidate, request.Action)
		if next == nil || request.Release == candidateID {
			continue
		}
		if saved := t.releases.Raw(request.Release); saved != nil {
			started, err := platform.ReadCandidate(request.Release, saved)
			if err == nil && bytes.Equal(actionIn(started, request.Action), next) {
				continue
			}
		}
		return fmt.Errorf("pending approval %s holds %s under another release; decide or withdraw it before activating", request.ID, request.Action)
	}
	return nil
}

// ActiveRelease is the tenant's active release ID, empty before any activation.
func (t *Tenant) ActiveRelease() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.releases.Active()
}

// commitReleaseLocked appends one release result and applies only what the
// journal returned; the caller holds the tenant lock.
func (t *Tenant) commitReleaseLocked(m platform.Member, saved acceptedformat.Release) (string, error) {
	candidateID := saved.CandidateID
	raw, err := acceptedformat.EncodeRelease(saved)
	if err != nil {
		return "", err
	}
	principal, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	entry := Entry{App: build.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: saved.At}
	if prior, ok := t.releases.Request(saved.Key); ok {
		if prior != saved.RequestHash {
			return "", fmt.Errorf("release key %s belongs to another request", saved.Key)
		}
		return candidateID, nil
	}
	if _, applied := t.releases.AppliedDigest(saved.Key); applied && t.AcceptResult == nil {
		return "", fmt.Errorf("historical release request needs its journal receipt")
	}
	committed, err := t.acceptResult(entry, saved.Key, saved.RequestHash)
	if err != nil {
		return "", err
	}
	return t.finishCommittedRelease(m, saved, committed)
}

func (t *Tenant) finishCommittedRelease(m platform.Member, saved acceptedformat.Release, committed []byte) (id string, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("apply committed release: %v", failure)
			t.quarantine(err)
			id = ""
		}
	}()
	candidateID := saved.CandidateID
	applied, err := t.applyAcceptedRelease(committed)
	if err != nil || applied.Key != saved.Key || applied.RequestHash != saved.RequestHash ||
		applied.Member != m.ID || applied.CandidateID != candidateID {
		// Once appended, an invalid result is a tenant recovery fault, not a
		// transient error that may permit further writes over uncertain state.
		t.quarantine(fmt.Errorf("committed release result differs: %v", err))
		return "", fmt.Errorf("committed release result differs: %v", err)
	}
	t.acceptedResultApplied()
	t.changed()
	return applied.CandidateID, nil
}

// Internal protocol requests and replies are child decisions of the input,
// never separately confirmed journal writes. They run after application
// locks are released and before the single result is planned/committed.
func (d *stagedDecision) answerRequests(now time.Time) *kernel.Error {
	for count := 0; len(d.requests) > 0; count++ {
		if count >= acceptedformat.MaxHops {
			unsupportedStagedEffect()
		}
		q := d.requests[0]
		d.requests = d.requests[1:]
		if err := d.answerRequest(q, now); err != nil {
			return err
		}
	}
	return d.failure
}

func (d *stagedDecision) answerRequest(q request, now time.Time) (refusal *kernel.Error) {
	end := d.tenant.begin("request "+q.Protocol+"#"+q.Action, trace.SpanContext{}, attribute.String("platform.app", q.caller.App), attribute.String("platform.target", q.Target))
	outcome := "ok"
	defer func() {
		if refusal != nil {
			outcome = refusal.Code.String()
		}
		end(outcome)
	}()
	s := q.record.GetSubmission()
	key := fmt.Sprintf("%s:%s#%d", q.caller.App, s.GetIdempotencyKey(), q.n)
	providerKey := key
	if q.Key != "" {
		providerKey = q.Key
	}
	answer := platform.Answer{Tag: q.Tag, Call: q.Target, Action: q.Action, Outcome: "accepted"}
	var ref *pb.EntityRef
	_, err := d.Attempt(func() (*pb.ChangeRecord, *kernel.Error) {
		var record *pb.ChangeRecord
		var refusal *kernel.Error
		ref, record, refusal = d.routingTenant().invoke(q.caller, q.Protocol, q.Action, q.Target,
			platform.Raw(q.Payload), providerKey, s.GetIdempotencyKey(), now, q.Provider)
		return record, refusal
	})
	if err != nil {
		answer.Outcome, answer.Code, answer.Reason = "refused", err.Code.String(), err.Message
		outcome = answer.Code
	} else {
		answer.Ref = ref.GetType() + "/" + ref.GetId()
	}
	payload, errJSON := json.Marshal(answer)
	if errJSON != nil {
		unsupportedStagedEffect()
	}
	c := d.tenant.automated(q.caller, q.caller.App)
	reply := &pb.Submission{TenantId: d.tenant.ID, PrincipalId: c.ID, Authority: s.GetAuthority(), Target: s.GetTarget(),
		Schema: &pb.SchemaRef{Name: q.Reply, Version: 1}, IdempotencyKey: "answer:" + key,
		CorrelationId: s.GetIdempotencyKey(), Payload: payload}
	if _, err := platform.Decide(c, d.tenant.app(c.App), reply, now); err != nil {
		return err
	}
	return d.failure
}

// A direct model call has no owned effect. Its immutable usage observation is
// accepted before changing the meter, using the same predecessor as effects.

func (t *Tenant) usagePredecessor() (string, error) {
	if t.ai == nil {
		return "", fmt.Errorf("usage needs an AI meter")
	}
	raw, err := t.ai.Snapshot()
	if err != nil {
		return "", err
	}
	return platform.CanonicalDigest(raw)
}

func (t *Tenant) acceptUsage(m platform.Member, u platform.AIUsage) *kernel.Error {
	if u.Call != "" {
		for _, prior := range t.ai.Usage() {
			if prior.Call != u.Call {
				continue
			}
			a, _ := platform.CanonicalDigest(prior)
			b, _ := platform.CanonicalDigest(u)
			if a != b {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The model usage differs from its accepted reading")
			}
			return nil
		}
	}
	// Usage observes a completed call. Its principal is the meter owner; a
	// later revocation of the attributed caller cannot erase an incurred cost.
	c := platform.NewCaller(runtime{t}, platform.Member{ID: "app:" + platform.AIApp, Tenant: m.Tenant}, platform.AIApp, false, true)
	decision := t.authorizeNative(c, "usage:observe", "derive", nil, m.Tenant == t.ID && m.ID != "" && m.ID == u.Member && acceptedformat.ValidUsage(u))
	if refusal := authorizationRefusal(decision); refusal != nil {
		return refusal
	}
	before, err := t.usagePredecessor()
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	r := acceptedformat.Usage{Version: 1, Kind: "usage-result", Tenant: t.ID, App: platform.AIApp, Member: m.ID, At: u.At.UTC(), Before: before, Usage: u}
	r.Authorizations = []acceptedformat.AuthorizationEvidence{{Request: platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "derive", Action: "usage:observe"}, Decision: decision, Roles: slices.Clone(c.RolesHere())}}
	r.RequestHash, err = acceptedformat.UsageHash(before, u)
	if err != nil {
		return unknown()
	}
	r.Key = "usage:" + r.RequestHash
	r.Digest, err = acceptedformat.DigestUsage(r)
	if err != nil {
		return unknown()
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return unknown()
	}
	if _, err := acceptedformat.DecodeUsage(raw); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage cannot be accepted")
	}
	principal, _ := json.Marshal(m)
	committed, err := t.acceptResult(Entry{App: platform.AIApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The model usage was not accepted")
	}
	saved, err := acceptedformat.DecodeUsage(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed model usage differs: %v", err))
		return unknown()
	}
	if _, err := t.applyAcceptedUsage(committed); err != nil {
		t.quarantine(err)
		return unknown()
	}
	t.acceptedResultApplied()
	t.operationsChanged(platform.AIApp)
	return nil
}

func (t *Tenant) applyAcceptedUsage(raw []byte) (acceptedformat.Usage, error) {
	r, err := acceptedformat.DecodeUsage(raw)
	if err != nil {
		return r, err
	}
	before, err := t.usagePredecessor()
	if err != nil || r.Tenant != t.ID || before != r.Before {
		return r, fmt.Errorf("model usage predecessor differs: %v", err)
	}
	t.ai.Meter(r.Usage)
	return r, nil
}

// A job or delivery is a top-level input in its own right. Its immutable
// result includes the K9 generation, scheduling outcome, business changes and
// notices. No runner/listener executes when this result is recovered.
type acceptedWork struct {
	Unique         []journal.StateUnique                  `json:"unique,omitempty"`
	Authorizations []acceptedformat.AuthorizationEvidence `json:"authorizations,omitempty"`
	Version        int                                    `json:"version"`
	Kind           string                                 `json:"kind"`
	Tenant         string                                 `json:"tenant"`
	App            string                                 `json:"app"`
	Key            string                                 `json:"key"`
	At             time.Time                              `json:"at"`
	RequestHash    string                                 `json:"requestHash"`
	Digest         string                                 `json:"digest"`
	Input          automation.TaskState                   `json:"input"`
	PriorWork      json.RawMessage                        `json:"priorWork,omitempty"`
	Work           json.RawMessage                        `json:"work"`
	Outcome        string                                 `json:"outcome"`
	After          automation.Task                        `json:"after"`
	Changes        json.RawMessage                        `json:"changes,omitempty"`
	Notices        *acceptedformat.Notices                `json:"notices,omitempty"`
	Intents        []platform.Effect                      `json:"intents,omitempty"`
	States         []acceptedformat.State                 `json:"states,omitempty"`
	Control        *acceptedformat.Control                `json:"control,omitempty"`
}

func workInputHash(input automation.TaskState, prior json.RawMessage) (string, error) {
	return platform.CanonicalDigest([]any{input, prior})
}

func (t *Tenant) acceptWork(task *automation.Task, now time.Time) (outcome string) {
	if task.Event != nil {
		end := t.begin("deliver "+task.Event.Record.GetSubmission().GetSchema().GetName()+" to "+task.App, task.Event.Span,
			attribute.String("platform.app", task.App), attribute.String("platform.target", target(task.Event.Record.GetSubmission())))
		defer func() { end(outcome); counted(t, "delivery", task.App, outcome) }()
	}
	// Everything below is under the top-level tenant commit lock.
	defer func() {
		if p := recover(); p != nil {
			t.hops = 0
			t.quarantine(fmt.Errorf("accepted work: %v", p))
			outcome = pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	}()
	input, err := automation.StateOfTask(task)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	result := acceptedWork{Version: 3, Kind: "work-result", Tenant: t.ID, App: task.App,
		At: now.UTC(), Input: input, Outcome: "ok"}
	works := t.forkWorkState()
	if prior, err := works.Get(task.ID); err == nil {
		result.PriorWork, _ = protojson.Marshal(prior)
	}
	generation, _, refusal := works.Start(task.ID, "host")
	if refusal != nil {
		return refusal.Error()
	}
	result.Key = fmt.Sprintf("work:%s:%d", task.ID, generation)
	result.RequestHash, err = workInputHash(input, result.PriorWork)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	draft := t.newStagedDecision()
	draft.at = now
	if task.Event != nil {
		draft.hops = task.Event.Hops + 1
	}
	c := platform.NewCaller(draft, platform.Member{ID: "app:" + task.App, Tenant: t.ID}, task.App, false, true)
	refusal = decideWork(t, draft, c, task, now)
	result.Authorizations = slices.Clone(draft.authorizations)
	if refusal != nil {
		result.Outcome = refusal.Error()
		// A failed attempt may advance its K9 retry state, never a half-applied
		// business decision made by the listener before returning its refusal.
		draft = t.newStagedDecision()
		draft.at = now
	}
	if err := works.Finish(task.ID, generation, result.Outcome != "ok"); err != nil {
		return err.Error()
	}
	finished, _ := works.Get(task.ID)
	result.Work, _ = protojson.Marshal(finished)
	result.After = *task
	result.After.Attempts, result.After.Last, result.After.Error = int(generation), now, ""
	if task.Kind == "job" {
		result.After.Due = now.Add(task.Job.Every)
		if result.Outcome != "ok" {
			result.After.Error = result.Outcome
		}
	} else {
		retry := t.retryOf(task.App)
		switch {
		case result.Outcome == "ok":
			result.After.State = "done"
		case int(generation)-task.Since >= retry.Attempts:
			result.After.State, result.After.Error = "failed", result.Outcome
		default:
			result.After.State, result.After.Error = "retrying", result.Outcome
			result.After.Due = now.Add(retry.After(int(generation) - task.Since))
		}
	}
	if result.After.State == "failed" && t.app(controlowner.PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + controlowner.PlatformApp, Tenant: t.ID}, controlowner.PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Delivery to " + task.App + " failed: " + task.Title,
			Body: result.Outcome + ". Retry it in Settings → Automation once the cause is fixed.", Key: "failed:" + task.ID},
			now, platform.Recipient{AppRole: controlowner.Admin})
	}
	if len(draft.events) > 0 {
		last := draft.events[len(draft.events)-1]
		result.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, now)
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	} else if len(draft.records.writes) > 0 || len(draft.deliveries) > 0 || draft.failure != nil ||
		len(draft.observations) > 0 || len(draft.allocated) > 0 || len(draft.logs) > 0 || len(draft.operationCancels) > 0 || len(draft.requests) > 0 {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	} else {
		var plan acceptedformat.ChangePlan
		plan, err = draft.savedChangePlan()
		result.Notices, result.Intents = plan.Notices, plan.Intents
		result.States, result.Control = plan.States, plan.Control
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
	}
	result.Unique = draft.savedUniqueChanges()
	if task.Kind == "job" && result.Outcome == "ok" && len(result.Changes) == 0 &&
		result.Notices == nil && len(result.Intents) == 0 && len(result.States) == 0 && result.Control == nil && len(result.Unique) == 0 {
		// Polling without a business result is not an accepted K9 generation.
		// Leave the journaled predecessor intact for the next real result.
		return result.Outcome
	}
	if task.Kind == "job" {
		bindings, err := t.bindApplications(platform.AssetRef{App: task.App, Kind: platform.AssetApp, Name: task.App}, "", "", task.App)
		if err != nil {
			return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
		}
		result.After.Applications = bindings
	}
	result.Digest, err = digestAcceptedWork(result)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := decodeAcceptedWork(raw); err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := t.prepareAcceptedChanges(result.ChangePlan()); err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	principal, _ := json.Marshal(c.Member)
	committed, err := t.acceptResult(Entry{App: task.App, Kind: "accepted-result", Principal: principal, Body: raw, At: now, Versions: draft.flowVersions()},
		result.Key, result.RequestHash)
	if err != nil {
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	saved, err := decodeAcceptedWork(committed)
	if err != nil || saved.Tenant != t.ID || saved.App != task.App || saved.Key != result.Key || saved.RequestHash != result.RequestHash {
		t.quarantine(fmt.Errorf("committed work differs from input: %v", err))
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	if _, err := t.applyAcceptedWork(committed); err != nil {
		t.quarantine(err)
		return pb.ErrorCode_ERROR_CODE_CONFLICT.String()
	}
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	if len(saved.Changes) == 0 && len(saved.States) == 0 && saved.Notices == nil {
		t.operationsChanged(saved.App)
	} else {
		t.changedOwner(saved.App)
	}
	return saved.Outcome
}

func decideWork(t *Tenant, d *stagedDecision, c platform.Caller, task *automation.Task, now time.Time) (refusal *kernel.Error) {
	defer func() {
		t.hops = 0
		if p := recover(); p != nil {
			if _, unsupported := p.(stagedEffectPanic); !unsupported {
				panic(p)
			}
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action uses an effect outside the accepted-result boundary")
		}
	}()
	owner := t.app(task.App)
	if owner == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", "verb", "The operation is unavailable in its owner declaration")
	}
	permission, declared := "job:"+task.Job.Name, false
	if task.Kind == "job" {
		declared = slices.ContainsFunc(owner.Manifest().Jobs, func(job platform.Job) bool { return job.Name == task.Job.Name })
	} else if task.Event != nil {
		permission = "delivery:" + task.Event.Record.GetSubmission().GetSchema().GetName()
		declared = len(owner.Manifest().Subscribes) > 0 || slices.Contains(t.listeners, task.App)
	}
	if refusal := d.authorizeNative(c, permission, "action", owner.Manifest().AllRoles(), declared); refusal != nil {
		return refusal
	}
	if task.Kind == "job" {
		refusal = d.stateApp(t.app(task.App)).(platform.Runner).Run(c, task.Job.Name, now)
	} else {
		t.hops = task.Event.Hops + 1
		if listener, ok := d.stateApp(t.app(task.App)).(platform.Listener); ok {
			names := append([]string{task.Event.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(task.Event.Event)...)
			if task.Event.Plan != nil {
				names = task.Event.Plan.Names
			}
			refusal = listener.Listen(c, task.Event.Event, names, now)
		} else {
			refusal = d.stateApp(t.app(task.App)).(platform.Subscriber).Handle(c, task.Event.Event)
		}
	}
	if refusal == nil {
		refusal = d.failure
	}
	if refusal == nil {
		refusal = d.answerRequests(now)
	}
	if refusal == nil {
		refusal = d.stageObservers()
	}
	return refusal
}

func digestAcceptedWork(result acceptedWork) (string, error) {
	result.Digest = ""
	return platform.CanonicalDigest(result)
}

func decodeAcceptedWork(raw []byte) (acceptedWork, error) {
	var result acceptedWork
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return result, fmt.Errorf("work result is empty or too large")
	}
	if err := platform.DecodeJSON(raw, &result, "work result"); err != nil {
		return result, err
	}
	if err := acceptedformat.ValidateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, err
	}

	if result.Version < 3 && len(result.Unique) > 0 {
		return result, fmt.Errorf("legacy work result carries derived unique keys")
	}
	digest, err := digestAcceptedWork(result)
	hash, hashErr := workInputHash(result.Input, result.PriorWork)
	finished := new(pb.Work)
	workErr := protojson.Unmarshal(result.Work, finished)
	if err != nil || hashErr != nil || result.Digest != digest || result.RequestHash != hash || workErr != nil ||
		(result.Version != 1 && result.Version != 2 && result.Version != 3) || result.Version == 1 && (result.Control != nil || len(result.States) > 0) || result.Kind != "work-result" || result.Tenant == "" || result.App == "" || result.At.IsZero() ||
		result.Input.ID == "" || result.Input.App != result.App || result.Input.Kind != "delivery" && result.Input.Kind != "job" ||
		result.After.ID != result.Input.ID || result.After.App != result.App || result.After.Kind != result.Input.Kind ||
		result.After.Title != result.Input.Title || result.After.Attempts != result.Input.Attempts+1 ||
		!result.After.Last.Equal(result.At) || result.Key != fmt.Sprintf("work:%s:%d", result.Input.ID, result.After.Attempts) ||
		finished.GetWorkId() != result.Input.ID || finished.GetOwnerId() != "host" ||
		int(finished.GetGeneration()) != result.After.Attempts || result.Outcome == "" {
		return result, fmt.Errorf("work result has inconsistent identity, outcome or digest")
	}
	if err := appowner.CheckBindings(result.After.Applications); err != nil {
		return result, err
	}
	if result.Input.Since < 0 || result.Input.Since > result.Input.Attempts {
		return result, fmt.Errorf("work result has an invalid retry base")
	}
	if len(result.PriorWork) > 0 {
		prior := new(pb.Work)
		if protojson.Unmarshal(result.PriorWork, prior) != nil || prior.GetWorkId() != result.Input.ID ||
			int(prior.GetGeneration()) != result.Input.Attempts || prior.GetState() == pb.WorkState_WORK_STATE_RUNNING {
			return result, fmt.Errorf("work result has an invalid prior generation")
		}
	} else if result.Input.Attempts != 0 {
		return result, fmt.Errorf("work result omits its prior generation")
	}
	if result.Input.Kind == "job" {
		if result.Input.State != "scheduled" || result.After.State != "scheduled" || !result.After.Due.After(result.At) ||
			result.Outcome == "ok" && result.After.Error != "" ||
			result.Outcome != "ok" && result.After.Error != result.Outcome {
			return result, fmt.Errorf("work result has an invalid job outcome")
		}
	} else {
		if result.Input.State != "queued" && result.Input.State != "retrying" ||
			result.Outcome == "ok" && (result.After.State != "done" || result.After.Error != "") ||
			result.Outcome != "ok" && (result.After.State != "failed" && result.After.State != "retrying" || result.After.Error != result.Outcome) ||
			result.After.State == "retrying" && !result.After.Due.After(result.At) ||
			result.After.State != "retrying" && !result.After.Due.Equal(result.Input.Due) {
			return result, fmt.Errorf("work result has an invalid delivery outcome")
		}
	}
	if len(result.Changes) > 0 {
		changes, _, err := acceptedformat.DecodeBatch(result.Changes)
		if err != nil || changes.Tenant != result.Tenant || !changes.At.Equal(result.At) || result.Notices != nil || len(result.Intents) > 0 || len(result.States) > 0 || result.Control != nil ||
			result.Outcome != "ok" {
			return result, fmt.Errorf("work result has invalid business changes")
		}
	}
	return result, nil
}

func (t *Tenant) applyAcceptedWork(raw []byte) (bool, error) {
	result, err := decodeAcceptedWork(raw)
	if err != nil || result.Tenant != t.ID || t.app(result.App) == nil {
		return false, fmt.Errorf("work result cannot be applied: %v", err)
	}
	finished := new(pb.Work)
	_ = protojson.Unmarshal(result.Work, finished)
	previous, previousErr := t.works.Get(result.Input.ID)
	if previousErr == nil && previous.GetGeneration() >= finished.GetGeneration() {
		if previous.GetGeneration() == finished.GetGeneration() && !proto.Equal(previous, finished) {
			return false, fmt.Errorf("work result generation differs")
		}
		return false, nil
	}
	var task *automation.Task
	t.opsMu.Lock()
	if result.Input.Kind == "job" {
		task = t.work.Job(result.Input.ID)
	} else {
		task = t.work.Delivery(result.App, result.Input.ID)
	}
	t.opsMu.Unlock()
	if task == nil {
		return false, fmt.Errorf("work result needs missing work %s", result.Input.ID)
	}
	before, err := automation.StateOfTask(task)
	var prior json.RawMessage
	if previousErr == nil {
		prior, _ = protojson.Marshal(previous)
	}
	hash, hashErr := workInputHash(before, prior)
	if err != nil || hashErr != nil || hash != result.RequestHash {
		return false, fmt.Errorf("work result predecessor differs")
	}
	// Kernel validation and notification validation precede every promotion.
	works := t.forkWorkState()
	generation, _, refusal := works.Start(task.ID, "host")
	if refusal != nil || int(generation) != result.After.Attempts ||
		works.Finish(task.ID, generation, result.Outcome != "ok") != nil {
		return false, fmt.Errorf("work result K9 generation differs")
	}
	expected, _ := works.Get(task.ID)
	if !proto.Equal(expected, finished) {
		return false, fmt.Errorf("work result K9 state differs")
	}
	prepared, err := t.prepareAcceptedChanges(result.ChangePlan())
	if err != nil {
		return false, err
	}
	if err := prepared.apply(t); err != nil {
		return false, err
	}
	t.installAcceptedWork(finished)
	t.opsMu.Lock()
	task.RestoreOutcome(result.After)
	if task.Kind == "delivery" {
		switch task.State {
		case "done", "failed":
			t.work.Settled(task)
		}
	}
	t.opsMu.Unlock()
	if task.Kind == "delivery" {
		s := task.Event.Record.GetSubmission()
		t.delivered(platform.Delivery{At: result.At, App: task.Event.App, Action: s.GetSchema().GetName(), Target: target(s),
			Subscriber: task.App, Outcome: result.Outcome, Attempt: task.Attempts})
	}
	prepared.publish(t)
	return true, nil
}

func (t *Tenant) forkWorkState() platform.Works {
	items := t.works.All()
	for i, item := range items {
		items[i] = proto.Clone(item).(*pb.Work)
	}
	draft := t.services.Kernel.Works()
	draft.Restore(items)
	return draft
}

// Completing one accepted input must not restore its earlier full K9 fork:
// its child decision may already have cancelled another running generation.
func (t *Tenant) installAcceptedWork(finished *pb.Work) {
	all := t.works.All()
	found := false
	for i, work := range all {
		if work.GetWorkId() == finished.GetWorkId() {
			all[i], found = finished, true
			break
		}
	}
	if !found {
		all = append(all, finished)
	}
	t.works.Restore(all)
}
