package kernel

import (
	"context"
	"sync"
)

// Lane is the original non-reentrant tenant writer lock. A coordinated
// operation owns one fixed storage view until it leaves the lane.
type Lane struct {
	lock  sync.Mutex
	enter func(context.Context)
	leave func()
}

// BindReadScope connects the tenant's original storage view during composition.
func (l *Lane) BindReadScope(enter func(context.Context), leave func()) {
	l.enter, l.leave = enter, leave
}

func (l *Lane) Lock() { l.LockContext(context.Background()) }
func (l *Lane) LockContext(ctx context.Context) {
	l.lock.Lock()
	if l.enter != nil {
		l.enter(ctx)
	}
}
func (l *Lane) Unlock() {
	if l.leave != nil {
		l.leave()
	}
	l.lock.Unlock()
}
