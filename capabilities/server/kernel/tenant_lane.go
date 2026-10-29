package kernel

import (
	"context"
	"sync"
)

type Lane struct {
	lock  sync.Mutex
	enter func(context.Context)
	leave func()
}

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
