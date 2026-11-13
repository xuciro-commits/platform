package objects

import (
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	acceptedformat "platformserver/kernel/acceptedformat"
)

type RecordProvenance struct {
	Fields map[string]RecordFieldProvenance `json:"fields"`
}

func (t *Owner) SourceState(typ, id string, required bool) ([]records.RecordChange, []platform.FieldInfo, bool, string, *kernel.Error) {
	t.Store.Lock()
	defer t.Store.Unlock()
	entity := t.Store.Type(typ)
	if entity == nil {
		if required {
			return nil, nil, false, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The target object is no longer installed")
		}
		return nil, nil, false, "", nil
	}
	fields := entity.Info().Fields
	row := entity.Rows().Get(id)
	if row == nil {
		if required {
			return nil, fields, false, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The target object is no longer installed")
		}
		return nil, fields, false, "", nil
	}
	header := records.Header(row.Value)
	return row.RecordHistory(), fields, header.Archived, header.Changed.Change, nil
}

func (t *Owner) SourceObjectStates(object string) (platform.EntityInfo, map[string]bool, *kernel.Error) {
	t.Store.Lock()
	defer t.Store.Unlock()
	entity := t.Store.Type(object)
	if entity == nil {
		return platform.EntityInfo{}, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The target object is no longer installed")
	}
	rows := map[string]bool{}
	for id, row := range entity.Rows().All() {
		rows[id] = records.Header(row.Value).Archived
	}
	return entity.Info(), rows, nil
}

func (t *Owner) RecordProvenance(m platform.Member, typ, id string, now time.Time) (RecordProvenance, *kernel.Error) {
	m = t.authorizationMember(m)
	caller := t.caller(m, "")
	if _, refusal := t.readRecordFrom(t.Store, caller, typ, id, now); refusal != nil {
		return RecordProvenance{}, refusal
	}
	t.Store.Lock()
	et := t.Store.Type(typ)
	row := et.Rows().Get(id)
	history := row.RecordHistory()
	info := et.Info()
	t.Store.Unlock()
	answer := RecordProvenance{Fields: map[string]RecordFieldProvenance{}}
	for _, field := range info.Fields {
		if field.Name == "" || field.Name == "id" {
			continue
		}
		decision := t.authorizeFrom(t.Store, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.TenantID, Kind: "read", Type: typ, Record: id, Fields: []string{field.Name}}, now, false)
		if !decision.Allow || len(decision.Withheld) > 0 {
			continue
		}
		if selected := t.selectedSourceValue(typ, id, field.Name, history, nil); selected.Kind != "" {
			answer.Fields[field.Name] = selected
		}
	}
	return answer, nil
}

func (t *Owner) selectedSourceValue(typ, id, attribute string, history []records.RecordChange, incoming *acceptedformat.SourceClaim) RecordFieldProvenance {
	ref := &pb.EntityRef{Type: typ, Id: id}
	sources := []RecordFieldProvenance{}
	absent := map[string]bool{}
	for _, record := range t.Claims.CurrentClaims(t.TenantID, ref, "$record") {
		fact := record.GetFact()
		absent[fact.GetProvenance().GetConnectorId()] = string(fact.GetPayload()) == "false"
	}
	for _, record := range t.Claims.CurrentClaims(t.TenantID, ref, attribute) {
		fact := record.GetFact()
		connector := fact.GetProvenance().GetConnectorId()
		if absent[connector] || incoming != nil && connector == incoming.Connector {
			continue
		}
		at := fact.GetProvenance().GetSourceTime().AsTime()
		sources = append(sources, RecordFieldProvenance{Value: fact.GetPayload(), Kind: "source", Connector: connector, SourceTime: &at})
	}
	if incoming != nil && incoming.Attribute == attribute {
		at := incoming.SourceTime
		sources = append(sources, RecordFieldProvenance{Value: incoming.Payload, Kind: "source", Connector: incoming.Connector, SourceTime: &at})
	}
	var edit RecordFieldProvenance
	for i := len(history) - 1; i >= 0 && edit.Kind == ""; i-- {
		change := history[i]
		if t.Claims.Accepted(t.TenantID, typ+"/"+id+"/"+change.Change+"/"+attribute) != nil {
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
	return MergeAttribute(sources, edit)
}

func (t *Owner) SelectedSourceValue(typ, id, attribute string, history []records.RecordChange, incoming *acceptedformat.SourceClaim) RecordFieldProvenance {
	return t.selectedSourceValue(typ, id, attribute, history, incoming)
}

func (t *Owner) SourceDeletionRemoves(typ, id, connector string) bool {
	t.Store.Lock()
	et := t.Store.Type(typ)
	row := et.Rows().Get(id)
	created := records.Header(row.Value).Created.Change
	fields := et.Info().Fields
	bindings := et.Info().DataSources
	t.Store.Unlock()
	ref := &pb.EntityRef{Type: typ, Id: id}
	createdByAction := true
	for _, field := range fields {
		if t.Claims.Accepted(t.TenantID, typ+"/"+id+"/"+created+"/"+field.Name) != nil {
			createdByAction = false
			break
		}
	}
	if t.Claims.Accepted(t.TenantID, typ+"/"+id+"/"+created+"/$record") != nil {
		createdByAction = false
	}
	presence := map[string]bool{}
	for _, claim := range t.Claims.CurrentClaims(t.TenantID, ref, "$record") {
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
				if slices.ContainsFunc(t.Claims.CurrentClaims(t.TenantID, ref, field.Name), func(claim *pb.FactRecord) bool {
					return claim.GetFact().GetProvenance().GetConnectorId() == other
				}) {
					live = true
					break
				}
			}
		}
		otherPresent = otherPresent || live
	}
	return SourceDeletionRemovesObject(createdByAction, otherPresent)
}

func (t *Owner) SourceRestores(typ, id, change string) bool {
	claim := t.Claims.Accepted(t.TenantID, typ+"/"+id+"/"+change+"/$record")
	return claim != nil && string(claim.GetFact().GetPayload()) == "false"
}
