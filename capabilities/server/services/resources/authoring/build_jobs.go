package build

import (
	"fmt"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"time"
)

func (b *Build) Run(c platform.Caller, job string, at time.Time) *kernel.Error {
	if job == "dispatches" {
		return b.runDispatches(c, at)
	}
	if job != "confirmations" {
		return nil
	}
	if err := b.automation.Run(c, at); err != nil {
		return err
	}
	runs, err := platform.PendingConfirmations(c)
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.Completion != "" || run.CompletionError != "" || run.State == "compensated" {
			continue
		}
		if !at.Before(run.Deadline) && !slices.Contains([]string{"confirmed", "expired", "compensating", "rejected"}, run.State) {
			_, err := platform.Decide(c, b, confirmationSubmission(c, run.ID, confirmationTick, "deadline", map[string]any{}, at), at)
			return err
		}
		action := ""
		if run.State == "confirmed" {
			action = run.Definition.OnComplete
		} else if run.Partial && run.Definition.OnPartial != "hold" && run.State != "expired" && run.State != "compensating" {
			action = run.Definition.OnPartialAction
		}
		if action != "" {
			change, problem := c.ConfirmationTransport().CompleteConfirmation(c, nil, run, action, at)
			payload := map[string]any{"completion": change}
			if problem != nil {
				payload["completionError"] = problem.Code.String() + ": " + problem.Message
			}
			key := fmt.Sprintf("completion:%d", run.Revision)
			_, err := platform.Decide(c, b, confirmationSubmission(c, run.ID, confirmationTick, key, payload, at), at)
			return err
		}
	}
	return nil
}
