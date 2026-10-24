package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"reflect"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

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
	fork.migrationDefinitions = slices.Clone(d.migrationDefinitions)
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
