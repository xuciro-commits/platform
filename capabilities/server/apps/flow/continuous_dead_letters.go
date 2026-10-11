package flow

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type deadLetterReplayPreparation struct {
	batch *BatchPreparation
	asset platform.FlowDeadLetterAsset
}

func (f *Flows) PlanDeadLetterReplay(x FlowInstance, c platform.Caller, asset platform.FlowDeadLetterAsset) (platform.FlowDeadLetterReplayPreparation, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if asset.Instance != x.ID || asset.Flow != x.Flow || asset.Version != x.Version || asset.OnBehalf != x.OnBehalf || asset.Replayed || asset.Archived || asset.Withheld || asset.Action != "" || !json.Valid([]byte(asset.Letter.Value)) || d == nil || d.Continuous == nil || !d.Continuous.DeadLetterRetry || d.Continuous.Window == nil || d.Continuous.Aggregate == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained signal dead letter is unavailable for replay")
	}
	plan, err := f.PlanBatch(x, c)
	if err != nil {
		return nil, err
	}
	batch := plan.(*BatchPreparation)
	window := *batch.rule.Window
	window.LateEvents = "accept" // explicit replay reopens the retained window; ordinary intake keeps its policy
	batch.rule.Window = &window
	return &deadLetterReplayPreparation{batch: batch, asset: asset}, nil
}

func (p *deadLetterReplayPreparation) Prepare(sub *pb.Submission, at time.Time, store platform.FlowFrameStore) (platform.FlowPreparedBatch, *kernel.Error) {
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

// A rejected business action is retained in the original Flow ledger. The
// captured key is its logical request, independent of retry or replay attempts.
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
		member := ""
		if asset.AsMember {
			member = asset.OnBehalf
		}
		if _, err := ss.act(asset.App, asset.Protocol, asset.Action, asset.Target, asset.Payload, asset.Key, member); err != nil {
			return nil, err
		}
		asset.Replayed = true
		ss.assets = append(ss.assets, asset)
		ss.trace(x, asset.Letter.Node, "dead letter replayed", asset.ID, c.ID)
		return ss.apply, nil
	})
}
