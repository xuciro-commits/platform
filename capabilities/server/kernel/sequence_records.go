package kernel

import (
	"fmt"
	"reflect"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type RecordApply struct {
	App            func(string) platform.App
	CheckBindings  func(any) error
	Claims         func(acceptedformat.Record, *pb.ChangeRecord) (func() error, error)
	DefinitionType string
}

func (s *Sequence) ApplyRecordResult(l *platform.Ledger, raw []byte, h RecordApply) (bool, error) {
	result, receipt, err := acceptedformat.DecodeRecord(raw)
	if err != nil {
		return false, err
	}
	if result.Tenant != s.ID || l == nil || !slices.ContainsFunc(l.Declarations(), func(d *pb.AuthorityDeclaration) bool {
		return d.GetTenantId() == result.Tenant && d.GetAuthorityId() == result.App && d.GetDataClass() == result.Row.Type
	}) {
		return false, fmt.Errorf("accepted result belongs to another tenant or authority")
	}
	draft := s.Projection.ForkRecords()
	draft.Lock()
	et := draft.Type(result.Row.Type)
	if et == nil || et.Info().App != result.App {
		draft.Unlock()
		return false, fmt.Errorf("accepted result needs installed entity %s", result.Row.Type)
	}
	value, decoded, err := DecodeAcceptedRecord(et, draft, result, receipt, h.CheckBindings)
	if err != nil {
		draft.Unlock()
		return false, err
	}
	rec := records.Header(value)
	claims, err := h.Claims(result, receipt)
	if err != nil {
		draft.Unlock()
		return false, err
	}
	var publisher platform.AcceptedPublisher
	if result.Kind == "publication" {
		schema := receipt.GetSubmission().GetSchema().GetName()
		var ok bool
		publisher, ok = h.App(result.App).(platform.AcceptedPublisher)
		if !ok || !slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
			draft.Unlock()
			return false, fmt.Errorf("accepted publication has no installer")
		}
		if err := publisher.ValidateAcceptedPublication(schema, decoded); err != nil {
			draft.Unlock()
			return false, fmt.Errorf("accepted publication is invalid: %w", err)
		}
	}
	prior := et.Rows().Get(rec.ID)
	if prior != nil && reflect.DeepEqual(prior.Value.Interface(), value.Interface()) &&
		records.ImageAlreadyApplied(prior, result.Row) {
		draft.Unlock()
		changed, err := l.ApplyAcceptedChange(receipt)
		if err == nil {
			s.Committed.SaveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
			s.Committed.SaveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
			err = claims()
		}
		return changed, err
	}
	if err := InstallAcceptedRecord(draft, et, result, receipt, value, prior); err != nil {
		draft.Unlock()
		return false, err
	}
	draft.Unlock()
	if err := ValidateUniqueReplacements(draft, result.Unique); err != nil {
		return false, err
	}
	changed, err := l.ApplyAcceptedChange(receipt)
	if err != nil {
		return false, err
	}
	if err := s.Projection.PromoteRecords(draft); err != nil {
		return false, err
	}
	if err := claims(); err != nil {
		return false, err
	}
	if result.Row.Type == h.DefinitionType {
		s.DefinitionEpoch.Add(1)
	}
	s.Committed.SaveSchemaVersion(result.App, receipt.GetSubmission().GetIdempotencyKey(), result.SchemaVersion)
	s.Committed.SaveDecisionDefinition(result.App, receipt.GetChangeId(), result.Definition)
	if changed && publisher != nil {
		if err := publisher.ApplyAcceptedPublication(receipt.GetSubmission().GetSchema().GetName(), decoded); err != nil {
			return false, err
		}
	}
	return changed, nil
}
