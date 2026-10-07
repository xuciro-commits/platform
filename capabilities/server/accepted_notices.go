package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Notification state and already-rendered mail intents belong to the decision,
// not to enqueue. Replaying a result does not resolve recipients or translate it.
type acceptedNotices struct {
	Base    int                     `json:"base"`
	Seq     int                     `json:"seq"`
	Before  string                  `json:"before"`
	Notices []platform.Notification `json:"notices"`
	Effects []platform.Effect       `json:"effects,omitempty"`
}

func (d *stagedDecision) Notify(c platform.Caller, n platform.Notification, now time.Time, to []platform.Recipient) []string {
	var out []string
	for _, member := range d.tenant.recipients(c, now, to) {
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
		if console, ok := d.tenant.app(PlatformApp).(*Console); ok {
			address, language := console.Account(member).Effective.Email, console.language(member)
			d.tenant.opsMu.Lock()
			d.noticeEffects = append(d.noticeEffects, d.tenant.planMailNotice(x, address, language)...)
			d.tenant.opsMu.Unlock()
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

func (d *stagedDecision) savedNotices() *acceptedNotices {
	if !d.noticeChanged {
		return nil
	}
	return &acceptedNotices{Base: d.noticeBase, Seq: d.noticeSeq, Before: d.noticeBefore,
		Notices: slices.Clone(d.notices), Effects: slices.Clone(d.noticeEffects)}
}

func (t *Tenant) validateAcceptedNotices(n *acceptedNotices) error {
	if n == nil {
		return nil
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	before, err := canonicalDigest(t.notices)
	if err != nil || n.Base != t.noticeSeq || n.Before != before || n.Seq < n.Base || len(n.Notices) > noticesKept {
		return fmt.Errorf("notification result predecessor differs")
	}
	ids := map[string]bool{}
	prior := map[string]platform.Notification{}
	for _, x := range t.notices {
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
	expected := slices.Clone(t.notices)
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

func (t *Tenant) applyAcceptedNotices(n *acceptedNotices) {
	if n == nil {
		return
	}
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.notices, t.noticeSeq = slices.Clone(n.Notices), n.Seq
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
	defer func() {
		if p := recover(); p != nil {
			*d = saved
			// An unsupported effect is not a business refusal from the child.
			// It invalidates the entire top-level attempt; inventing a refused
			// provider answer would turn a supported business operation into a
			// durable, false rejection.
			panic(p)
		}
	}()
	r, err = decide()
	if err != nil {
		*d = saved
	}
	return r, err
}

func (d *stagedDecision) privateFork() *stagedDecision {
	fork := *d
	fork.operationCancels = maps.Clone(d.operationCancels)
	fork.operationAnswers = maps.Clone(d.operationAnswers)
	fork.records = d.records.forkRecords()
	fork.records.parent, fork.records.baseGeneration = d.records.parent, d.records.baseGeneration
	fork.records.writes = maps.Clone(d.records.writes)
	fork.logs = map[*platform.Ledger]*kernel.ChangeLog{}
	for ledger, log := range d.logs {
		fork.logs[ledger] = log.Fork()
	}
	fork.events, fork.notices = slices.Clone(d.events), slices.Clone(d.notices)
	fork.sequences, fork.allocated, fork.sequenceBases = maps.Clone(d.sequences), maps.Clone(d.allocated), maps.Clone(d.sequenceBases)
	fork.writers, fork.changed = cloneLists(d.writers), cloneLists(d.changed)
	fork.noticeEffects, fork.publications = slices.Clone(d.noticeEffects), maps.Clone(d.publications)
	fork.intents = slices.Clone(d.intents)
	fork.observations = slices.Clone(d.observations)
	fork.active = maps.Clone(d.active)
	fork.requests, fork.requestCounts = slices.Clone(d.requests), maps.Clone(d.requestCounts)
	descriptors, marks := d.connectors.State()
	fork.connectors = kernel.NewConnectors()
	fork.connectors.Restore(descriptors, marks)
	fork.deliveries = slices.Clone(d.deliveries)
	fork.states, fork.stateBases = map[string]platform.AcceptedStateApp{}, map[string]json.RawMessage{}
	for id, base := range d.stateBases {
		fork.stateBases[id] = slices.Clone(base)
	}
	for id, owner := range d.states {
		app, err := owner.ForkAcceptedState()
		copy, ok := app.(platform.AcceptedStateApp)
		if err != nil || !ok {
			unsupportedStagedEffect()
		}
		fork.states[id] = copy
	}
	for ledger := range fork.logs {
		fork.bindPrivateFacts(ledger)
	}
	return &fork
}

func (d *stagedDecision) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	probe := d.privateFork()
	probe.probing = true
	return decide(platform.NewCaller(probe, c.Member, c.App, c.Replaying, c.Automation))
}

func cloneLists(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for key, list := range src {
		dst[key] = slices.Clone(list)
	}
	return dst
}
