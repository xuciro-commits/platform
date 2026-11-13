package kernel

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"sync"
	"time"

	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

const NoticesKept = 2000

type NoticeBoard struct {
	mu  sync.Mutex
	all []platform.Notification
	seq int
}

func (b *NoticeBoard) State() ([]platform.Notification, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.all), b.seq
}

func (b *NoticeBoard) Restore(all []platform.Notification, seq int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all, b.seq = all, seq
}

func (b *NoticeBoard) Post(n platform.Notification, member string) (platform.Notification, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n.Key != "" && slices.ContainsFunc(b.all, func(x platform.Notification) bool { return x.Member == member && x.App == n.App && x.Key == n.Key }) {
		return n, false
	}
	b.seq++
	n.ID, n.Member = fmt.Sprintf("n-%d", b.seq), member
	b.all = append(b.all, n)
	if len(b.all) > NoticesKept {
		b.all = b.all[len(b.all)-NoticesKept:]
	}
	return n, true
}

func (b *NoticeBoard) ForMember(member string) []platform.Notification {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []platform.Notification{}
	for i := len(b.all) - 1; i >= 0; i-- {
		if b.all[i].Member == member {
			out = append(out, b.all[i])
		}
	}
	return out
}

func (b *NoticeBoard) Owner(id string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := slices.IndexFunc(b.all, func(x platform.Notification) bool { return x.ID == id })
	if i < 0 {
		return "", false
	}
	return b.all[i].Member, true
}

func (b *NoticeBoard) MarkRead(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i := slices.IndexFunc(b.all, func(x platform.Notification) bool { return x.ID == id }); i >= 0 {
		b.all[i].Read = true
	}
}

func (b *NoticeBoard) MarkKeysRead(app string, keys []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, n := range b.all {
		if n.App == app && slices.Contains(keys, n.Key) {
			b.all[i].Read = true
		}
	}
}

func (b *NoticeBoard) Decide(c platform.Caller, s *pb.Submission, _ time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	owner, found := b.Owner(id)
	if !found {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if owner != c.ID {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return func(*pb.ChangeRecord) { b.MarkRead(id) }, nil
}

func (b *NoticeBoard) Apply(n *acceptedformat.Notices) (int, []platform.Effect) {
	if n == nil {
		return 0, nil
	}
	b.Restore(slices.Clone(n.Notices), n.Seq)
	return n.Seq - n.Base, slices.Clone(n.Effects)
}

type NoticeDraft struct {
	notices   []platform.Notification
	base, seq int
	before    string
	effects   []platform.Effect
}

func (b *NoticeBoard) Draft() *NoticeDraft {
	all, seq := b.State()
	return &NoticeDraft{notices: all, base: seq, seq: seq}
}

func (d *NoticeDraft) touch() {
	if d.before == "" {
		d.before, _ = platform.CanonicalDigest(d.notices)
	}
}

func (d *NoticeDraft) Changed() bool { return d.before != "" }

func (d *NoticeDraft) Clone() *NoticeDraft {
	fork := *d
	fork.notices, fork.effects = slices.Clone(d.notices), slices.Clone(d.effects)
	return &fork
}

func (d *NoticeDraft) Add(n platform.Notification, read bool) (platform.Notification, bool) {
	if n.Key != "" && slices.ContainsFunc(d.notices, func(x platform.Notification) bool {
		return x.Member == n.Member && x.App == n.App && x.Key == n.Key
	}) {
		return n, false
	}
	d.touch()
	d.seq++
	n.ID, n.Read = fmt.Sprintf("n-%d", d.seq), read
	d.notices = append(d.notices, n)
	if len(d.notices) > NoticesKept {
		d.notices = d.notices[len(d.notices)-NoticesKept:]
	}
	return n, true
}

func (d *NoticeDraft) Mail(effects ...platform.Effect) { d.effects = append(d.effects, effects...) }

func (d *NoticeDraft) Owner(id string) (string, bool) {
	i := slices.IndexFunc(d.notices, func(x platform.Notification) bool { return x.ID == id })
	if i < 0 {
		return "", false
	}
	return d.notices[i].Member, true
}

func (d *NoticeDraft) MarkRead(id string) {
	if i := slices.IndexFunc(d.notices, func(x platform.Notification) bool { return x.ID == id }); i >= 0 {
		d.touch()
		d.notices[i].Read = true
	}
}

func (d *NoticeDraft) Seen(app string, keys []string) {
	for i, n := range d.notices {
		if n.App == app && slices.Contains(keys, n.Key) && !n.Read {
			d.touch()
			d.notices[i].Read = true
		}
	}
}

func (d *NoticeDraft) Saved() *acceptedformat.Notices {
	if !d.Changed() {
		return nil
	}
	return &acceptedformat.Notices{Base: d.base, Seq: d.seq, Before: d.before,
		Notices: slices.Clone(d.notices), Effects: slices.Clone(d.effects)}
}

func (b *NoticeBoard) Check(n *acceptedformat.Notices) error {
	if n == nil {
		return nil
	}
	current, seq := b.State()
	before, err := platform.CanonicalDigest(current)
	if err != nil || n.Base != seq || n.Before != before || n.Seq < n.Base || len(n.Notices) > NoticesKept {
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
			if n.Seq-n.Base > NoticesKept {
				want = n.Seq - NoticesKept + added
			}
			if x.ID != fmt.Sprintf("n-%d", want) {
				return fmt.Errorf("notification result numbering differs")
			}
			expected = append(expected, x)
		}
	}
	if added != min(n.Seq-n.Base, NoticesKept) {
		return fmt.Errorf("notification result omits an allocated notice")
	}
	if len(expected) > NoticesKept {
		expected = expected[len(expected)-NoticesKept:]
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
