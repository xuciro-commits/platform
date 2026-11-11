package kernel

import (
	"fmt"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type BatchApply struct {
	App            func(string) platform.App
	OwnerLedger    func(string) *platform.Ledger
	CheckBindings  func(any) error
	DefinitionType string
	Claims         func(acceptedformat.Batch, *pb.ChangeRecord) error
	Check          func(acceptedformat.Batch) error
	Rows           func(*records.Store, acceptedformat.Batch) error
	Install        func(*records.Store, acceptedformat.Batch) (*records.Store, func() error, error)
	Sequences      func(acceptedformat.Batch) error
	Installed      func(acceptedformat.Batch) error
	Apply          func(acceptedformat.Batch) error
	Settle         func(acceptedformat.Batch) error
}

type PreparedBatch struct {
	Result  acceptedformat.Batch
	Records *records.Store
	ledgers map[string]*platform.Ledger
	changes map[string]*pb.ChangeRecord
	key     string
	raw     []byte
	install func() error
}

func (s *Sequence) PrepareBatch(l *platform.Ledger, raw []byte, h BatchApply) (*PreparedBatch, error) {
	result, receipt, err := acceptedformat.DecodeBatch(raw)
	if err != nil {
		return nil, err
	}
	owner, ok := h.App(result.App).(platform.App)
	if result.Tenant != s.ID || !ok || owner.AcceptedLedger() != l {
		return nil, fmt.Errorf("record batch belongs to another tenant or ledger")
	}
	sub, _ := acceptedformat.BatchSubmission(result, receipt)
	if err := h.Claims(result, receipt); err != nil {
		return nil, err
	}
	key := result.App + "/" + sub.GetIdempotencyKey()
	if saved := s.Committed.Answer(key); len(saved) > 0 {
		prior, answer, err := acceptedformat.DecodeBatch(saved)
		if err != nil || prior.RequestHash != result.RequestHash || !proto.Equal(answer, receipt) {
			return nil, fmt.Errorf("record batch key belongs to another answer")
		}
		return nil, nil
	}
	answerOwner, ok := h.App(receipt.GetSubmission().GetAuthority()).(platform.App)
	if !ok {
		return nil, fmt.Errorf("record batch answer has no authority")
	}
	if prior := answerOwner.AcceptedLedger().AcceptedFor(s.ID, receipt.GetSubmission().GetIdempotencyKey()); prior != nil {
		if !proto.Equal(prior, receipt) {
			return nil, fmt.Errorf("record batch key belongs to another result")
		}
		return nil, nil
	}
	ledgers := map[string]*platform.Ledger{}
	changes := map[string]*pb.ChangeRecord{}
	for _, decision := range result.Decisions {
		ra, ok := h.App(decision.Event.App).(platform.App)
		if !ok {
			return nil, fmt.Errorf("record batch needs missing authority %s", decision.Event.App)
		}
		ledgers[decision.Event.App] = ra.AcceptedLedger()
		for _, subscriber := range decision.Event.Subscribers {
			if h.App(subscriber) == nil {
				return nil, fmt.Errorf("record batch needs missing subscriber %s", subscriber)
			}
		}
		r := new(pb.ChangeRecord)
		if err := protojson.Unmarshal(decision.Receipt, r); err != nil {
			return nil, err
		}
		if _, refused := s.Committed.Refusal(decision.Event.App + "/" + r.GetSubmission().GetIdempotencyKey()); refused {
			return nil, fmt.Errorf("record batch reused a refused child key")
		}
		changes[decision.Event.App+"/"+r.GetChangeId()] = r
	}
	draft := s.Projection.ForkRecords()
	if err := h.Check(result); err != nil {
		return nil, err
	}
	for i, decision := range result.Decisions {
		if decision.Publication == nil {
			continue
		}
		publisher, ok := h.App(decision.Event.App).(platform.AcceptedPublisher)
		if !ok || !slices.Contains(publisher.AcceptedPublicationSchemas(), decision.Publication.Schema) ||
			decision.Publication.Schema != decision.Event.Names[0] {
			return nil, fmt.Errorf("record batch has an invalid publication")
		}
		r := changes[decision.Event.App+"/"+acceptedformat.ReceiptID(decision.Receipt)]
		if r == nil {
			return nil, fmt.Errorf("publication has no receipt")
		}
		et := draft.Type(r.GetSubmission().GetTarget().GetType())
		if et == nil {
			return nil, fmt.Errorf("publication record type is unavailable")
		}
		decoded, err := records.DecodeFormattedImage(et.Info(), decision.Publication.Image, nil, draft.Current().Release(), decision.Publication.Format)
		if err != nil {
			return nil, err
		}
		copy := *decision.Publication
		copy.Image = decoded
		copy.Format = et.Info().RecordFormat
		result.Decisions[i].Publication = &copy
		if err := publisher.ValidateAcceptedPublication(copy.Schema, copy.Image); err != nil {
			return nil, err
		}
	}
	if err := h.Rows(draft, result); err != nil {
		return nil, err
	}
	if err := ApplyBatchRows(draft, result, s.ID, h.OwnerLedger, ledgers, changes, h.CheckBindings); err != nil {
		return nil, err
	}
	draft, install, err := h.Install(draft, result)
	if err != nil {
		return nil, err
	}
	if err := draft.ValidateChangedLinkConstraints(); err != nil {
		return nil, err
	}
	if err := ValidateUniqueReplacements(draft, result.Unique); err != nil {
		return nil, err
	}
	if err := h.Sequences(result); err != nil {
		return nil, err
	}
	checks := map[string]*kernel.ChangeLog{}
	for app, ledger := range ledgers {
		checks[app] = ledger.ForkAcceptedChanges()
	}
	for _, decision := range result.Decisions {
		r := changes[decision.Event.App+"/"+acceptedformat.ReceiptID(decision.Receipt)]
		if changed, err := checks[decision.Event.App].ApplyAccepted(r); err != nil || !changed {
			return nil, fmt.Errorf("record batch ledger validation: %v (new=%t)", err, changed)
		}
	}
	return &PreparedBatch{Result: result, Records: draft, ledgers: ledgers, changes: changes, key: key, raw: raw, install: install}, nil
}

func (s *Sequence) PromoteBatch(prepared *PreparedBatch, h BatchApply) (bool, error) {
	result, key := prepared.Result, prepared.key
	for _, decision := range result.Decisions {
		r := prepared.changes[decision.Event.App+"/"+acceptedformat.ReceiptID(decision.Receipt)]
		if changed, err := prepared.ledgers[decision.Event.App].ApplyAcceptedChange(r); err != nil || !changed {
			return false, fmt.Errorf("record batch ledger application: %v (new=%t)", err, changed)
		}
	}
	if err := s.Projection.PromoteRecords(prepared.Records); err != nil {
		return false, err
	}
	if prepared.install != nil {
		if err := prepared.install(); err != nil {
			return false, err
		}
	}
	if err := h.Installed(result); err != nil {
		return false, err
	}
	if slices.ContainsFunc(result.Rows, func(row acceptedformat.BatchRow) bool { return row.Type == h.DefinitionType }) {
		s.DefinitionEpoch.Add(1)
	}
	if err := h.Apply(result); err != nil {
		return false, err
	}
	for _, decision := range result.Decisions {
		if p := decision.Publication; p != nil {
			if err := h.App(decision.Event.App).(platform.AcceptedPublisher).ApplyAcceptedPublication(p.Schema, p.Image); err != nil {
				return false, err
			}
		}
	}
	if err := h.Settle(result); err != nil {
		return false, err
	}
	for _, child := range result.Decisions {
		s.Committed.SaveDecisionDefinition(child.Event.App, acceptedformat.ReceiptID(child.Receipt), child.Definition)
	}
	if result.SchemaVersion != nil {
		_, request, _ := strings.Cut(key, "/")
		s.Committed.SaveSchemaVersion(result.App, request, result.SchemaVersion)
	}
	answer := new(pb.ChangeRecord)
	_ = protojson.Unmarshal(result.Receipt, answer)
	input, _ := acceptedformat.BatchSubmission(result, answer)
	if result.HasSavedAnswer(input) {
		s.Committed.SaveAnswer(key, prepared.raw)
	}
	return true, nil
}
