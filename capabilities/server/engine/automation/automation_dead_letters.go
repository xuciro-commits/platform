package automation

import (
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"time"
)

type deadLetterReplayPreparation struct {
	batch *BatchPreparation
	asset platform.FlowDeadLetterAsset
}

func (f *Flows) PlanDeadLetterReplay(x FlowInstance, c platform.Caller, asset platform.FlowDeadLetterAsset) (platform.FlowDeadLetterReplayPreparation, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if asset.Instance != x.ID || asset.Flow != x.Flow || asset.Version != x.Version || asset.OnBehalf != x.OnBehalf || asset.Replayed || asset.Archived || asset.Withheld || asset.Action != "" || !json.Valid([]byte(asset.Letter.Value)) || d == nil || d.Continuous == nil || !d.Continuous.DeadLetterRetry || d.Continuous.Window == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained signal dead letter is unavailable for replay")
	}
	plan, err := f.PlanBatch(x, c)
	if err != nil {
		return nil, err
	}
	batch := plan.(*BatchPreparation)
	window := *batch.rule.Window
	window.LateEvents = "accept"
	batch.rule.Window = &window
	return &deadLetterReplayPreparation{batch: batch, asset: asset}, nil
}

func (p *deadLetterReplayPreparation) Prepare(sub *pb.Submission, at time.Time, store platform.FlowFrameStore) (platform.FlowPreparedBatch, *kernel.Error) {
	if !p.batch.owner.deadLetterReplayAllowed(p.batch.caller, p.asset, at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained signal dead letter is unavailable for replay")
	}
	if sub.GetSchema().GetName() != SchemaFlowReplay || sub.GetTarget().GetType() != "flow.dead-letter" || sub.GetTarget().GetId() != p.asset.ID || sub.GetPrincipalId() != p.asset.OnBehalf {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A signal replay requires its original member and dead-letter asset")
	}
	x := p.batch.before
	batch := Batch{ID: "replay:" + p.asset.ID, Predecessor: x.Batch.Cursor, Source: x.Batch.Source, Signals: []Signal{{Key: p.asset.Letter.Key, Partition: p.asset.Letter.Partition, At: p.asset.Letter.At, Value: json.RawMessage(p.asset.Letter.Value)}}}
	prepared, err := p.batch.Prepare(batch, at, store)
	if err != nil {
		return nil, err
	}
	result := prepared.(*PreparedBatch)
	result.replayAsset = &p.asset
	result.replaySubmission = proto.Clone(sub).(*pb.Submission)
	return result, nil
}

func (ss *session) deadLetterAct(x *FlowInstance, token *Token, step *platform.Step, target string, payload json.RawMessage, reason string) {
	d := ss.def(x)
	if d == nil || d.Continuous == nil || step.Act == nil {
		return
	}
	limit := stepAttempts
	if step.Retry != nil && step.Retry.Attempts > 0 {
		limit = step.Retry.Attempts
	}
	if !step.NoRetry && token.Attempts+1 < limit {
		return
	}
	key := fmt.Sprintf("flow:%s:%s:%d", x.ID, token.Step, x.Seq)
	batch := ""
	if x.Batch != nil {
		batch = x.Batch.Cursor
	}
	asset := platform.FlowDeadLetterAsset{Record: platform.Record{ID: fmt.Sprintf("flow-dead-letter:%x", sha256.Sum256([]byte(key)))}, App: d.app,
		Instance: x.ID, Flow: x.Flow, Version: x.Version, OnBehalf: x.OnBehalf, Sources: slices.Clone(x.Sources),
		Letter: platform.FlowDeadLetter{Batch: batch, Node: step.Name, Key: target, RecordedAt: ss.now, Reason: reason, Value: string(payload)},
		Action: step.Act.Action, Protocol: step.Act.Protocol, Target: target, Payload: slices.Clone(payload), Key: key, AsMember: step.Act.AsMember}
	ss.assets = append(ss.assets, asset)
	x.Seq++
}

func (f *Flows) replayDeadLetter(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return f.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		asset, ok := platform.Get[platform.FlowDeadLetterAsset](c, sub.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The Flow dead letter is unavailable")
		}
		if asset.Withheld || asset.Archived || asset.Replayed || asset.Action == "" || asset.Key == "" || !json.Valid(asset.Payload) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This dead letter has no replayable rejected action")
		}
		ss := f.session(f.host.Automation(c, ID), now)
		x := ss.load(asset.Instance)
		if x == nil || ended(x.State) || x.Flow != asset.Flow || x.Version != asset.Version || x.OnBehalf != asset.OnBehalf {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original Flow version must be available and running to replay its dead letter")
		}
		if err := f.checkBinding(*x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		d := ss.def(x)
		if d == nil || d.app != asset.App || d.Continuous == nil || !d.Continuous.DeadLetterRetry {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained dead-letter definition is unavailable")
		}
		if !f.deadLetterReplayAllowed(ss.c, asset, now) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained dead-letter definition is unavailable")
		}
		member := ""
		if asset.AsMember {
			member = asset.OnBehalf
		}
		if _, err := ss.act(x, asset.App, asset.Protocol, asset.Action, asset.Target, asset.Payload, asset.Key, member); err != nil {
			return nil, err
		}
		asset.Replayed = true
		ss.assets = append(ss.assets, asset)
		if asset.Key == fmt.Sprintf("flow:%s:%s:%d", x.ID, asset.Letter.Node, x.Seq) {
			x.Seq++
		}
		ss.trace(x, asset.Letter.Node, "dead letter replayed", asset.ID, c.ID)
		if err := ss.retainDeadLetters(); err != nil {
			return nil, err
		}
		return ss.apply, nil
	})
}

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
