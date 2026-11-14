package automation

import (
	"fmt"
	"slices"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

var deliveryRetry = platform.Retry{Initial: 2 * time.Second, Max: time.Minute, Attempts: 5}

func RetryOf(a platform.App) platform.Retry {
	if a != nil && a.Manifest().Retry != nil {
		return *a.Manifest().Retry
	}
	return deliveryRetry
}

type EventRoute struct {
	MaxHops    int
	Apps       []platform.App
	Listeners  []string
	Names      func(event platform.Event) []string
	Delivered  func(platform.Delivery)
	Observe    func(event platform.Event, names []string)
	Add        func(effects ...platform.Effect)
	Emit       func(event platform.Event, names []string)
	Exists     func(id string) bool
	Interested func(id string, names []string, event platform.Event) bool
	Deliver    func(subscriber string, e QueuedEvent)
}

func (r EventRoute) Route(e QueuedEvent, now time.Time) {
	s := e.Record.GetSubmission()
	names := append([]string{s.GetSchema().GetName()}, r.Names(e.Event)...)
	stopped := func(id string) {
		r.Delivered(platform.Delivery{At: now, App: e.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: id,
			Outcome: fmt.Sprintf("stopped: more than %d events caused by events", r.MaxHops)})
	}
	if e.Plan != nil {
		for _, id := range e.Plan.Stopped {
			stopped(id)
		}
		names = e.Plan.Names
	}
	if e.Plan == nil || !e.Plan.Observed {
		r.Observe(e.Event, names[1:])
	}
	if e.Plan != nil {
		r.Add(e.Plan.Effects...)
		for _, id := range e.Plan.Subscribers {
			if !r.Exists(id) {
				panic(fmt.Errorf("accepted result requires missing subscriber %s", id))
			}
			r.Deliver(id, e)
		}
		return
	}
	r.Emit(e.Event, names)
	for _, a := range r.Apps {
		if !slices.ContainsFunc(a.Manifest().Subscribes, func(x string) bool { return slices.Contains(names, x) }) {
			continue
		}
		if e.Hops > r.MaxHops {
			stopped(a.Manifest().ID)
			continue
		}
		r.Deliver(a.Manifest().ID, e)
	}
	for _, id := range r.Listeners {
		if id != e.App && r.Interested(id, names, e.Event) {
			r.Deliver(id, e)
		}
	}
}

type Replay struct {
	Board     *WorkBoard
	App       func(string) platform.App
	Caller    func(app string) platform.Caller
	Names     func(event platform.Event) []string
	Retry     func(app string) platform.Retry
	Start     func(id string) uint32
	Finish    func(id string, generation uint32, failed bool)
	SetHops   func(int)
	Delivered func(platform.Delivery)
	Failed    func(title, why, id string)
	Observe   func(source, runID, app, resource, status, title, detail string)
	Enqueue   func()
}

func runStatus(outcome string) string {
	if outcome != "ok" {
		return "failed"
	}
	return "ok"
}

func (r Replay) Attempt(task *Task, now time.Time) string {
	generation := r.Start(task.ID)
	r.SetHops(task.Event.Hops + 1)
	outcome := "ok"
	var err *kernel.Error
	if x, ok := r.App(task.App).(platform.Listener); ok {
		names := append([]string{task.Event.Record.GetSubmission().GetSchema().GetName()}, r.Names(task.Event.Event)...)
		if task.Event.Plan != nil {
			names = task.Event.Plan.Names
		}
		err = x.Listen(r.Caller(task.App), task.Event.Event, names, now)
	} else {
		err = r.App(task.App).(platform.Subscriber).Handle(r.Caller(task.App), task.Event.Event)
	}
	if err != nil {
		outcome = err.Error()
	}
	r.SetHops(0)
	r.Finish(task.ID, generation, outcome != "ok")
	s := task.Event.Record.GetSubmission()
	r.Delivered(platform.Delivery{At: now, App: task.Event.App, Action: s.GetSchema().GetName(), Target: target(s), Subscriber: task.App,
		Outcome: outcome, Attempt: int(generation)})
	r.Board.Lock()
	task.ScheduledTask = task.Settle(int(generation), outcome, r.Retry(task.App), now)
	if task.State == "done" || task.State == "failed" {
		r.Board.Settled(task)
	}
	r.Board.Unlock()
	if task.State == "failed" {
		r.Failed("Delivery to "+task.App+" failed: "+task.Title, outcome, task.ID)
	}
	r.Observe("run", task.Kind+":"+task.App+":"+task.ID, task.App, target(s), runStatus(outcome), "Delivery to "+task.App, outcome)
	r.Enqueue()
	return outcome
}

func (r Replay) Run(task *Task, now time.Time) string {
	generation := r.Start(task.ID)
	outcome := "ok"
	if err := r.App(task.App).(platform.Runner).Run(r.Caller(task.App), task.Job.Name, now); err != nil {
		outcome = err.Error()
	}
	r.Finish(task.ID, generation, outcome != "ok")
	r.Board.Lock()
	task.ScheduledTask = task.Settle(int(generation), outcome, platform.Retry{}, now)
	r.Board.Unlock()
	r.Observe("worker", "job:"+task.App+":"+task.ID, task.App, task.ID, runStatus(outcome), "Scheduled job "+task.Title, outcome)
	r.Enqueue()
	return outcome
}

func (b *WorkBoard) Adopt(from *WorkBoard) {
	from.Lock()
	rotation, jobs := from.Rotation, slices.Clone(from.Jobs)
	from.Unlock()
	b.Lock()
	defer b.Unlock()
	b.Rotation = rotation
	for _, job := range jobs {
		if current := b.Job(job.ID); current != nil {
			current.NextRun = job.NextRun
		}
	}
}

func (b *WorkBoard) Deliver(subscriber string, e QueuedEvent, now time.Time) {
	s := e.Record.GetSubmission()
	b.Lock()
	defer b.Unlock()
	b.Queue(subscriber, &Task{ScheduledTask: platform.ScheduledTask{ID: "delivery:" + subscriber + ":" + e.App + "/" + e.Record.GetChangeId(), Kind: "delivery", App: subscriber,
		Title: s.GetSchema().GetName() + " " + target(s), State: "queued", Due: now.UTC()}, Event: &e})
}

func (b *WorkBoard) Tasks(apps []platform.App) []Task {
	b.Lock()
	defer b.Unlock()
	return b.All(apps)
}

func (b *WorkBoard) Retried(id string, now time.Time) bool {
	b.Lock()
	defer b.Unlock()
	return b.Retry(id, now)
}
