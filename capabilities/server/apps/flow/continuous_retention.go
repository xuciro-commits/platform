package flow

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Archived rows keep their original content and history. A retention decision
// writes at most 64 KiB of additional images; subsequent timer passes continue
// any backlog through the same original Flow ledger.
const deadLetterArchiveBytes = 64 << 10
const deadLetterArchivePage = 64

func deadLetterDomain(x FlowInstance) []any {
	return []any{[]any{"instance", "=", x.ID}, []any{"flow", "=", x.Flow}, []any{"version", "=", x.Version}}
}

func deadLetterBorn(asset platform.FlowDeadLetterAsset) time.Time {
	if !asset.Created.At.IsZero() {
		return asset.Created.At
	}
	return asset.Letter.RecordedAt
}

func (f *Flows) deadLetterReplayAllowed(c platform.Caller, asset platform.FlowDeadLetterAsset, now time.Time) bool {
	d := f.def(asset.Flow, asset.Version)
	if d == nil || d.Continuous == nil || asset.Archived || asset.Replayed {
		return false
	}
	rule := d.Continuous
	if rule.DeadLetterTTLMs > 0 {
		born := deadLetterBorn(asset)
		if born.IsZero() || !born.Add(time.Duration(rule.DeadLetterTTLMs)*time.Millisecond).After(now) {
			return false
		}
	}
	if rule.DeadLetterMaxRecords > 0 {
		x := FlowInstance{Record: platform.Record{ID: asset.Instance}, Flow: asset.Flow, Version: asset.Version}
		rows, total, err := platform.Find[platform.FlowDeadLetterAsset](f.host.Automation(c, ID), platform.Query{Domain: platform.Raw(deadLetterDomain(x)), Sort: []string{"-created", "-id"}, Offset: rule.DeadLetterMaxRecords - 1, Limit: 1})
		if err != nil || total >= rule.DeadLetterMaxRecords && len(rows) != 1 {
			return false
		}
		if len(rows) == 1 {
			boundary := rows[0]
			if born, cutoff := deadLetterBorn(asset), deadLetterBorn(boundary); born.Before(cutoff) || born.Equal(cutoff) && asset.ID < boundary.ID {
				return false
			}
		}
	}
	return true
}

func (f *Flows) deadLetterArchives(c platform.Caller, x FlowInstance, now time.Time, pending []platform.FlowDeadLetterAsset) ([]platform.FlowDeadLetterAsset, []platform.FlowDeadLetterAsset, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if d == nil || d.Continuous == nil {
		return nil, pending, nil
	}
	rule, domain := d.Continuous, deadLetterDomain(x)
	if rule.DeadLetterTTLMs == 0 && rule.DeadLetterMaxRecords == 0 {
		return nil, pending, nil
	}
	c = f.host.Automation(c, ID)
	var candidates []platform.FlowDeadLetterAsset
	if rule.DeadLetterTTLMs > 0 {
		expired := append(slices.Clone(domain), []any{"created", "<=", now.Add(-time.Duration(rule.DeadLetterTTLMs) * time.Millisecond).Format(time.RFC3339Nano)})
		rows, _, err := platform.Find[platform.FlowDeadLetterAsset](c, platform.Query{Domain: platform.Raw(expired), Sort: []string{"created", "id"}, Limit: deadLetterArchivePage})
		if err != nil {
			return nil, pending, err
		}
		candidates = append(candidates, rows...)
	}
	fresh := 0
	for i := range pending {
		if rule.DeadLetterTTLMs > 0 && !deadLetterBorn(pending[i]).Add(time.Duration(rule.DeadLetterTTLMs)*time.Millisecond).After(now) {
			pending[i].Archived = true
		}
		if _, exists := platform.Get[platform.FlowDeadLetterAsset](c, pending[i].ID); !exists && !pending[i].Archived {
			fresh++
		}
	}
	if rule.DeadLetterMaxRecords > 0 {
		rows, total, err := platform.Find[platform.FlowDeadLetterAsset](c, platform.Query{Domain: platform.Raw(domain), Sort: []string{"created", "id"}, Limit: deadLetterArchivePage})
		if err != nil {
			return nil, pending, err
		}
		overflow := max(0, total+fresh-rule.DeadLetterMaxRecords)
		candidates = append(candidates, rows[:min(overflow, len(rows))]...)
		// A single action loop may create more new failures than its whole
		// retained bound. Archive those extra pending rows in this same result.
		extra := max(0, fresh-rule.DeadLetterMaxRecords)
		slices.SortFunc(pending, func(a, b platform.FlowDeadLetterAsset) int { return cmp.Compare(a.ID, b.ID) })
		for i := range pending {
			if extra == 0 {
				break
			}
			if _, exists := platform.Get[platform.FlowDeadLetterAsset](c, pending[i].ID); !exists && !pending[i].Archived {
				pending[i].Archived = true
				extra--
			}
		}
	}
	seen, used := map[string]bool{}, 0
	var archived []platform.FlowDeadLetterAsset
	for _, asset := range candidates {
		if seen[asset.ID] {
			continue
		}
		seen[asset.ID] = true
		asset.Archived = true
		size := len(platform.Raw(asset))
		if used+size > deadLetterArchiveBytes && len(archived) > 0 {
			break
		}
		// The first historical image may exceed the paging target. The original
		// accepted-result bound decides whether that record can commit.
		used += size
		archived = append(archived, asset)
	}
	return archived, pending, nil
}

func (ss *session) retainDeadLetters() *kernel.Error {
	groups := map[string]*FlowInstance{}
	for id, x := range ss.changed {
		groups[id] = x
	}
	pending := map[string][]platform.FlowDeadLetterAsset{}
	var other []any
	for _, value := range ss.assets {
		if asset, ok := value.(platform.FlowDeadLetterAsset); ok {
			pending[asset.Instance] = append(pending[asset.Instance], asset)
			if groups[asset.Instance] == nil {
				groups[asset.Instance] = ss.load(asset.Instance)
			}
		} else {
			other = append(other, value)
		}
	}
	archiveBytes := 0
	for _, id := range slices.Sorted(maps.Keys(groups)) {
		x := groups[id]
		if x == nil {
			continue
		}
		archived, next, err := ss.f.deadLetterArchives(ss.c, *x, ss.now, pending[id])
		if err != nil {
			return err
		}
		byID := map[string]platform.FlowDeadLetterAsset{}
		for _, asset := range next {
			byID[asset.ID] = asset
		}
		for _, asset := range archived {
			if archiveBytes >= deadLetterArchiveBytes {
				break
			}
			if updated, ok := byID[asset.ID]; ok {
				updated.Archived = true
				asset = updated
			}
			byID[asset.ID] = asset
			archiveBytes += len(platform.Raw(asset))
		}
		for _, key := range slices.Sorted(maps.Keys(byID)) {
			other = append(other, byID[key])
		}
	}
	ss.assets = other
	return nil
}

func (f *Flows) retainExpiredDeadLetters(c platform.Caller, now time.Time) (bool, *kernel.Error) {
	for offset := 0; ; {
		rows, total, err := platform.Find[FlowInstance](c, platform.Query{Archived: true, Sort: []string{"id"}, Limit: 128, Offset: offset})
		if err != nil {
			return false, err
		}
		for _, x := range rows {
			archived, _, err := f.deadLetterArchives(c, x, now, nil)
			if err != nil {
				return false, err
			}
			if len(archived) == 0 {
				continue
			}
			if _, err := f.decide(c, SchemaFlowStep, x.ID, fmt.Sprintf("%s:%d", x.ID, x.Revision+1), now, func(ss *session) *kernel.Error { ss.load(x.ID); return nil }); err != nil {
				return false, err
			}
			// The timer itself is one accepted host result. Do not accumulate
			// one archive budget per instance in that enclosing result.
			return true, nil
		}
		offset += len(rows)
		if offset >= total {
			return false, nil
		}
		if len(rows) == 0 {
			return false, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: "The retained Flow inventory is incomplete"}
		}
	}
}
