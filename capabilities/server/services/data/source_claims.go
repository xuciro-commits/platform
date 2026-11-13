package data

import (
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type SourceApplication struct {
	Binding *platform.DataSourceBinding
	Row     SourceRow
	Deleted bool
}

type SourceRecord struct {
	History  []records.RecordChange
	Fields   []platform.FieldInfo
	Archived bool
	Change   string
}

type SelectedSourceValue struct {
	Kind       string
	Value      json.RawMessage
	Connector  string
	SourceTime *time.Time
}

type SourcePrepared struct {
	Submission *pb.Submission
	Origin     string
	Claims     []acceptedformat.SourceClaim
	Restore    bool
}

type SourcePreparation struct {
	Record    func(typ, id string, required bool) (SourceRecord, *kernel.Error)
	Objects   func(object string) (platform.EntityInfo, map[string]bool, *kernel.Error)
	Authorize func(m platform.Member, app, typ, id, action, kind string, fields []string, at time.Time) *kernel.Error
	Selected  func(typ, id, attribute string, history []records.RecordChange, incoming *acceptedformat.SourceClaim) SelectedSourceValue
	Restores  func(typ, id, change string) bool
	Deletes   func(typ, id, connector string) bool
	Claimed   func(object, id, connector string, info platform.EntityInfo) bool
}

func (s SourcePreparation) Prepare(m platform.Member, submission *pb.Submission, source *SourceApplication, now time.Time) (*SourcePrepared, *kernel.Error) {
	if source.Deleted {
		return s.prepareDeletion(m, submission, source, now)
	}
	values, attributes, refusal := SourceAttributes(submission.GetPayload(), source.Binding, source.Row)
	if refusal != nil {
		return nil, refusal
	}
	kind := "write"
	if strings.HasSuffix(submission.GetSchema().GetName(), ".create") {
		kind = "create"
	}
	if refusal := s.Authorize(m, submission.GetAuthority(), submission.GetTarget().GetType(), submission.GetTarget().GetId(), submission.GetSchema().GetName(), kind, attributes, now); refusal != nil {
		return nil, refusal
	}
	record, refusal := s.Record(submission.GetTarget().GetType(), submission.GetTarget().GetId(), false)
	if refusal != nil {
		return nil, refusal
	}
	restore := record.Archived && s.Restores(submission.GetTarget().GetType(), submission.GetTarget().GetId(), record.Change)
	claims := make([]acceptedformat.SourceClaim, 0, len(attributes))
	payload := map[string]json.RawMessage{}
	for _, attribute := range attributes {
		claim := acceptedformat.SourceClaim{Attribute: attribute, Connector: SourceConnector(source.Binding), SourceTime: *source.Row.At, Payload: values[attribute]}
		claims = append(claims, claim)
		selected := s.Selected(submission.GetTarget().GetType(), submission.GetTarget().GetId(), attribute, record.History, &claim)
		if selected.Kind != "" {
			payload[attribute] = selected.Value
		}
	}
	claims = append(claims, acceptedformat.SourceClaim{Attribute: "$record", Connector: SourceConnector(source.Binding), SourceTime: *source.Row.At, Payload: json.RawMessage("true")})
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source row cannot be encoded")
	}
	copy := proto.Clone(submission).(*pb.Submission)
	copy.Payload = encoded
	origin := source.Row.Origin
	if origin == copy.GetTarget().GetId() {
		origin = ""
	}
	return &SourcePrepared{Submission: copy, Origin: origin, Claims: claims, Restore: restore}, nil
}

func (s SourcePreparation) prepareDeletion(m platform.Member, submission *pb.Submission, source *SourceApplication, now time.Time) (*SourcePrepared, *kernel.Error) {
	typ, id := submission.GetTarget().GetType(), submission.GetTarget().GetId()
	record, refusal := s.Record(typ, id, true)
	if refusal != nil {
		return nil, refusal
	}
	connector := SourceConnector(source.Binding)
	copy := proto.Clone(submission).(*pb.Submission)
	verb := "edit"
	if s.Deletes(typ, id, connector) {
		verb = "archive"
	}
	copy.Schema = &pb.SchemaRef{Name: typ + "." + verb, Version: 1}
	copy.Payload = json.RawMessage("{}")
	if refusal := s.Authorize(m, submission.GetAuthority(), typ, id, copy.GetSchema().GetName(), "write", nil, now); refusal != nil {
		return nil, refusal
	}
	at := now
	if source.Row.At != nil {
		at = *source.Row.At
	}
	claim := acceptedformat.SourceClaim{Attribute: "$record", Connector: connector, SourceTime: at, Payload: json.RawMessage("false")}
	claims := []acceptedformat.SourceClaim{claim}
	if verb == "edit" {
		payload := map[string]json.RawMessage{}
		for _, field := range record.Fields {
			selected := s.Selected(typ, id, field.Name, record.History, &claim)
			if selected.Kind == "source" {
				payload[field.Name] = selected.Value
				claims = append(claims, acceptedformat.SourceClaim{Attribute: field.Name, Connector: selected.Connector, SourceTime: *selected.SourceTime, Payload: selected.Value})
			}
		}
		copy.Payload = platform.Raw(payload)
	}
	return &SourcePrepared{Submission: copy, Claims: claims}, nil
}

func (s SourcePreparation) Absent(object string, binding *platform.DataSourceBinding, current []SourceRow, now time.Time) ([]SourceRow, *kernel.Error) {
	present, complete := PresentSourceRows(current)
	if !complete {
		return nil, nil
	}
	connector := SourceConnector(binding)
	info, rows, refusal := s.Objects(object)
	if refusal != nil {
		return nil, refusal
	}
	var ids []string
	for id, archived := range rows {
		if !present[id] && !archived && s.Claimed(object, id, connector, info) {
			ids = append(ids, id)
		}
	}
	return DeletionRows(ids, now), nil
}
