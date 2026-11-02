package host

import (
	"context"
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
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
)

type sourceSubmission struct {
	binding *platform.DataSourceBinding
	row     build.SourceRow
	deleted bool
}

type preparedSourceClaim struct {
	restore bool
	origin  string
	claims  []acceptedformat.SourceClaim
}

type RecordFieldProvenance = objects.RecordFieldProvenance

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
	answer := RecordProvenance{Fields: map[string]RecordFieldProvenance{}}
	for _, field := range info.Fields {
		if field.Name == "" || field.Name == "id" {
			continue
		}
		decision := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "read", Type: typ, Record: id, Fields: []string{field.Name}}, now, false)
		if !decision.Allow || len(decision.Withheld) > 0 {
			continue
		}
		if selected := t.selectedSourceValue(typ, id, field.Name, history, nil); selected.Kind != "" {
			answer.Fields[field.Name] = selected
		}
	}
	return answer, nil
}

func (t *Tenant) selectedSourceValue(typ, id, attribute string, history []records.RecordChange, incoming *acceptedformat.SourceClaim) RecordFieldProvenance {
	ref := &pb.EntityRef{Type: typ, Id: id}
	sources := []RecordFieldProvenance{}
	absent := map[string]bool{}
	for _, record := range t.claims.CurrentClaims(t.ID, ref, "$record") {
		fact := record.GetFact()
		absent[fact.GetProvenance().GetConnectorId()] = string(fact.GetPayload()) == "false"
	}
	for _, record := range t.claims.CurrentClaims(t.ID, ref, attribute) {
		fact := record.GetFact()
		connector := fact.GetProvenance().GetConnectorId()
		if absent[connector] || incoming != nil && connector == incoming.Connector {
			continue
		}
		at := fact.GetProvenance().GetSourceTime().AsTime()
		sources = append(sources, RecordFieldProvenance{Value: fact.GetPayload(), Kind: "source", Connector: fact.GetProvenance().GetConnectorId(), SourceTime: &at})
	}
	if incoming != nil && incoming.Attribute == attribute {
		at := incoming.SourceTime
		sources = append(sources, RecordFieldProvenance{Value: incoming.Payload, Kind: "source", Connector: incoming.Connector, SourceTime: &at})
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
				edit = RecordFieldProvenance{Value: field.After, Kind: "edit", At: &at, By: change.By}
				break
			}
		}
	}
	return objects.MergeAttribute(sources, edit)
}

func (t *Tenant) prepareSourceSubmission(m platform.Member, sub *pb.Submission, source *sourceSubmission, now time.Time) (*pb.Submission, *preparedSourceClaim, *kernel.Error) {
	if source.deleted {
		projected, claims, refusal := t.prepareSourceDeletion(sub, source, now)
		if refusal != nil {
			return nil, nil, refusal
		}
		caller := platform.NewCaller(runtime{t}, m, sub.GetAuthority(), false, false)
		decision := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "write", Type: sub.GetTarget().GetType(), Record: sub.GetTarget().GetId(), Action: projected.GetSchema().GetName()}, now, false)
		if !decision.Allow {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, decision.Reason)
		}
		return projected, claims, nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(sub.GetPayload(), &values); err != nil || len(values) == 0 || source.row.At == nil {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source row has no mapped values or source time")
	}
	mapped := map[string]bool{}
	for _, column := range source.binding.Columns {
		mapped[column.Attribute] = true
	}
	attributes := make([]string, 0, len(values))
	for name := range values {
		if !mapped[name] {
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
	decision := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: kind, Type: sub.GetTarget().GetType(), Record: sub.GetTarget().GetId(), Action: sub.GetSchema().GetName(), Fields: attributes}, now, false)
	if !decision.Allow || len(decision.Withheld) > 0 {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, decision.Reason)
	}
	t.records.Lock()
	et := t.records.Type(sub.GetTarget().GetType())
	var history []records.RecordChange
	restore := false
	if et != nil {
		if row := et.Rows().Get(sub.GetTarget().GetId()); row != nil {
			history = row.RecordHistory()
			header := records.Header(row.Value)
			if header.Archived {
				if claim := t.claims.Accepted(t.ID, sub.GetTarget().GetType()+"/"+sub.GetTarget().GetId()+"/"+header.Changed.Change+"/$record"); claim != nil {
					restore = string(claim.GetFact().GetPayload()) == "false"
				}
			}
		}
	}
	t.records.Unlock()
	claims := make([]acceptedformat.SourceClaim, 0, len(attributes))
	payload := map[string]json.RawMessage{}
	for _, attribute := range attributes {
		claim := acceptedformat.SourceClaim{Attribute: attribute, Connector: source.binding.Kind + ":" + source.binding.Source, SourceTime: *source.row.At, Payload: values[attribute]}
		claims = append(claims, claim)
		selected := t.selectedSourceValue(sub.GetTarget().GetType(), sub.GetTarget().GetId(), attribute, history, &claim)
		if selected.Kind != "" {
			payload[attribute] = selected.Value
		}
	}
	claims = append(claims, acceptedformat.SourceClaim{Attribute: "$record", Connector: source.binding.Kind + ":" + source.binding.Source, SourceTime: *source.row.At, Payload: json.RawMessage("true")})
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
	return copy, &preparedSourceClaim{origin: origin, claims: claims, restore: restore}, nil
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

func (t *Tenant) prepareSourceDeletion(sub *pb.Submission, source *sourceSubmission, now time.Time) (*pb.Submission, *preparedSourceClaim, *kernel.Error) {
	typ, id := sub.GetTarget().GetType(), sub.GetTarget().GetId()
	t.records.Lock()
	et := t.records.Type(typ)
	row := et.Rows().Get(id)
	created := records.Header(row.Value).Created.Change
	history := row.RecordHistory()
	fields := et.Info().Fields
	bindings := et.Info().DataSources
	t.records.Unlock()
	connector := source.binding.Kind + ":" + source.binding.Source
	createdByAction := true
	for _, field := range fields {
		if t.claims.Accepted(t.ID, typ+"/"+id+"/"+created+"/"+field.Name) != nil {
			createdByAction = false
			break
		}
	}
	if t.claims.Accepted(t.ID, typ+"/"+id+"/"+created+"/$record") != nil {
		createdByAction = false
	}
	presence := map[string]bool{}
	for _, claim := range t.claims.CurrentClaims(t.ID, sub.GetTarget(), "$record") {
		presence[claim.GetFact().GetProvenance().GetConnectorId()] = string(claim.GetFact().GetPayload()) == "true"
	}
	otherPresent := false
	for other, live := range presence {
		otherPresent = otherPresent || other != connector && live
	}
	for _, binding := range bindings {
		other := binding.Kind + ":" + binding.Source
		if other == connector {
			continue
		}
		live, known := presence[other]
		if !known {
			for _, field := range fields {
				if slices.ContainsFunc(t.claims.CurrentClaims(t.ID, sub.GetTarget(), field.Name), func(claim *pb.FactRecord) bool { return claim.GetFact().GetProvenance().GetConnectorId() == other }) {
					live = true
					break
				}
			}
		}
		otherPresent = otherPresent || live
	}
	copy := proto.Clone(sub).(*pb.Submission)
	verb := "edit"
	if objects.SourceDeletionRemovesObject(createdByAction, otherPresent) {
		verb = "archive"
	}
	copy.Schema = &pb.SchemaRef{Name: typ + "." + verb, Version: 1}
	copy.Payload = json.RawMessage("{}")
	at := now
	if source.row.At != nil {
		at = *source.row.At
	}
	claim := acceptedformat.SourceClaim{Attribute: "$record", Connector: connector, SourceTime: at, Payload: json.RawMessage("false")}
	claims := []acceptedformat.SourceClaim{claim}
	if verb == "edit" {
		payload := map[string]json.RawMessage{}
		for _, field := range fields {
			if selected := t.selectedSourceValue(typ, id, field.Name, history, &claim); selected.Kind == "source" {
				payload[field.Name] = selected.Value
				claims = append(claims, acceptedformat.SourceClaim{Attribute: field.Name, Connector: selected.Connector, SourceTime: *selected.SourceTime, Payload: selected.Value})
			}
		}
		copy.Payload = platform.Raw(payload)
	}
	return copy, &preparedSourceClaim{claims: claims}, nil
}

func (t *Tenant) absentSourceRows(object string, binding *platform.DataSourceBinding, current []build.SourceRow, now time.Time) ([]build.SourceRow, *kernel.Error) {
	return recordRead(t, context.Background(), func() ([]build.SourceRow, *kernel.Error) {
		present := map[string]bool{}
		for _, row := range current {
			if row.Error != "" {
				return nil, nil
			}
			present[row.ID] = true
		}
		connector := binding.Kind + ":" + binding.Source
		var removed []build.SourceRow
		t.records.Lock()
		defer t.records.Unlock()
		et := t.records.Type(object)
		if et == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The target object is no longer installed")
		}
		for id, row := range et.Rows().All() {
			if present[id] || records.Header(row.Value).Archived {
				continue
			}
			ref := &pb.EntityRef{Type: object, Id: id}
			live, known := false, false
			for _, claim := range t.claims.CurrentClaims(t.ID, ref, "$record") {
				if claim.GetFact().GetProvenance().GetConnectorId() == connector {
					live, known = string(claim.GetFact().GetPayload()) == "true", true
				}
			}
			if !known {
				for _, field := range et.Info().Fields {
					live = live || slices.ContainsFunc(t.claims.CurrentClaims(t.ID, ref, field.Name), func(claim *pb.FactRecord) bool {
						return claim.GetFact().GetProvenance().GetConnectorId() == connector
					})
				}
			}
			if live {
				removed = append(removed, build.SourceRow{ID: id, Verb: ".edit", Payload: json.RawMessage("{}"), Key: "delete:" + id, At: &now, Deleted: true})
			}
		}
		slices.SortFunc(removed, func(a, b build.SourceRow) int { return strings.Compare(a.ID, b.ID) })
		return removed, nil
	})
}
