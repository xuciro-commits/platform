package kernel

import (
	"context"
	"go.opentelemetry.io/otel/trace"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type LaneState struct {
	Holder  string
	Since   time.Time
	Waiters int64
}

type LaneObserver func(holder string, waited, held time.Duration)

type Lane struct {
	once    sync.Once
	slot    chan struct{}
	enter   func(context.Context)
	leave   func()
	observe atomic.Pointer[LaneObserver]
	waiters atomic.Int64
	hold    atomic.Pointer[laneHold]
}

type laneHold struct {
	holder string
	since  time.Time
	waited time.Duration
}

func (l *Lane) BindReadScope(enter func(context.Context), leave func()) {
	l.enter, l.leave = enter, leave
}

func (l *Lane) Observe(observer LaneObserver) { l.observe.Store(&observer) }

func (l *Lane) Lock() { _ = l.Hold(context.Background(), callerName(2)) }

func (l *Lane) Hold(ctx context.Context, holder string) error {
	if holder == "" {
		holder = callerName(2)
	}
	return l.acquire(ctx, ctx, holder)
}

func (l *Lane) HoldForCommit(ctx context.Context, holder string) error {
	trace.SpanFromContext(ctx).AddEvent("lane.wait")
	scope := ctx
	if ctx.Done() != nil {
		scope = context.WithoutCancel(ctx)
	}
	return l.acquire(ctx, scope, holder)
}

func (l *Lane) State() LaneState {
	state := LaneState{Waiters: l.waiters.Load()}
	if h := l.hold.Load(); h != nil {
		state.Holder, state.Since = h.holder, h.since
	}
	return state
}

func (l *Lane) acquire(ctx, scope context.Context, holder string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.once.Do(func() { l.slot = make(chan struct{}, 1) })
	started := time.Now()
	select {
	case l.slot <- struct{}{}:
	default:
		l.waiters.Add(1)
		select {
		case l.slot <- struct{}{}:
			l.waiters.Add(-1)
		case <-ctx.Done():
			l.waiters.Add(-1)
			return ctx.Err()
		}
	}
	now := time.Now()
	l.hold.Store(&laneHold{holder: holder, since: now, waited: now.Sub(started)})
	if l.enter != nil {
		l.enter(scope)
	}
	return nil
}

func (l *Lane) Unlock() {
	if l.leave != nil {
		l.leave()
	}
	h := l.hold.Swap(nil)
	select {
	case <-l.slot:
	default:
		panic("kernel: unlock of an unheld lane")
	}
	if observer := l.observe.Load(); h != nil && observer != nil {
		(*observer)(h.holder, h.waited, time.Since(h.since))
	}
}

var receiverMarks = strings.NewReplacer("(*", "", ")", "")

func callerName(skip int) string {
	pc, _, _, ok := runtime.Caller(skip)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}
	name := fn.Name()
	if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
		name = name[slash+1:]
	}
	name = receiverMarks.Replace(name)
	if dot := strings.Index(name, ".func"); dot >= 0 {
		name = name[:dot]
	}
	return name
}
