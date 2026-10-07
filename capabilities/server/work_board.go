package platformserver

import (
	"fmt"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// workBoard holds the tenant's owned work: each subscriber's queue of event
// deliveries (head first), the deliveries that gave up, and the apps' jobs
// (from the manifests; their runs from the journal). Protected by the
// tenant's opsMu, which the runner and the reads share.
type workBoard struct {
	queues map[string][]*Task // subscriber → its deliveries, head first
	failed []*Task
	jobs   []*Task
}

func newWorkBoard() workBoard { return workBoard{queues: map[string][]*Task{}} }

func (b *workBoard) addJob(task *Task)             { b.jobs = append(b.jobs, task) }
func (b *workBoard) queue(app string, task *Task)  { b.queues[app] = append(b.queues[app], task) }
func (b *workBoard) queued(app string) []*Task     { return b.queues[app] }
func (b *workBoard) failedCount() int              { return len(b.failed) }
func (b *workBoard) job(id string) *Task           { return findTask(b.jobs, id) }
func (b *workBoard) delivery(app, id string) *Task { return findTask(b.queues[app], id) }
func (b *workBoard) knows(id string) bool {
	return findTask(b.failed, id) != nil || findTask(b.jobs, id) != nil
}

func findTask(xs []*Task, id string) *Task {
	if i := slices.IndexFunc(xs, func(x *Task) bool { return x.ID == id }); i >= 0 {
		return xs[i]
	}
	return nil
}

// anyDelivery finds a queued delivery in any subscriber's queue.
func (b *workBoard) anyDelivery(id string) *Task {
	for _, q := range b.queues {
		if t := findTask(q, id); t != nil {
			return t
		}
	}
	return nil
}

// settled takes a done or failed delivery out of its queue; a failed one waits for a retry.
func (b *workBoard) settled(task *Task) {
	b.queues[task.App] = slices.DeleteFunc(b.queues[task.App], func(x *Task) bool { return x == task })
	if task.State == "failed" {
		b.failed = append(b.failed, task)
	}
}

// retry queues a failed delivery again at the head of its queue, or makes a job due.
func (b *workBoard) retry(id string, now time.Time) bool {
	if i := slices.IndexFunc(b.failed, func(x *Task) bool { return x.ID == id }); i >= 0 {
		task := b.failed[i]
		b.failed = slices.Delete(b.failed, i, i+1)
		task.State, task.Due, task.since = "queued", now, task.Attempts
		b.queues[task.App] = append([]*Task{task}, b.queues[task.App]...)
		return true
	}
	if j := findTask(b.jobs, id); j != nil {
		j.Due = now
		return true
	}
	return false
}

// all lists jobs, then each app's queue in app order, then the failed ones.
func (b *workBoard) all(apps []platform.App) []Task {
	out := []Task{}
	for _, j := range b.jobs {
		out = append(out, *j)
	}
	for _, a := range apps {
		for _, q := range b.queues[a.Manifest().ID] {
			out = append(out, *q)
		}
	}
	for _, f := range b.failed {
		out = append(out, *f)
	}
	return out
}

func (b *workBoard) snapshot(s *tenantState) error {
	tasks := map[string]*Task{}
	for app, queue := range b.queues {
		for _, x := range queue {
			s.Queues[app], tasks[x.ID] = append(s.Queues[app], x.ID), x
		}
	}
	for _, x := range b.failed {
		s.Failed, tasks[x.ID] = append(s.Failed, x.ID), x
	}
	for _, id := range slices.Sorted(maps.Keys(tasks)) {
		x := tasks[id]
		state := taskState{Task: *x, Since: x.since}
		if x.event != nil {
			var err error
			state.EventApp, state.Hops, state.Changed = x.event.App, x.event.hops, x.event.Changed
			state.Plan = x.event.plan
			if state.Event, err = platform.Protos([]*pb.ChangeRecord{x.event.Record}); err != nil {
				return err
			}
		}
		s.Tasks = append(s.Tasks, state)
	}
	for _, x := range b.jobs {
		s.Jobs = append(s.Jobs, *x)
	}
	return nil
}

func (b *workBoard) restore(tenant string, s *tenantState) error {
	tasks := map[string]*Task{}
	for _, x := range s.Tasks {
		task := x.Task
		task.since = x.Since
		if x.Event != nil {
			records, err := platform.Unprotos[*pb.ChangeRecord](x.Event)
			if err != nil || len(records) != 1 {
				return fmt.Errorf("tenant %s: task %s: bad event", tenant, x.ID)
			}
			task.event = &caused{Event: platform.Event{App: x.EventApp, Record: records[0], Changed: x.Changed}, hops: x.Hops, plan: x.Plan}
		}
		tasks[x.ID] = &task
	}
	b.queues, b.failed = map[string][]*Task{}, nil
	for app, ids := range s.Queues {
		for _, id := range ids {
			b.queues[app] = append(b.queues[app], tasks[id])
		}
	}
	for _, id := range s.Failed {
		b.failed = append(b.failed, tasks[id])
	}
	for _, saved := range s.Jobs { // the jobs come from the manifests; their runs from the snapshot
		if j := findTask(b.jobs, saved.ID); j != nil {
			job := j.job
			*j = saved
			j.job = job
		}
	}
	return nil
}
