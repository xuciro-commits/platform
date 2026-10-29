package kernel

import (
	"fmt"
	"slices"
	"sync"

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
