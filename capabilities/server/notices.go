package platformserver

import (
	"fmt"
	"slices"
	"sync"

	"platformserver/platform"
)

// noticeBoard is the component that owns the tenant's notifications: numbered
// in order ("n-<seq>"), the latest noticesKept kept, read marks per member.
// Its own lock: reads run inside other apps' submissions and the host console
// marks keys read from outside any decision.
type noticeBoard struct {
	mu  sync.Mutex
	all []platform.Notification
	seq int
}

// state is the board as it stands, for snapshots, staged decisions and the
// accepted-notices predecessor check.
func (b *noticeBoard) state() ([]platform.Notification, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.all), b.seq
}

func (b *noticeBoard) restore(all []platform.Notification, seq int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all, b.seq = all, seq
}

// post numbers and keeps one notice for member unless one with the same key
// from the same app is already there; it answers the kept notice.
func (b *noticeBoard) post(n platform.Notification, member string) (platform.Notification, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n.Key != "" && slices.ContainsFunc(b.all, func(x platform.Notification) bool { return x.Member == member && x.App == n.App && x.Key == n.Key }) {
		return n, false
	}
	b.seq++
	n.ID, n.Member = fmt.Sprintf("n-%d", b.seq), member
	b.all = append(b.all, n)
	if len(b.all) > noticesKept {
		b.all = b.all[len(b.all)-noticesKept:]
	}
	return n, true
}

// forMember is member's notices, newest first.
func (b *noticeBoard) forMember(member string) []platform.Notification {
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

// owner is who a notice belongs to; "" when there is no such notice.
func (b *noticeBoard) owner(id string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	i := slices.IndexFunc(b.all, func(x platform.Notification) bool { return x.ID == id })
	if i < 0 {
		return "", false
	}
	return b.all[i].Member, true
}

// markRead marks the notice id read, if it is still kept.
func (b *noticeBoard) markRead(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i := slices.IndexFunc(b.all, func(x platform.Notification) bool { return x.ID == id }); i >= 0 {
		b.all[i].Read = true
	}
}

// markKeysRead marks an app's keyed notices read (the app settled what they announced).
func (b *noticeBoard) markKeysRead(app string, keys []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, n := range b.all {
		if n.App == app && slices.Contains(keys, n.Key) {
			b.all[i].Read = true
		}
	}
}
