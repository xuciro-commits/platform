package host

import (
	"fmt"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
	"reflect"
	"slices"
	"strings"
	"time"
)

func (t *Tenant) validateAcceptedIntents(intents []platform.Effect) error {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	ids := map[string]bool{}
	for _, x := range intents {
		if err := platform.CheckApplicationBindings(x.Applications); err != nil {
			return err
		}
		kind, ok := strings.CutPrefix(x.Event, x.App+"/")
		id := fmt.Sprintf("%s:%s:%s:%s:%s", t.ID, x.App, kind, x.Key, x.Endpoint)
		if kind == "model" && x.Endpoint == acceptedformat.ModelEndpoint {
			id = fmt.Sprintf("%s:%s:model:%s", t.ID, x.App, x.Key)
		}
		if !ok || kind == "" || x.App == "" || t.app(x.App) == nil || x.Key == "" || x.Endpoint == "" ||
			x.ID != id ||
			ids[x.ID] || x.State != "pending" && x.State != "held" || x.At.IsZero() || !x.Due.Equal(x.At) ||
			x.Attempts != 0 || !x.Last.IsZero() || x.Error != "" || x.Digest != "" ||
			x.State == "held" && x.Agent == "" || slices.ContainsFunc(t.outbound, func(e *effect) bool { return e.ID == x.ID }) {
			return fmt.Errorf("accepted result has an invalid or duplicate outbound intent")
		}
		ids[x.ID] = true
	}
	return nil
}

func (t *Tenant) installAcceptedIntents(intents []platform.Effect) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	for _, planned := range intents {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}

func (d *stagedDecision) recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	view := &Tenant{apps: slices.Clone(d.tenant.apps), appIndex: d.tenant.appIndex, directory: d.directoryView()}
	for i, app := range view.apps {
		if app.Manifest().ID == controlowner.PlatformApp {
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
		console, _ := d.tenant.app(controlowner.PlatformApp).(platform.IdentityProfile)
		if private, ok := d.states[controlowner.PlatformApp].(platform.IdentityProfile); ok {
			console = private
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
	if len(d.notices) > runtimekernel.NoticesKept {
		d.notices = d.notices[len(d.notices)-runtimekernel.NoticesKept:]
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

func (d *stagedDecision) savedNotices() *acceptedformat.Notices {
	if !d.noticeChanged {
		return nil
	}
	return &acceptedformat.Notices{Base: d.noticeBase, Seq: d.noticeSeq, Before: d.noticeBefore,
		Notices: slices.Clone(d.notices), Effects: slices.Clone(d.noticeEffects)}
}

func (t *Tenant) validateAcceptedNotices(n *acceptedformat.Notices) error {
	if n == nil {
		return nil
	}
	current, seq := t.notices.State()
	before, err := platform.CanonicalDigest(current)
	if err != nil || n.Base != seq || n.Before != before || n.Seq < n.Base || len(n.Notices) > runtimekernel.NoticesKept {
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
			if n.Seq-n.Base > runtimekernel.NoticesKept {
				want = n.Seq - runtimekernel.NoticesKept + added
			}
			if x.ID != fmt.Sprintf("n-%d", want) {
				return fmt.Errorf("notification result numbering differs")
			}
			expected = append(expected, x)
		}
	}
	if added != min(n.Seq-n.Base, runtimekernel.NoticesKept) {
		return fmt.Errorf("notification result omits an allocated notice")
	}
	if len(expected) > runtimekernel.NoticesKept {
		expected = expected[len(expected)-runtimekernel.NoticesKept:]
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

func (t *Tenant) applyAcceptedNotices(n *acceptedformat.Notices) {
	if n == nil {
		return
	}
	t.notices.Restore(slices.Clone(n.Notices), n.Seq)
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	t.acted += n.Seq - n.Base
	for _, planned := range n.Effects {
		t.outbound = append(t.outbound, &effect{Effect: planned, span: t.current()})
	}
	t.trimEffects()
}
