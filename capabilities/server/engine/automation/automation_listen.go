package automation

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (f *Flows) Interested(names []string, e platform.Event) bool {
	for _, versions := range f.defs {
		if d := versions[len(versions)-1]; slices.ContainsFunc(d.Start.On, func(on string) bool { return slices.Contains(names, on) }) {
			return true
		}
		if d := versions[len(versions)-1]; d.Start.Type != "" && slices.ContainsFunc(e.Changed, func(ref string) bool { return strings.HasPrefix(ref, d.Start.Type+"/") }) {
			return true
		}
		for _, d := range versions {
			for _, s := range d.Steps {
				if s.Wait != nil && slices.Contains(names, s.Wait.On) || s.Ask != nil && s.Ask.On != "" && slices.Contains(names, s.Ask.On) {
					return true
				}
			}
		}
	}
	s := e.Record.GetSubmission()
	return s.GetSchema().GetName() == "work.task.complete" && e.App == platform.WorkApp
}

func (f *Flows) Listen(c platform.Caller, e platform.Event, names []string, now time.Time) *kernel.Error {
	s := e.Record.GetSubmission()
	for _, id := range slices.Sorted(maps.Keys(f.defs)) {
		latest, _ := f.latest(id)
		if latest.Start.Type != "" {
			for _, ref := range e.Changed {
				key, ok := strings.CutPrefix(ref, latest.Start.Type+"/")
				if !ok {
					continue
				}
				if _, started := platform.Get[FlowInstance](c, id+":"+key); started {
					continue
				}
				record, held := f.host.Record(ref)
				if !held || !latest.Start.When(f.host.Automation(c, latest.app), record) {
					continue
				}
				d, err := f.version(c, id)
				if err != nil {
					return err
				}
				if err := f.start(c, d, key, nil, cmp.Or(d.Start.OnBehalf, s.GetPrincipalId()), &e, "", now); err != nil {
					return err
				}
			}
			continue
		}
		if latest.Start.Manual {
			continue
		}
		if !slices.ContainsFunc(latest.Start.On, func(on string) bool { return slices.Contains(names, on) }) {
			continue
		}
		key, data, ok := latest.Start.Begin(f.host.Automation(c, latest.app), e)
		if !ok {
			continue
		}
		d, err := f.version(c, id)
		if err != nil {
			return err
		}
		if err := f.start(c, d, key, data, s.GetPrincipalId(), &e, "", now); err != nil {
			return err
		}
	}
	running, readErr := f.running(c)
	if readErr != nil {
		return readErr
	}
	for _, x := range running {
		d := f.def(x.Flow, x.Version)

		if d == nil {
			continue
		}
		run := f.run(&x)
		run.Now = now
		for i := range x.Tokens {
			tok := &x.Tokens[i]
			run.Outputs = maps.Clone(tok.Outputs)
			run.Frames = slices.Clone(tok.Frames)
			step := d.steps[tok.Step]
			app := f.host.Automation(c, d.app)
			switch {
			case tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.On != "" && slices.Contains(names, step.Wait.On) && step.Wait.Match(app, run, e):
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.event = &e
					ss.trace(in, tok.Step, "event", s.GetSchema().GetName()+" "+target(s), s.GetPrincipalId())
					ss.next(in, tok.ID, "")
				}); err != nil {
					return err
				}
			case tok.Waits == "ask" && s.GetSchema().GetName() == "work.task.complete" && s.GetTarget().GetId() == tok.Task:
				task, _ := platform.Get[platform.WorkTask](f.host.Automation(c, platform.WorkApp), tok.Task)
				answer := cmp.Or(task.Answer, "done")
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					in.Answer = answer
					ss.trace(in, tok.Step, "answered", answer, task.Assignee)
					if step != nil && step.Ask != nil && len(step.Ask.Answers) > 0 {
						ss.review(in, step, map[bool]string{true: "accepted", false: "corrected"}[answer == step.Ask.Answers[0]], task.Assignee, answer)
					}
					ss.next(in, tok.ID, "")
				}); err != nil {
					return err
				}
			case tok.Waits == "ask" && step != nil && step.Ask != nil && step.Ask.On != "" && slices.Contains(names, step.Ask.On) && step.Ask.Match(app, run, e):
				if err := f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.close = append(ss.close, tok.Task)
					ss.event, in.Answer = &e, "event"
					ss.review(in, step, "bypassed", s.GetPrincipalId(), s.GetSchema().GetName()+" "+target(s))
					ss.trace(in, tok.Step, "event", s.GetSchema().GetName()+" "+target(s)+" closed the task", s.GetPrincipalId())
					ss.next(in, tok.ID, "")
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f *Flows) Run(c platform.Caller, _ string, now time.Time) *kernel.Error {
	if err := f.tick(c, now); err != nil {
		return err
	}
	running, readErr := f.running(c)
	if readErr != nil {
		return readErr
	}
	for _, x := range running {
		d := f.def(x.Flow, x.Version)
		if d == nil {
			continue
		}
		run := f.run(&x)
		run.Now = now
		for _, tok := range x.Tokens {
			run.Outputs = maps.Clone(tok.Outputs)
			run.Frames = slices.Clone(tok.Frames)
			step := d.steps[tok.Step]
			if d.Continuous != nil && d.Continuous.Entry != "" && tok.Step == d.Continuous.Entry && tok.Waits == "retry" {
				continue
			}
			due := !tok.Due.IsZero() && !tok.Due.After(now)
			holds := tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.Until != nil && step.Wait.Until(f.host.Automation(c, d.app), run)
			if tok.Waits == "operation" || tok.Waits == "invocation" || tok.Waits == "approval" {
				if err := f.resumeInvocation(c, x.ID, tok.ID, now); err != nil {
					return err
				}
				continue
			}
			if !due && !holds {
				continue
			}
			var err *kernel.Error
			switch {
			case holds:
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.trace(in, tok.Step, "condition", "it holds", "")
					ss.next(in, tok.ID, "")
				})
			case tok.Waits == "retry" || tok.Waits == "undo" || tok.Waits == "yield":
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ready := ss.token(in, tok.ID)
					ready.Waits, ready.Due = "ready", time.Time{}
				})
			case tok.Waits == "wait" && step != nil && step.Wait != nil && step.Wait.At != nil && due:
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					ss.trace(in, tok.Step, "time", "reached", "")
					ss.next(in, tok.ID, "")
				})
			case tok.Waits == "wait" || tok.Waits == "ask" || tok.Waits == "call" || tok.Waits == "agent":
				err = f.step(c, x.ID, now, func(ss *session, in *FlowInstance) {
					if tok.Task != "" {
						ss.close = append(ss.close, tok.Task)
					}
					ss.trace(in, tok.Step, "timeout", map[bool]string{true: fmt.Sprintf("%d working days", step.WorkingDays), false: step.Timeout.String()}[step.WorkingDays > 0], "")
					ss.next(in, tok.ID, step.OnTimeout)
				})
			}
			if err != nil {
				return err
			}
			break
		}
	}
	_, retentionErr := f.retainExpiredDeadLetters(c, now)
	return retentionErr
}

func (f *Flows) run(x *FlowInstance) *platform.Run {
	return &platform.Run{ID: x.ID, Flow: x.Flow, Version: x.Version, Key: x.Key, OnBehalf: x.OnBehalf, Release: x.Release, Sequence: x.Seq, Sources: slices.Clone(x.Sources), Outputs: maps.Clone(x.Outputs), Data: json.RawMessage(cmp.Or(x.Data, "null")), Answer: x.Answer}
}

func (f *Flows) OperationEnded(c platform.Caller, call string, now time.Time) *kernel.Error {
	c = f.host.Automation(c, ID)
	running, err := f.running(c)
	if err != nil {
		return err
	}
	for _, x := range running {
		for _, tok := range x.Tokens {
			if tok.Waits == "operation" && tok.Operation == call {
				if err := f.resumeInvocation(c, x.ID, tok.ID, now); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f *Flows) resumeInvocation(c platform.Caller, id string, token int, now time.Time) *kernel.Error {
	c = f.host.Automation(c, ID)
	x, ok := platform.Get[FlowInstance](c, id)
	if !ok || ended(x.State) {
		return nil
	}
	tok := f.session(c, now).token(&x, token)
	if tok.ID < 0 {
		return nil
	}
	step := f.def(x.Flow, x.Version).steps[tok.Step]
	var output json.RawMessage
	var failure string
	var sources []string
	var retryable bool
	if tok.Waits == "operation" {
		result, err := c.OperationResult(tok.Operation)
		if err != nil {
			failure = cmp.Or(err.Message, "Operation result is unavailable")
		} else if result.State == "pending" || result.State == "running" {
			return nil
		} else if result.State != "completed" {
			failure = cmp.Or(result.Error, "Operation "+result.State)
			retryable = result.State == "failed"
		} else {
			output = result.Output
		}
	} else if tok.Waits == "approval" {
		approval, ok := platform.Get[platform.ApprovalRequest](f.host.Automation(c, platform.WorkApp), tok.Child)
		if !ok || approval.State == "pending" {
			return nil
		}
		if approval.State != "approved" {
			failure = cmp.Or(approval.Outcome, "Approval "+approval.State)
		} else {
			output = platform.Raw(map[string]string{"state": approval.State, "target": approval.Target, "receipt": approval.ID})
		}
	} else if tok.Waits == "invocation" && step != nil && step.Invoke != nil {
		run := f.run(&x)
		run.Now = now
		run.Outputs = maps.Clone(tok.Outputs)
		run.Frames = slices.Clone(tok.Frames)
		var done bool
		var err *kernel.Error
		output, done, err = step.Invoke.Result(f.host.Automation(c, f.def(x.Flow, x.Version).app), run, tok.Child)
		sources = run.Sources
		if !done && err == nil {
			return nil
		}
		if err != nil {
			failure = err.Error()
		}
	} else {
		return nil
	}
	return f.step(c, id, now, func(ss *session, in *FlowInstance) {
		if failure != "" {
			if !retryable {
				ss.token(in, token).Attempts = stepAttempts
			}
			ss.failed(in, token, step, failure)
			return
		}
		if !ss.output(in, token, output) {
			ss.outputRefused(in, token)
			return
		}
		ss.sources(in, sources)
		ss.trace(in, tok.Step, "answered", "saved typed output", "")
		ss.next(in, token, "")
	})
}
