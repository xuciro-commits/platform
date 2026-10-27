package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/host"
	"platformserver/platform"
	"reflect"
	"slices"
	"time"
)

// applyAcceptedEffect checks every predecessor before promoting an effect
// outcome. Neither the application callback nor the external send is run here.
func (t *Tenant) applyAcceptedEffect(raw []byte) (resultformat.Effect, bool, error) {
	result, err := resultformat.DecodeEffect(raw)
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
	var before resultformat.EffectState
	if current != nil {
		before = resultformat.EffectState{Effect: current.Effect, Since: current.since}
	}
	t.opsMu.Unlock()
	beforeHash, err := acceptance.Digest(before)
	savedHash, savedErr := acceptance.Digest(result.Before)
	if current == nil || err != nil || savedErr != nil || beforeHash != savedHash {
		return result, false, fmt.Errorf("effect result predecessor differs")
	}
	works, priorWork, savedWork, err := t.prepareOperationFinish(result.Before.Effect, result.Outcome)
	if err != nil {
		return result, false, err
	}
	if works != nil {
		priorHash, _ := acceptance.Digest(priorWork)
		expectedPrior, _ := acceptance.Digest(result.WorkBefore)
		workHash, _ := acceptance.Digest(savedWork)
		expectedWork, _ := acceptance.Digest(result.Work)
		if priorHash != expectedPrior || workHash != expectedWork {
			return result, false, fmt.Errorf("operation result ownership differs")
		}
	}
	if result.Usage != nil {
		if t.ai == nil {
			return result, false, fmt.Errorf("effect result needs the AI meter")
		}
		state, err := t.ai.Snapshot()
		currentHash, hashErr := acceptance.Digest(state)
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
	var before resultformat.EffectState
	if pending != nil {
		before = resultformat.EffectState{Effect: pending.Effect, Since: pending.since}
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
		if before.Effect.Endpoint == resultformat.ModelEndpoint {
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
	// New attempts always retain a safe public message when they retain an
	// error. An empty message on an old effect remains an explicit legacy case.
	if out.Detail != "" && out.PublicMessage == "" {
		out.PublicMessage = "The last delivery attempt failed"
	}
	after := &effect{Effect: before.Effect, since: before.Since}
	markEffect(after, out, at)
	result := resultformat.Effect{Version: 5, Kind: "effect-result", Tenant: t.ID, App: PlatformApp,
		Key: resultformat.EffectResultKey(before), At: at.UTC(), Before: before,
		After: resultformat.EffectState{Effect: after.Effect, Since: after.since}, Outcome: out, Usage: usage}
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
			result.UsageBefore, snapshotErr = acceptance.Digest(state)
		}
		if snapshotErr != nil {
			t.quarantine(fmt.Errorf("effect %s usage checkpoint: %w", id, snapshotErr))
			return
		}
	}
	result.RequestHash, err = resultformat.EffectResultHash(before, out, result.At)
	if err != nil {
		t.quarantine(err)
		return
	}
	draft := t.newStagedDecision()
	draft.at = at
	if after.State == "failed" && t.app(PlatformApp) != nil {
		admin := platform.NewCaller(draft, platform.Member{ID: "app:" + PlatformApp, Tenant: t.ID}, PlatformApp, false, true)
		admin.Notify(platform.Notification{Title: "Effect to " + after.Endpoint + " failed: " + after.Event,
			Body: after.Error + ". Retry it in Settings → Automation once the cause is fixed.",
			Key:  "failed:" + after.ID}, at, platform.Recipient{AppRole: Admin})
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
			var plan resultformat.ChangePlan
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
	result.Digest, err = resultformat.DigestEffect(result)
	if err != nil {
		t.quarantine(err)
		return
	}
	raw, err := json.Marshal(result)
	if err == nil {
		_, err = resultformat.DecodeEffect(raw)
	}
	if err != nil {
		t.quarantine(fmt.Errorf("encode effect %s: %w", id, err))
		return
	}
	principal, _ := json.Marshal(t.automation(PlatformApp, false).Member)
	committed, err := t.acceptResult(Entry{App: PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: draft.flowVersions()}, result.Key, result.RequestHash)
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
	if x.Endpoint == resultformat.ModelEndpoint {
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
	if x.Endpoint == resultformat.OperationEndpoint {
		draft.operationAnswers = map[string]platform.OperationResult{x.ID: operationResultOf(x)}
		processesOwner := t.procs
		if app, ok := t.procs.(platform.App); ok {
			processesOwner = draft.stateApp(app).(host.Processes)
		}
		if processes, ok := processesOwner.(host.OperationCompletion); ok {
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
		draft.stateApp(t.agents).(AgentRuntime).AgentEffectEnded(t.automated(c, AgentApp), nil, x, out, at)
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

// Notification state and already-rendered mail intents belong to the decision,
// not to enqueue. Replaying a result does not resolve recipients or translate it.

// Reuse the tenant recipient resolver over private directory owners. This
// read view has no runtime lane, storage hooks or mutable host components.
func (d *stagedDecision) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	view := &Tenant{apps: slices.Clone(d.tenant.apps), appIndex: d.tenant.appIndex, directory: d.directoryView()}
	for i, app := range view.apps {
		if app.Manifest().ID == PlatformApp {
			view.apps[i] = d.stateApp(app)
			break
		}
	}
	return view.recipients(c, now, to)
}

func (d *stagedDecision) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	var out []string
	for _, member := range d.recipients(c, now, to) {
		if n.Key != "" && slices.ContainsFunc(d.notices, func(x platform.Notification) bool {
			return x.Member == member && x.App == c.App && x.Key == n.Key
		}) {
			continue
		}
		d.noticeSeq++
		x := n
		x.ID, x.Member, x.App, x.At, x.Read = fmt.Sprintf("n-%d", d.noticeSeq), member, c.App, now, false
		d.notices = append(d.notices, x)
		d.noticeChanged = true
		out = append(out, member)
		console, _ := d.tenant.app(PlatformApp).(IdentityProfile)
		if private, ok := d.states[PlatformApp].(IdentityProfile); ok {
			console = private // invitation recipients exist in the accepted directory fork
		}
		if console != nil {
			to := console.Reach(member)
			if !to.InApp {
				d.notices[len(d.notices)-1].Read = true
			}
			router := d.routingTenant()
			router.opsMu.Lock()
			d.noticeEffects = append(d.noticeEffects, router.planMailNotice(x, to)...)
			router.opsMu.Unlock()
		}
	}
	if len(d.notices) > noticesKept {
		d.notices = d.notices[len(d.notices)-noticesKept:]
	}
	return out
}

func (d *stagedDecision) Seen(c platform.Caller, keys ...string) {
	for i, n := range d.notices {
		if n.App == c.App && slices.Contains(keys, n.Key) && !n.Read {
			d.notices[i].Read = true
			d.noticeChanged = true
		}
	}
}

func (d *stagedDecision) savedNotices() *resultformat.Notices {
	if !d.noticeChanged {
		return nil
	}
	return &resultformat.Notices{Base: d.noticeBase, Seq: d.noticeSeq, Before: d.noticeBefore,
		Notices: slices.Clone(d.notices), Effects: slices.Clone(d.noticeEffects)}
}

func (t *Tenant) validateAcceptedNotices(n *resultformat.Notices) error {
	if n == nil {
		return nil
	}
	current, seq := t.notices.state()
	before, err := acceptance.Digest(current)
	if err != nil || n.Base != seq || n.Before != before || n.Seq < n.Base || len(n.Notices) > noticesKept {
		return fmt.Errorf("notification result predecessor differs")
	}
	ids := map[string]bool{}
	prior := map[string]platform.Notification{}
	for _, x := range current {
		prior[x.ID] = x
	}
	for _, x := range n.Notices {
		if x.ID == "" || x.App == "" || x.Member == "" || x.At.IsZero() || ids[x.ID] {
			return fmt.Errorf("notification result has an invalid notification")
		}
		ids[x.ID] = true
		if p, ok := prior[x.ID]; ok {
			p.Read = x.Read
			if p != x || prior[x.ID].Read && !x.Read {
				return fmt.Errorf("notification result rewrites an existing notice")
			}
		}
	}
	// The only operations are appending stable numbered notices and marking
	// existing ones read. Retention may drop a prefix, never an arbitrary row.
	expected := current
	for i, x := range expected {
		if found := slices.IndexFunc(n.Notices, func(n platform.Notification) bool { return n.ID == x.ID }); found >= 0 {
			expected[i].Read = n.Notices[found].Read
		}
	}
	added := 0
	for _, x := range n.Notices {
		if _, known := prior[x.ID]; !known {
			added++
			want := n.Base + added
			if n.Seq-n.Base > noticesKept {
				want = n.Seq - noticesKept + added
			}
			if x.ID != fmt.Sprintf("n-%d", want) {
				return fmt.Errorf("notification result numbering differs")
			}
			expected = append(expected, x)
		}
	}
	if added != min(n.Seq-n.Base, noticesKept) {
		return fmt.Errorf("notification result omits an allocated notice")
	}
	if len(expected) > noticesKept {
		expected = expected[len(expected)-noticesKept:]
	}
	if !reflect.DeepEqual(expected, n.Notices) {
		return fmt.Errorf("notification result omits or reorders existing notices")
	}
	for _, effect := range n.Effects {
		if effect.ID == "" || effect.Endpoint == "" || effect.State != "pending" || effect.At.IsZero() {
			return fmt.Errorf("notification result has an invalid mail intent")
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedNotices(n *resultformat.Notices) {
	if n == nil {
		return
	}
	t.notices.restore(slices.Clone(n.Notices), n.Seq)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.acted += n.Seq - n.Base
	for _, planned := range n.Effects {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}

// A caught child refusal is a savepoint, not a partial accepted transaction.
// This copies only private record/ledger/intent owners, never the live tenant.
func (d *stagedDecision) Attempt(decide func() (*pb.ChangeRecord, *kernel.Error)) (r *pb.ChangeRecord, err *kernel.Error) {
	saved := *d
	*d = *d.privateFork()
	d.attachPrivateHosts()
	defer func() {
		if p := recover(); p != nil {
			*d = saved
			d.attachPrivateHosts()
			// An unsupported effect is not a business refusal from the child.
			// It invalidates the entire top-level attempt; inventing a refused
			// provider answer would turn a supported business operation into a
			// durable, false rejection.
			panic(p)
		}
	}()
	r, err = decide()
	if err == nil && r != nil && len(d.events) > len(saved.events) && slices.ContainsFunc(d.events[len(saved.events):], func(event platform.Event) bool {
		return event.App == r.GetSubmission().GetAuthority() && event.Record.GetChangeId() == r.GetChangeId()
	}) {
		// A completed child decision must report its deferred business refusal
		// before its caller chooses a success/error path. An in-progress parent
		// transaction has no published receipt yet and keeps its final boundary.
		err = d.checkFinalConstraints()
	}
	if err != nil {
		*d = saved
		d.attachPrivateHosts()
	}
	return r, err
}

func (d *stagedDecision) privateFork() *stagedDecision {
	fork := *d
	fork.businessFacts = slices.Clone(d.businessFacts)
	for i := range fork.businessFacts {
		fork.businessFacts[i] = resultformat.CloneBusinessFact(fork.businessFacts[i])
	}
	fork.businessRefusals = maps.Clone(d.businessRefusals)
	for key, fact := range fork.businessRefusals {
		fork.businessRefusals[key] = resultformat.CloneBusinessFact(fact)
	}
	fork.callbackWrites = maps.Clone(d.callbackWrites)
	if d.callbackOrigin != nil {
		origin := *d.callbackOrigin
		origin.Output = slices.Clone(origin.Output)
		fork.callbackOrigin = &origin
	}
	fork.operationCancels = maps.Clone(d.operationCancels)
	fork.operationAnswers = maps.Clone(d.operationAnswers)
	for id, answer := range fork.operationAnswers {
		answer.Output = slices.Clone(answer.Output)
		if answer.Ref != nil {
			ref := *answer.Ref
			answer.Ref = &ref
		}
		fork.operationAnswers[id] = answer
	}
	// Prepared artifacts are immutable, but consuming a queue belongs to this
	// attempt. A refused child or probe must leave its parent's queue intact.
	fork.operationInputs = maps.Clone(d.operationInputs)
	for hash, pending := range fork.operationInputs {
		fork.operationInputs[hash] = slices.Clone(pending)
	}
	fork.inputIssues = maps.Clone(d.inputIssues)
	fork.constraintWrites = maps.Clone(d.constraintWrites)
	for refusal, issues := range fork.inputIssues {
		issues = slices.Clone(issues)
		for i := range issues {
			issues[i].Translations = maps.Clone(issues[i].Translations)
			issues[i].Values = maps.Clone(issues[i].Values)
			for name, raw := range issues[i].Values {
				issues[i].Values[name] = slices.Clone(raw)
			}
			issues[i].Path = slices.Clone(issues[i].Path)
			issues[i].RelatedPaths = slices.Clone(issues[i].RelatedPaths)
			for j := range issues[i].RelatedPaths {
				issues[i].RelatedPaths[j] = slices.Clone(issues[i].RelatedPaths[j])
			}
		}
		fork.inputIssues[refusal] = issues
	}
	fork.records = d.records.forkRecords()
	fork.records.parent, fork.records.baseGeneration = d.records.parent, d.records.baseGeneration
	fork.records.writes = maps.Clone(d.records.writes)
	fork.logs = map[*platform.Ledger]platform.ChangeLog{}
	for ledger, log := range d.logs {
		suspended := d.receiving[ledger]
		for id := range d.active {
			owner := d.tenant.app(id)
			if owner != nil && owner.AcceptedLedger() == ledger {
				suspended = true
				break
			}
		}
		if suspended {
			// Receive appends its parent's receipt after deciding the nested
			// child. The parent's live stack already holds this private log.
			fork.logs[ledger] = log
		} else {
			fork.logs[ledger] = log.ForkLog()
		}
	}
	fork.events, fork.notices = slices.Clone(d.events), slices.Clone(d.notices)
	fork.definitions = maps.Clone(d.definitions)
	fork.sequences, fork.allocated, fork.sequenceBases = maps.Clone(d.sequences), maps.Clone(d.allocated), maps.Clone(d.sequenceBases)
	fork.writers, fork.changed = cloneLists(d.writers), cloneLists(d.changed)
	fork.noticeEffects, fork.publications = slices.Clone(d.noticeEffects), maps.Clone(d.publications)
	fork.intents = slices.Clone(d.intents)
	fork.iterations = slices.Clone(d.iterations)
	fork.authorizations = slices.Clone(d.authorizations)
	fork.observations = slices.Clone(d.observations)
	fork.active = maps.Clone(d.active)
	fork.receiving = maps.Clone(d.receiving)
	fork.requests, fork.requestCounts = slices.Clone(d.requests), maps.Clone(d.requestCounts)
	descriptors, marks := d.connectors.State()
	for i, descriptor := range descriptors {
		descriptors[i] = proto.Clone(descriptor).(*pb.ConnectorDescriptor)
	}
	fork.connectors = d.tenant.services.Kernel.Connectors()
	fork.connectors.Restore(descriptors, marks)
	fork.deliveries = slices.Clone(d.deliveries)
	fork.privateApps = map[string]platform.App{}
	fork.states, fork.stateBases = map[string]platform.AcceptedStateApp{}, map[string]json.RawMessage{}
	for id, base := range d.stateBases {
		fork.stateBases[id] = slices.Clone(base)
	}
	for id, owner := range d.states {
		// A suspended parent is still executing this receiver. Children cannot
		// re-enter its owner (Decide guards active IDs), so keep that receiver
		// instead of replacing it or taking its already-held decision lock.
		if d.active[id] || d.receiving[owner.AcceptedLedger()] {
			fork.states[id], fork.privateApps[id] = owner, owner
			continue
		}
		app, err := owner.ForkAcceptedState()
		copy, ok := app.(platform.AcceptedStateApp)
		if err != nil || !ok {
			unsupportedStagedEffect()
		}
		fork.states[id], fork.privateApps[id] = copy, app
	}
	for id, app := range d.privateApps {
		if d.active[id] || d.receiving[app.AcceptedLedger()] {
			fork.privateApps[id] = app
			continue
		}
		if fork.privateApps[id] != nil {
			continue
		}
		owner, ok := app.(platform.DecisionForkApp)
		if !ok {
			unsupportedStagedEffect()
		}
		copy, err := owner.ForkDecision()
		if err != nil || copy == nil || copy.Manifest().ID != id || copy.AcceptedLedger() != app.AcceptedLedger() {
			unsupportedStagedEffect()
		}
		fork.privateApps[id] = copy
	}
	for id, app := range fork.privateApps {
		if d.active[id] || d.receiving[app.AcceptedLedger()] {
			continue
		}
		host.Attach(app, hostView{t: d.tenant, app: app, decision: &fork})
	}
	fork.forkControlDecision(d.control)
	for ledger := range fork.logs {
		fork.bindPrivateFacts(ledger)
	}
	return &fork
}

func (d *stagedDecision) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	probe := d.privateFork()
	err := probe.runProbe(c, decide)
	if err != nil {
		// Carry only the refused answer's diagnostics across a read-only probe.
		// No private rows, intents, numbers or successful branch facts escape.
		if fact, ok := probe.businessRefusals[err]; ok {
			d.BusinessRefused(err, fact)
		} else if issues := probe.inputIssues[err]; len(issues) > 0 {
			d.InputRefused(err, issues)
		}
	}
	return err
}

// runProbe owns its mode and all host-managed writes. No live tenant or
// parent decision is switched into a different mode while rules run.
func (d *stagedDecision) runProbe(c platform.Caller, decide func(platform.Caller) *kernel.Error) (err *kernel.Error) {
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(stagedEffectPanic); !ok {
				panic(failure)
			}
			err = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action uses an effect outside the accepted-result boundary")
		}
	}()
	d.probing = true
	err = decide(platform.NewCaller(d, c.Member, c.App, c.Replaying, c.Automation))
	if err == nil {
		err = d.failure
	}
	if err == nil {
		err = d.checkFinalConstraints()
	}
	return err
}

func cloneLists(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, list := range src {
		dst[key] = slices.Clone(list)
	}
	return dst
}

func (d *stagedDecision) stageObservers() *kernel.Error {
	if d.observed {
		return d.failure
	}
	count := len(d.events)
	for _, e := range slices.Clone(d.events) {
		for _, app := range d.tenant.liveApps() {
			_, observer := app.(host.Observer)
			if !observer {
				continue
			}
			owner, ok := d.stateApp(app).(host.AcceptedObserver)
			if !ok {
				unsupportedStagedEffect()
			}
			id := app.Manifest().ID
			c := platform.NewCaller(d, platform.Member{ID: "app:" + id, Tenant: d.tenant.ID}, id, false, true)
			raw, err := owner.PlanObserved(c, e, d.tenant.protocolEvents(e))
			if err != nil || owner.ValidateObserved(raw) != nil {
				return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			d.observations = append(d.observations, resultformat.Observation{App: id, Body: raw})
		}
	}
	if len(d.events) != count {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	d.observed = true
	return d.failure
}

func (t *Tenant) validateAcceptedObservations(saved []resultformat.Observation) error {
	for _, item := range saved {
		owner, ok := t.app(item.App).(host.AcceptedObserver)
		if !ok || len(item.Body) == 0 {
			return fmt.Errorf("accepted observation needs missing projection %s", item.App)
		}
		if err := owner.ValidateObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedObservations(saved []resultformat.Observation) error {
	for _, item := range saved {
		if err := t.app(item.App).(host.AcceptedObserver).ApplyObserved(item.Body); err != nil {
			return err
		}
	}
	return nil
}

// A connector's cursor and last-seen mark belong to the same durable result
// as the decisions caused by its delivery. Before guards both fields, including
// push deliveries (which have no cursor transition).

func markFor(marks []kernel.ConnectorMark, tenant, connector string) kernel.ConnectorMark {
	for _, mark := range marks {
		if mark.Tenant == tenant && mark.Connector == connector {
			return mark
		}
	}
	return kernel.ConnectorMark{}
}

func (t *Tenant) validateAcceptedDeliveries(deliveries []resultformat.ConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	descriptors, marks := t.connectors.kernel.State()
	private := t.services.Kernel.Connectors()
	private.Restore(descriptors, marks)
	for _, delivery := range deliveries {
		if delivery.Connector == "" || delivery.DataClass == "" || delivery.At.IsZero() || delivery.Before == "" {
			return fmt.Errorf("accepted connector delivery is incomplete")
		}
		_, current := private.State()
		before, err := acceptance.Digest(markFor(current, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor differs", delivery.Connector)
		}
		if err := private.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s delivery: %w", delivery.Connector, err)
		}
	}
	return nil
}

func (t *Tenant) applyAcceptedDeliveries(deliveries []resultformat.ConnectorDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, delivery := range deliveries {
		_, marks := t.connectors.kernel.State()
		before, err := acceptance.Digest(markFor(marks, t.ID, delivery.Connector))
		if err != nil || before != delivery.Before {
			return fmt.Errorf("accepted connector %s predecessor changed during application", delivery.Connector)
		}
		if err := t.connectors.kernel.Deliver(t.ID, delivery.Connector, delivery.DataClass,
			delivery.From, delivery.To, delivery.At); err != nil {
			return fmt.Errorf("accepted connector %s changed during application: %w", delivery.Connector, err)
		}
	}
	return nil
}
