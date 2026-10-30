package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/studio/build"
)

type sourceSubmission struct {
	binding *platform.DataSourceBinding
	row     build.SourceRow
}

type preparedSourceClaim struct {
	origin string
	claims []acceptedformat.SourceClaim
}

type RecordFieldProvenance struct {
	Value      json.RawMessage `json:"-"`
	Kind       string          `json:"kind"`
	Connector  string          `json:"connector,omitempty"`
	SourceTime *time.Time      `json:"sourceTime,omitempty"`
	At         *time.Time      `json:"at,omitempty"`
}

type RecordProvenance struct {
	Fields map[string]RecordFieldProvenance `json:"fields"`
}

func (t *Tenant) recordProvenance(m platform.Member, typ, id string, now time.Time) (RecordProvenance, *kernel.Error) {
	m = t.authorizationMember(m)
	caller := platform.NewCaller(runtime{t}, m, "", false, false)
	if _, refusal := t.readRecordFrom(t.records, caller, typ, id, now); refusal != nil {
		return RecordProvenance{}, refusal
	}
	t.records.Lock()
	et := t.records.Type(typ)
	row := et.Rows().Get(id)
	history := row.RecordHistory()
	info := et.Info()
	t.records.Unlock()
	strategies := map[string]string{}
	for _, source := range info.DataSources {
		for _, column := range source.Columns {
			strategies[column.Attribute] = column.Strategy
		}
	}
	answer := RecordProvenance{Fields: map[string]RecordFieldProvenance{}}
	for _, field := range info.Fields {
		if field.Name == "" || field.Name == "id" {
			continue
		}
		decision := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "read", Type: typ, Record: id, Fields: []string{field.Name}}, now, false)
		if !decision.Allow || len(decision.Withheld) > 0 {
			continue
		}
		if selected := t.selectedSourceValue(typ, id, field.Name, strategies[field.Name], history, nil); selected.Kind != "" {
			answer.Fields[field.Name] = selected
		}
	}
	return answer, nil
}

func (t *Tenant) selectedSourceValue(typ, id, attribute, strategy string, history []records.RecordChange, incoming *acceptedformat.SourceClaim) RecordFieldProvenance {
	ref := &pb.EntityRef{Type: typ, Id: id}
	var source RecordFieldProvenance
	for _, record := range t.claims.CurrentClaims(t.ID, ref, attribute) {
		fact := record.GetFact()
		at := fact.GetProvenance().GetSourceTime().AsTime()
		if source.Kind == "" || !at.Before(*source.SourceTime) {
			source = RecordFieldProvenance{Value: fact.GetPayload(), Kind: "source", Connector: fact.GetProvenance().GetConnectorId(), SourceTime: &at}
		}
	}
	if incoming != nil && (source.Kind == "" || !incoming.SourceTime.Before(*source.SourceTime)) {
		at := incoming.SourceTime
		source = RecordFieldProvenance{Value: incoming.Payload, Kind: "source", Connector: incoming.Connector, SourceTime: &at}
	}
	var edit RecordFieldProvenance
	for i := len(history) - 1; i >= 0 && edit.Kind == ""; i-- {
		change := history[i]
		if t.claims.Accepted(t.ID, typ+"/"+id+"/"+change.Change+"/"+attribute) != nil {
			continue
		}
		for _, field := range change.Fields {
			if field.Field == attribute {
				at := change.At
				edit = RecordFieldProvenance{Value: field.After, Kind: "edit", At: &at}
				break
			}
		}
	}
	switch strategy {
	case "edit-only":
		return edit
	case "latest-source-time":
		if edit.Kind != "" && (source.Kind == "" || !edit.At.Before(*source.SourceTime)) {
			return edit
		}
		return source
	default:
		if edit.Kind != "" {
			return edit
		}
		return source
	}
}

func (t *Tenant) prepareSourceSubmission(m platform.Member, sub *pb.Submission, source *sourceSubmission, now time.Time) (*pb.Submission, *preparedSourceClaim, *kernel.Error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(sub.GetPayload(), &values); err != nil || len(values) == 0 || source.row.At == nil {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source row has no mapped values or source time")
	}
	strategies := map[string]string{}
	for _, column := range source.binding.Columns {
		strategies[column.Attribute] = column.Strategy
	}
	attributes := make([]string, 0, len(values))
	for name := range values {
		if _, ok := strategies[name]; !ok {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source row has an undeclared attribute")
		}
		attributes = append(attributes, name)
	}
	slices.Sort(attributes)
	caller := platform.NewCaller(runtime{t}, m, sub.GetAuthority(), false, false)
	kind := "write"
	if strings.HasSuffix(sub.GetSchema().GetName(), ".create") {
		kind = "create"
	}
	decision := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: kind, Type: sub.GetTarget().GetType(), Record: sub.GetTarget().GetId(), Fields: attributes}, now, false)
	if !decision.Allow || len(decision.Withheld) > 0 {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, decision.Reason)
	}
	t.records.Lock()
	et := t.records.Type(sub.GetTarget().GetType())
	var history []records.RecordChange
	if et != nil {
		if row := et.Rows().Get(sub.GetTarget().GetId()); row != nil {
			history = row.RecordHistory()
		}
	}
	t.records.Unlock()
	claims := make([]acceptedformat.SourceClaim, 0, len(attributes))
	payload := map[string]json.RawMessage{}
	for _, attribute := range attributes {
		claim := acceptedformat.SourceClaim{Attribute: attribute, Connector: source.binding.Kind + ":" + source.binding.Source, SourceTime: *source.row.At, Payload: values[attribute]}
		claims = append(claims, claim)
		selected := t.selectedSourceValue(sub.GetTarget().GetType(), sub.GetTarget().GetId(), attribute, strategies[attribute], history, &claim)
		if selected.Kind == "source" {
			payload[attribute] = selected.Value
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source row cannot be encoded")
	}
	copy := proto.Clone(sub).(*pb.Submission)
	copy.Payload = encoded
	origin := source.row.Origin
	if origin == sub.GetTarget().GetId() {
		origin = ""
	}
	return copy, &preparedSourceClaim{origin: origin, claims: claims}, nil
}

func (t *Tenant) sourceFacts(typ, id, change string, prepared *preparedSourceClaim) ([]*pb.Fact, error) {
	if prepared == nil {
		return nil, nil
	}
	facts := make([]*pb.Fact, 0, len(prepared.claims))
	for _, claim := range prepared.claims {
		fact := &pb.Fact{TenantId: t.ID, Kind: pb.FactKind_FACT_KIND_CLAIM, Subject: &pb.EntityRef{Type: typ, Id: id}, Attribute: claim.Attribute,
			Schema:         &pb.SchemaRef{Name: "integration.claim", Version: 1},
			Provenance:     &pb.Provenance{Source: &pb.Provenance_ConnectorId{ConnectorId: claim.Connector}, SourceTime: timestamppb.New(claim.SourceTime), Confidence: 1},
			IdempotencyKey: typ + "/" + id + "/" + change + "/" + claim.Attribute, Payload: claim.Payload}
		if old := t.claims.Accepted(t.ID, fact.IdempotencyKey); old != nil && !proto.Equal(old.GetFact(), fact) {
			return nil, fmt.Errorf("source claim idempotency conflict")
		}
		facts = append(facts, fact)
	}
	if prepared.origin != "" {
		resolved, failure := t.entityIDs.Resolve(&pb.EntityRef{Type: typ, Id: prepared.origin})
		if failure == nil && (len(resolved) != 1 || resolved[0] != (kernel.Ref{Type: typ, ID: id}) && resolved[0] != (kernel.Ref{Type: typ, ID: prepared.origin})) {
			return nil, fmt.Errorf("source identity already resolves elsewhere")
		}
	}
	return facts, nil
}

func (t *Tenant) applySourceClaims(typ, id, change string, at time.Time, prepared *preparedSourceClaim) error {
	if prepared == nil {
		return nil
	}
	facts, err := t.sourceFacts(typ, id, change, prepared)
	if err != nil {
		return err
	}
	for _, fact := range facts {
		if _, failure := t.claims.Record(fact, at); failure != nil {
			return fmt.Errorf("accepted source claim: %v", failure)
		}
	}
	target := &pb.EntityRef{Type: typ, Id: id}
	if _, failure := t.entityIDs.Resolve(target); failure != nil {
		if failure := t.entityIDs.Create(target); failure != nil {
			return fmt.Errorf("accepted source identity: %v", failure)
		}
	}
	if prepared.origin != "" {
		origin := &pb.EntityRef{Type: typ, Id: prepared.origin}
		resolved, failure := t.entityIDs.Resolve(origin)
		if failure != nil {
			if failure := t.entityIDs.Create(origin); failure != nil {
				return fmt.Errorf("accepted source origin: %v", failure)
			}
			resolved = []kernel.Ref{{Type: typ, ID: prepared.origin}}
		}
		if len(resolved) == 1 && resolved[0] == (kernel.Ref{Type: typ, ID: prepared.origin}) {
			if failure := t.entityIDs.AddRedirect(&pb.Redirect{From: origin, To: []*pb.EntityRef{target}, Kind: pb.RedirectKind_REDIRECT_KIND_MERGE}); failure != nil {
				return fmt.Errorf("accepted source redirect: %v", failure)
			}
		}
	}
	return nil
}
