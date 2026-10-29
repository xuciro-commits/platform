package automation

import (
	"fmt"
	"go.opentelemetry.io/otel/trace"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"slices"
	"time"
)

type WorkState struct {
	Queues map[string][]string
	Failed []string
	Tasks  []platform.ScheduledTaskState
	Jobs   []Task
}
type WorkLoop struct {
	Board        *WorkBoard
	Quota        *Quota
	Apps         []platform.App
	Limit        int
	Active       func(string) bool
	Lock, Unlock func()
	Attempt, Run func(*Task, time.Time, bool) string
}

type Task struct {
	platform.ScheduledTask
	Since   int          `json:"-"`
	Event   *QueuedEvent `json:"-"`
	Job     platform.Job `json:"-"`
	NextRun time.Time    `json:"-"`
}

type QueuedEvent struct {
	platform.Event
	Hops int
	Span trace.SpanContext
	Plan *platform.AcceptedEventPlan
}

func StateOfTask(task *Task) (platform.ScheduledTaskState, error) {
	s := platform.ScheduledTaskState{ScheduledTask: task.ScheduledTask, Since: task.Since}
	if task.Event != nil {
		s.EventApp, s.Hops, s.Changed, s.Plan = task.Event.App, task.Event.Hops, slices.Clone(task.Event.Changed), task.Event.Plan
		var err error
		s.Event, err = platform.Protos([]*pb.ChangeRecord{task.Event.Record})
		if err != nil {
			return s, err
		}
	}
	return s, nil
}

type Quota struct {
	used map[string]usedMinute
	turn int
}

type usedMinute struct {
	minute time.Time
	n      int
}

func (q *Quota) Over(app string, limit int, now time.Time) bool {
	if limit <= 0 {
		return false
	}
	u := q.used[app]
	return u.minute.Equal(now.Truncate(time.Minute)) && u.n >= limit
}

func (q *Quota) Spend(app string, now time.Time) {
	if q.used == nil {
		q.used = map[string]usedMinute{}
	}
	u, minute := q.used[app], now.Truncate(time.Minute)
	if !u.minute.Equal(minute) {
		u = usedMinute{minute: minute}
	}
	u.n++
	q.used[app] = u
}

func (q *Quota) Next(n int) { q.turn = (q.turn + 1) % max(n, 1) }

type WorkBoard struct {
	Queues map[string][]*Task
	Failed []*Task
	Jobs   []*Task
}

func NewWorkBoard() WorkBoard { return WorkBoard{Queues: map[string][]*Task{}} }

func (b *WorkBoard) AddJob(task *Task) { b.Jobs = append(b.Jobs, task) }

func (b *WorkBoard) Queue(app string, task *Task) { b.Queues[app] = append(b.Queues[app], task) }

func (b *WorkBoard) Queued(app string) []*Task { return b.Queues[app] }

func (b *WorkBoard) FailedCount() int { return len(b.Failed) }

func (b *WorkBoard) Job(id string) *Task { return findTask(b.Jobs, id) }

func (b *WorkBoard) Delivery(app, id string) *Task { return findTask(b.Queues[app], id) }

func (b *WorkBoard) Knows(id string) bool {
	return findTask(b.Failed, id) != nil || findTask(b.Jobs, id) != nil
}

func findTask(xs []*Task, id string) *Task {
	if i := slices.IndexFunc(xs, func(x *Task) bool { return x.ID == id }); i >= 0 {
		return xs[i]
	}
	return nil
}

func (b *WorkBoard) AnyDelivery(id string) *Task {
	for _, q := range b.Queues {
		if t := findTask(q, id); t != nil {
			return t
		}
	}
	return nil
}

func (b *WorkBoard) Settled(task *Task) {
	b.Queues[task.App] = slices.DeleteFunc(b.Queues[task.App], func(x *Task) bool { return x == task })
	if task.State == "failed" {
		b.Failed = append(b.Failed, task)
	}
}

func (b *WorkBoard) Retry(id string, now time.Time) bool {
	if i := slices.IndexFunc(b.Failed, func(x *Task) bool { return x.ID == id }); i >= 0 {
		task := b.Failed[i]
		b.Failed = slices.Delete(b.Failed, i, i+1)
		task.State, task.Due, task.Since = "queued", now, task.Attempts
		b.Queues[task.App] = append([]*Task{task}, b.Queues[task.App]...)
		return true
	}
	if j := findTask(b.Jobs, id); j != nil {
		j.Due = now
		j.NextRun = time.Time{}
		return true
	}
	return false
}

func (b *WorkBoard) All(apps []platform.App) []Task {
	out := []Task{}
	for _, j := range b.Jobs {
		job := *j
		if job.NextRun.After(job.Due) {
			job.Due = job.NextRun
		}
		out = append(out, job)
	}
	for _, a := range apps {
		for _, q := range b.Queues[a.Manifest().ID] {
			out = append(out, *q)
		}
	}
	for _, f := range b.Failed {
		out = append(out, *f)
	}
	return out
}

func (b *WorkBoard) Snapshot() (WorkState, error) {
	s := WorkState{Queues: map[string][]string{}}
	tasks := map[string]*Task{}
	for app, queue := range b.Queues {
		for _, x := range queue {
			s.Queues[app], tasks[x.ID] = append(s.Queues[app], x.ID), x
		}
	}
	for _, x := range b.Failed {
		s.Failed, tasks[x.ID] = append(s.Failed, x.ID), x
	}
	for _, id := range slices.Sorted(maps.Keys(tasks)) {
		state, err := StateOfTask(tasks[id])
		if err != nil {
			return s, err
		}
		s.Tasks = append(s.Tasks, state)
	}
	for _, x := range b.Jobs {
		s.Jobs = append(s.Jobs, *x)
	}
	return s, nil
}

func (b *WorkBoard) Restore(tenant string, s WorkState) error {
	tasks := map[string]*Task{}
	for _, x := range s.Tasks {
		task := Task{ScheduledTask: x.ScheduledTask}
		task.Since = x.Since
		if x.Event != nil {
			records, err := platform.Unprotos[*pb.ChangeRecord](x.Event)
			if err != nil || len(records) != 1 {
				return fmt.Errorf("tenant %s: task %s: bad event", tenant, x.ID)
			}
			task.Event = &QueuedEvent{Event: platform.Event{App: x.EventApp, Record: records[0], Changed: x.Changed}, Hops: x.Hops, Plan: x.Plan}
		}
		tasks[x.ID] = &task
	}
	b.Queues, b.Failed = map[string][]*Task{}, nil
	for app, ids := range s.Queues {
		for _, id := range ids {
			b.Queues[app] = append(b.Queues[app], tasks[id])
		}
	}
	for _, id := range s.Failed {
		b.Failed = append(b.Failed, tasks[id])
	}
	for _, saved := range s.Jobs {
		if j := findTask(b.Jobs, saved.ID); j != nil {
			job := j.Job
			*j = saved
			j.Job = job
		}
	}
	return nil
}

func Ready(q []*Task, now time.Time) *Task {
	held := map[string]bool{}
	for _, x := range q {
		k := x.Event.Record.GetSubmission().GetTarget().GetType() + "/" + x.Event.Record.GetSubmission().GetTarget().GetId()
		if !held[k] && !x.Due.After(now) {
			return x
		}
		held[k] = true
	}
	return nil
}

func (task *Task) DueAt(now time.Time) bool { return !task.Due.After(now) && !task.NextRun.After(now) }

func (s WorkLoop) Round(now time.Time, budget int) bool {
	took, n := 0, len(s.Apps)
	for progressed := true; progressed && took < budget; {
		progressed = false
		for i := range n {
			if took >= budget {
				break
			}
			a := s.Apps[(s.Quota.turn+i)%n].Manifest().ID
			if !s.Active(a) {
				continue
			}
			if s.Quota.Over(a, s.Limit, now) {
				continue
			}
			s.Lock()
			next := Ready(s.Board.Queued(a), now)
			s.Unlock()
			if next == nil {
				continue
			}
			s.Attempt(next, now, false)
			s.Quota.Spend(a, now)
			took, progressed = took+1, true
		}
	}
	s.Quota.Next(n)
	for _, j := range s.Board.Jobs {
		if s.Active(j.App) && j.DueAt(now) && took < budget && !s.Quota.Over(j.App, s.Limit, now) {
			s.Run(j, now, false)
			s.Quota.Spend(j.App, now)
			took++
		}
	}
	return s.Pending(now)
}

func (s WorkLoop) Pending(now time.Time) bool {
	s.Lock()
	defer s.Unlock()
	for app, q := range s.Board.Queues {
		if s.Active(app) && Ready(q, now) != nil && !s.Quota.Over(app, s.Limit, now) {
			return true
		}
	}
	return slices.ContainsFunc(s.Board.Jobs, func(j *Task) bool { return s.Active(j.App) && j.DueAt(now) && !s.Quota.Over(j.App, s.Limit, now) })
}

func (s WorkLoop) Deferred(now time.Time) []string {
	s.Lock()
	defer s.Unlock()
	var out []string
	for _, a := range s.Apps {
		id := a.Manifest().ID
		if !s.Active(id) {
			continue
		}
		if s.Quota.Over(id, s.Limit, now) && (Ready(s.Board.Queued(id), now) != nil || slices.ContainsFunc(s.Board.Jobs, func(j *Task) bool { return j.App == id && j.DueAt(now) })) {
			out = append(out, id)
		}
	}
	return out
}

func (task *Task) RestoreOutcome(saved Task) {
	event, job, since := task.Event, task.Job, task.Since
	*task = saved
	task.Event, task.Job, task.Since = event, job, since
}
