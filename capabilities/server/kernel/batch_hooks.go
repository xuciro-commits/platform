package kernel

import (
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (c SourceClaims) BatchPrepare(tenant string) func(acceptedformat.Batch, *pb.ChangeRecord) error {
	return func(result acceptedformat.Batch, receipt *pb.ChangeRecord) error {
		if len(result.Claims) == 0 {
			return nil
		}
		sub, _ := acceptedformat.BatchSubmission(result, receipt)
		_, err := c.Prepare(tenant, sub.GetTarget().GetType(), sub.GetTarget().GetId(), receipt.GetChangeId(), &ClaimSet{Origin: result.Origin, Claims: result.Claims})
		return err
	}
}

func (c SourceClaims) BatchInstalled(tenant string) func(acceptedformat.Batch) error {
	return func(result acceptedformat.Batch) error {
		if len(result.Claims) == 0 {
			return nil
		}
		answer := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(result.Receipt, answer); err != nil {
			return err
		}
		sub := answer.GetSubmission()
		return c.Apply(tenant, sub.GetTarget().GetType(), sub.GetTarget().GetId(), answer.GetChangeId(), result.At, &ClaimSet{Origin: result.Origin, Claims: result.Claims})
	}
}

type BatchInstall struct {
	Definitions func(*records.Store, []platform.ReleaseInstallation) (*records.Store, func() error, error)
	Publication func(store *records.Store, schema string, image []byte) error
}

func (b BatchInstall) Install(draft *records.Store, result acceptedformat.Batch) (*records.Store, func() error, error) {
	var install func() error
	if len(result.Definitions) > 0 {
		var err error
		if draft, install, err = b.Definitions(draft, result.Definitions); err != nil {
			return nil, nil, err
		}
	}
	for _, decision := range result.Decisions {
		if p := decision.Publication; p != nil {
			if err := b.Publication(draft, p.Schema, p.Image); err != nil {
				return nil, nil, err
			}
		}
	}
	return draft, install, nil
}

func PublishBatch(result acceptedformat.Batch, publish func(platform.Event, platform.AcceptedEventPlan)) {
	for _, decision := range result.Decisions {
		r := new(pb.ChangeRecord)
		_ = protojson.Unmarshal(decision.Receipt, r)
		publish(platform.Event{App: decision.Event.App, Record: r, Changed: decision.Event.Changed}, decision.Event)
	}
}

type BatchState struct {
	App             func(string) platform.App
	States          map[string]platform.AcceptedStateApp
	Notices         *NoticeBoard
	Sequences       *Sequences
	CheckControl    func(*acceptedformat.Control) error
	ApplyControl    func(*acceptedformat.Control) error
	CheckIntents    func([]platform.Effect) error
	InstallIntents  func([]platform.Effect)
	ApplyNotices    func(*acceptedformat.Notices)
	CheckOperations func([]acceptedformat.OperationCancellation) error
	ApplyOperations func([]acceptedformat.OperationCancellation) error
}

func (b BatchState) Hooks(h BatchApply) BatchApply {
	h.Check = func(result acceptedformat.Batch) error {
		if err := b.CheckControl(result.Control); err != nil {
			return err
		}
		if err := b.Notices.Check(result.Notices); err != nil {
			return err
		}
		if err := b.CheckIntents(result.Intents); err != nil {
			return err
		}
		if err := CheckObservations(b.App, result.Observations); err != nil {
			return err
		}
		if err := CheckStates(b.States, result.States); err != nil {
			return err
		}
		return b.CheckOperations(result.Operations)
	}
	h.Sequences = func(result acceptedformat.Batch) error {
		return b.Sequences.Check(result.SequenceBases, result.Sequences)
	}
	h.Apply = func(result acceptedformat.Batch) error {
		if err := b.ApplyControl(result.Control); err != nil {
			return err
		}
		if err := ApplyStates(b.States, result.States); err != nil {
			return err
		}
		return b.ApplyOperations(result.Operations)
	}
	h.Settle = func(result acceptedformat.Batch) error {
		b.ApplyNotices(result.Notices)
		b.InstallIntents(result.Intents)
		if err := ApplyObservations(b.App, result.Observations); err != nil {
			return err
		}
		b.Sequences.Set(result.Sequences)
		return nil
	}
	return h
}

func (b BatchState) Changes(batch BatchApply) ChangeApply {
	return ChangeApply{Batch: batch,
		Check: func(plan acceptedformat.ChangePlan) error {
			if err := b.CheckControl(plan.Control); err != nil {
				return err
			}
			if err := b.Notices.Check(plan.Notices); err != nil {
				return err
			}
			if err := b.CheckIntents(plan.Intents); err != nil {
				return err
			}
			if err := CheckStates(b.States, plan.States); err != nil {
				return err
			}
			return b.Sequences.Check(plan.SequenceBases, plan.Sequences)
		},
		Apply: func(plan acceptedformat.ChangePlan) error {
			if err := b.ApplyControl(plan.Control); err != nil {
				return err
			}
			if err := ApplyStates(b.States, plan.States); err != nil {
				return err
			}
			b.ApplyNotices(plan.Notices)
			b.InstallIntents(plan.Intents)
			b.Sequences.Set(plan.Sequences)
			return nil
		}}
}

func LedgerOf(app func(string) platform.App) func(string) *platform.Ledger {
	return func(name string) *platform.Ledger {
		if owner := app(name); owner != nil {
			return owner.AcceptedLedger()
		}
		return nil
	}
}
