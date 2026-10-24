package platform

import (
	"fmt"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// IntervalIdentity is a current projection row. Retired rows are excluded by
// the original declared collection and can never enter the matching pool.
type IntervalIdentity struct {
	ID       string
	Interval ObservationInterval
}
type IntervalChange struct {
	ID           string
	Interval     ObservationInterval
	New          bool
	Changed      bool
	Review       bool
	Predecessors []string
}
type IntervalReconciliation struct {
	Events    []IntervalChange
	Retired   []string
	Redirects []*pb.Redirect
}

func intervalOverlap(a, b ObservationInterval) bool {
	return a.Since.Before(b.Until) && b.Since.Before(a.Until) || a.OpenEnd && b.OpenStart && a.Until.Equal(b.Since) || b.OpenEnd && a.OpenStart && b.Until.Equal(a.Since)
}

// ReconcileObservationIntervals matches only confirmed ranges. K1 validates
// fresh identities and redirects; the caller writes rows/links in its original
// private transaction. Human fields are deliberately absent from this plan.
func ReconcileObservationIntervals(object string, previous []IntervalIdentity, result ObservationIntervalResult, newID func(int) string, engines ...KernelFactory) (IntervalReconciliation, error) {
	out := IntervalReconciliation{Events: []IntervalChange{}, Retired: []string{}, Redirects: []*pb.Redirect{}}
	if err := result.Check(); err != nil {
		return out, err
	}
	if object == "" || newID == nil || len(previous) > RecordSetMaxRows {
		return out, fmt.Errorf("interval reconciliation needs its bounded original collection")
	}
	previous = slices.Clone(previous)
	slices.SortFunc(previous, func(a, b IntervalIdentity) int {
		if order := a.Interval.Since.Compare(b.Interval.Since); order != 0 {
			return order
		}
		return a.Interval.Until.Compare(b.Interval.Until)
	})
	entities := make([]*pb.EntityRef, 0, len(previous))
	seen := map[string]bool{}
	for i, old := range previous {
		if old.ID == "" || seen[old.ID] || old.Interval.Since.IsZero() || !old.Interval.Since.Before(old.Interval.Until) || i > 0 && previous[i-1].Interval.Until.After(old.Interval.Since) {
			return out, fmt.Errorf("current interval identities must be distinct ordered ranges")
		}
		seen[old.ID] = true
		entities = append(entities, &pb.EntityRef{Type: object, Id: old.ID})
	}
	var engine KernelFactory = NativeKernel{}
	if len(engines) != 0 {
		engine = engines[0]
	}
	identity := engine.Identity(entities)
	type candidate struct {
		interval ObservationInterval
		review   bool
	}
	candidates := make([]candidate, 0, len(result.Intervals)+2)
	for _, event := range result.Intervals {
		candidates = append(candidates, candidate{interval: event})
	}
	affected := []IntervalIdentity{}
	for _, old := range previous {
		event := old.Interval
		intersects := event.Since.Before(result.Until) && result.Since.Before(event.Until)
		touchesLeft := event.OpenEnd && event.Until.Equal(result.Since)
		touchesRight := event.OpenStart && event.Since.Equal(result.Until)
		if !intersects && !touchesLeft && !touchesRight {
			continue
		}
		affected = append(affected, old)
		if event.Since.Before(result.Since) || touchesLeft {
			left := -1
			for i, c := range candidates {
				if c.interval.OpenStart && c.interval.Since.Equal(result.Since) {
					left = i
					break
				}
			}
			if left >= 0 {
				candidates[left].interval.Since = event.Since
				candidates[left].interval.OpenStart = event.OpenStart
			} else {
				// A contradictory correction inside an older range cannot prove where
				// its out-of-window prefix ended. Keep that boundary explicitly open.
				candidates = append(candidates, candidate{interval: ObservationInterval{Since: event.Since, Until: result.Since, OpenStart: event.OpenStart, OpenEnd: !touchesLeft}, review: !touchesLeft})
			}
		}
		if result.Until.Before(event.Until) || touchesRight {
			right := -1
			for i, c := range candidates {
				if c.interval.OpenEnd && c.interval.Until.Equal(result.Until) {
					right = i
					break
				}
			}
			if right >= 0 {
				candidates[right].interval.Until = event.Until
				candidates[right].interval.OpenEnd = event.OpenEnd
			} else {
				candidates = append(candidates, candidate{interval: ObservationInterval{Since: result.Until, Until: event.Until, OpenStart: true, OpenEnd: event.OpenEnd}, review: true})
			}
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int { return a.interval.Since.Compare(b.interval.Since) })
	merged := make([]candidate, 0, len(candidates))
	for _, next := range candidates {
		if !next.interval.Since.Before(next.interval.Until) {
			continue
		}
		if len(merged) > 0 && !merged[len(merged)-1].interval.Until.Before(next.interval.Since) {
			last := &merged[len(merged)-1]
			last.review = last.review || next.review
			if last.interval.Until.Before(next.interval.Until) {
				last.interval.Until = next.interval.Until
				last.interval.OpenEnd = next.interval.OpenEnd
			}
			continue
		}
		merged = append(merged, next)
	}
	if len(merged) > ObservationMaxIntervals {
		return out, fmt.Errorf("interval reconciliation exceeds its result bound")
	}
	predecessors := make([][]int, len(merged))
	successors := make([][]int, len(affected))
	for i, event := range merged {
		for j, old := range affected {
			if intervalOverlap(event.interval, old.Interval) {
				predecessors[i] = append(predecessors[i], j)
				successors[j] = append(successors[j], i)
			}
		}
	}
	for i, event := range merged {
		change := IntervalChange{Interval: event.interval, Review: event.review, Predecessors: []string{}}
		if len(predecessors[i]) == 1 && len(successors[predecessors[i][0]]) == 1 {
			old := affected[predecessors[i][0]]
			change.ID = old.ID
			change.Changed = old.Interval != event.interval
		} else {
			change.ID = newID(i)
			change.New = true
			change.Changed = true
			if refusal := identity.Create(&pb.EntityRef{Type: object, Id: change.ID}); refusal != nil {
				return out, fmt.Errorf("interval identity is not fresh: %v", refusal)
			}
			for _, index := range predecessors[i] {
				change.Predecessors = append(change.Predecessors, affected[index].ID)
			}
			slices.Sort(change.Predecessors)
			change.Review = change.Review || len(change.Predecessors) > 0
		}
		out.Events = append(out.Events, change)
	}
	for i, old := range affected {
		kept := false
		for _, event := range out.Events {
			kept = kept || event.ID == old.ID
		}
		if kept {
			continue
		}
		out.Retired = append(out.Retired, old.ID)
		if len(successors[i]) == 0 {
			continue
		}
		redirect := &pb.Redirect{From: &pb.EntityRef{Type: object, Id: old.ID}, Kind: pb.RedirectKind_REDIRECT_KIND_MERGE}
		for _, index := range successors[i] {
			redirect.To = append(redirect.To, &pb.EntityRef{Type: object, Id: out.Events[index].ID})
		}
		if len(redirect.To) > 1 {
			redirect.Kind = pb.RedirectKind_REDIRECT_KIND_SPLIT
		}
		if refusal := identity.AddRedirect(redirect); refusal != nil {
			return out, fmt.Errorf("interval redirect violates K1: %v", refusal)
		}
		out.Redirects = append(out.Redirects, redirect)
	}
	return out, nil
}
