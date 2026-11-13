package applications

import (
	"encoding/json"
	"slices"
	"strings"

	"platformserver/platform"
)

func IndexedRuns(entries []platform.RunIndexEntry, ref platform.AssetRef, title string, offset, limit int, filters ...platform.ApplicationRunFilter) platform.ApplicationRunPage {
	filter := platform.ApplicationRunFilter{}
	if len(filters) > 0 {
		filter = filters[0]
	}
	runs := make([]platform.ApplicationRun, 0, len(entries))
	for _, entry := range entries {
		var run platform.ApplicationRun
		if json.Unmarshal(entry.Payload, &run) != nil {
			continue
		}
		if filter.Kind != "" && run.Kind != filter.Kind || filter.State != "" && run.State != filter.State ||
			filter.Subject != "" && run.Subject != filter.Subject ||
			!filter.Since.IsZero() && run.Created.Before(filter.Since) ||
			!filter.Until.IsZero() && run.Created.After(filter.Until) {
			continue
		}
		runs = append(runs, run)
	}
	slices.SortFunc(runs, func(a, b platform.ApplicationRun) int {
		failed := func(run platform.ApplicationRun) bool {
			return run.Error != "" || slices.Contains([]string{"failed", "stuck", "blocked", "stopped", "rejected"}, run.State)
		}
		if failed(a) != failed(b) {
			if failed(a) {
				return -1
			}
			return 1
		}
		if a.Created.Before(b.Created) {
			return 1
		}
		if a.Created.After(b.Created) {
			return -1
		}
		return strings.Compare(a.Kind+"/"+a.ID, b.Kind+"/"+b.ID)
	})
	out := platform.ApplicationRunPage{Application: ref, Title: title, Runs: []platform.ApplicationRun{}, Total: len(runs)}
	out.Summary = runSummary(runs)
	start := min(offset, len(runs))
	out.Runs = append(out.Runs, runs[start:min(start+limit, len(runs))]...)
	return out
}

func runSummary(runs []platform.ApplicationRun) platform.ApplicationRunSummary {
	out := platform.ApplicationRunSummary{}
	for _, run := range runs {
		if run.Kind == "agent" || run.Kind == "function" {
			out.AITokens += run.AITokens
			out.AIUSD += run.AIUSD
			if !run.CostReported {
				out.UnmeteredAI++
			}
		}
		if slices.Contains([]string{"failed", "stuck", "blocked", "stopped", "rejected"}, run.State) || run.Error != "" {
			out.Failed++
		}
		if (run.Kind == "effect" || run.Kind == "compute") && !slices.Contains([]string{"delivered", "rejected", "failed", "discarded", "compensated"}, run.State) {
			out.PendingEffects++
		}
		if run.Kind == "flow" && slices.Contains([]string{"running", "waiting", "paused"}, run.State) {
			out.WaitingFlows++
		}
	}
	return out
}
