package automation

import (
	"fmt"
	"go.opentelemetry.io/otel/trace"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"sync"
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
	Apps         []platform.App
	Active       func(string) bool
	Attempt, Run func(*Task, time.Time, bool) string
}

type Worker struct {
	Board  *WorkBoard
	Apps   []platform.App
	Active func(string) bool
	Accept func(*Task, time.Time) string
	Replay func(time.Time) Replay
}

func (w Worker) Loop() WorkLoop {
	return WorkLoop{Board: w.Board, Apps: w.Apps, Active: w.Active, Attempt: w.Attempt, Run: w.Run}
}

func (w Worker) Attempt(task *Task, now time.Time, replaying bool) string {
	if !replaying {
		return w.Accept(task, now)
	}
	return w.Replay(now).Attempt(task, now)
}

func (w Worker) Run(task *Task, now time.Time, replaying bool) string {
	if replaying {
		return w.Replay(now).Run(task, now)
	}
	outcome := w.Accept(task, now)
	w.Board.Lock()
	task.NextRun = now.Add(task.Job.Every)
	w.Board.Unlock()
	return outcome
}

type Task struct {
	platform.ScheduledTask
	Status  string       `json:"status,omitempty" source:"record"`
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

type Rotation struct{ turn int }

func (r *Rotation) Next(n int) { r.turn = (r.turn + 1) % max(n, 1) }

type WorkBoard struct {
	sync.Mutex
	Queues   map[string][]*Task
	Failed   []*Task
	Jobs     []*Task
	Rotation Rotation
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

func (b *WorkBoard) Decide(_ platform.Caller, s *pb.Submission, now time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	b.Lock()
	known := b.Knows(id)
	b.Unlock()
	if !known {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return func(*pb.ChangeRecord) { b.Retried(id, now) }, nil
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
	for i := range out {
		task := &out[i]
		task.Status = "unknown"
		if task.Kind == "job" {
			switch {
			case task.Last.IsZero():
				task.Status = "pending"
			case task.Error != "":
				task.Status = "failed"
			default:
				task.Status = "ok"
			}
		} else {
			switch task.State {
			case "queued", "retrying":
				task.Status = "pending"
			case "running":
				task.Status = "running"
			case "failed":
				task.Status = "failed"
			case "done":
				task.Status = "ok"
			}
		}
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

func (b *WorkBoard) Install(tenant string, s WorkState, known func(app string) bool) error {
	tasks := map[string]bool{}
	for _, task := range s.Tasks {
		if task.ID == "" || tasks[task.ID] || !known(task.App) {
			return fmt.Errorf("control work identity differs")
		}
		tasks[task.ID] = true
	}
	indexed := map[string]bool{}
	for app, queue := range s.Queues {
		if !known(app) {
			return fmt.Errorf("control queue owner missing")
		}
		for _, id := range queue {
			if !tasks[id] || indexed[id] {
				return fmt.Errorf("control queue references missing or duplicate work")
			}
			indexed[id] = true
		}
	}
	for _, id := range s.Failed {
		if !tasks[id] || indexed[id] {
			return fmt.Errorf("control failed work identity differs")
		}
		indexed[id] = true
	}
	if len(indexed) != len(tasks) {
		return fmt.Errorf("control work is unassigned")
	}
	old := map[string]*Task{}
	for _, queue := range b.Queues {
		for _, task := range queue {
			old[task.ID] = task
		}
	}
	for _, task := range b.Failed {
		old[task.ID] = task
	}
	if err := b.Restore(tenant, s); err != nil {
		return err
	}
	reuse := func(task *Task) *Task {
		if prior := old[task.ID]; prior != nil {
			prior.RestoreOutcome(*task)
			prior.Since = task.Since
			return prior
		}
		return task
	}
	for app, queue := range b.Queues {
		for i, task := range queue {
			b.Queues[app][i] = reuse(task)
		}
	}
	for i, task := range b.Failed {
		b.Failed[i] = reuse(task)
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

func (s WorkLoop) Round(now time.Time, budget int) (bool, int) {
	took, n := 0, len(s.Apps)
	for progressed := true; progressed && took < budget; {
		progressed = false
		for i := range n {
			if took >= budget {
				break
			}
			s.Board.Lock()
			a := s.Apps[(s.Board.Rotation.turn+i)%n].Manifest().ID
			s.Board.Unlock()
			if !s.Active(a) {
				continue
			}
			s.Board.Lock()
			next := Ready(s.Board.Queued(a), now)
			s.Board.Unlock()
			if next == nil {
				continue
			}
			s.Attempt(next, now, false)
			took, progressed = took+1, true
		}
	}
	for _, j := range s.Board.Jobs {
		if s.Active(j.App) && j.DueAt(now) && took < budget {
			s.Run(j, now, false)
			took++
		}
	}
	if took > 0 {
		s.Board.Lock()
		s.Board.Rotation.Next(n)
		s.Board.Unlock()
	}
	return s.Pending(now), took
}

func (s WorkLoop) Pending(now time.Time) bool {
	s.Board.Lock()
	defer s.Board.Unlock()
	for app, q := range s.Board.Queues {
		if s.Active(app) && Ready(q, now) != nil {
			return true
		}
	}
	return slices.ContainsFunc(s.Board.Jobs, func(j *Task) bool { return s.Active(j.App) && j.DueAt(now) })
}

func (task *Task) RestoreOutcome(saved Task) {
	event, job, since := task.Event, task.Job, task.Since
	*task = saved
	task.Event, task.Job, task.Since = event, job, since
}

func (t *Task) Settle(attempts int, outcome string, retry platform.Retry, now time.Time) platform.ScheduledTask {
	after := t.ScheduledTask
	after.Attempts, after.Last, after.Error = attempts, now, ""
	if t.Kind == "job" {
		after.Due = now.Add(t.Job.Every)
		if outcome != "ok" {
			after.Error = outcome
		}
		return after
	}
	switch {
	case outcome == "ok":
		after.State = "done"
	case attempts-t.Since >= retry.Attempts:
		after.State, after.Error = "failed", outcome
	default:
		after.State, after.Error = "retrying", outcome
		after.Due = now.Add(retry.After(attempts - t.Since))
	}
	return after
}
