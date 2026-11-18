package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"time"
)

func (c Caller) ReadQuery(app, name string, inputs json.RawMessage, now time.Time) (json.RawMessage, *Error) {
	if c.rt != nil {
		return c.rt.ReadQuery(c, app, name, inputs, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Named queries require the host read path")
}

func (c Caller) ReadQueryVersion(app, name string, version int, inputs json.RawMessage, now time.Time) (json.RawMessage, *Error) {
	if c.rt != nil {
		return c.rt.ReadQueryVersion(c, app, name, version, inputs, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Retained queries require the host read path")
}

func (c Caller) ReadRecord(typ, id string, now time.Time) (json.RawMessage, *Error) {
	if c.rt != nil {
		return c.rt.ReadRecord(c, typ, id, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Record bindings require the host read path")
}

func (c Caller) ReadRecordPath(typ, id string, path []string, now time.Time) (json.RawMessage, []string, *Error) {
	if c.rt != nil {
		return c.rt.ReadRecordPath(c, typ, id, path, now)
	}
	return nil, nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Record paths require the host's scoped reads")
}

func (c Caller) RequestApproval(app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *Error) {
	if c.rt != nil {
		return c.rt.RequestApproval(c, app, s, now)
	}
	return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Approval requests require an accepted decision")
}

type RecordImageFormat struct {
	Format  string
	Model   any
	Convert func(json.RawMessage) (json.RawMessage, error)
}

func RecordPathField(typ string, path []string, entity func(string) (EntityInfo, bool)) (FieldInfo, error) {
	if len(path) == 0 || len(path) > 16 {
		return FieldInfo{}, fmt.Errorf("a record path needs 1–16 declared fields")
	}
	var field FieldInfo
	for i, name := range path {
		info, known := entity(typ)
		var found bool
		field, found = info.Field(name)
		if !known || !found {
			return field, fmt.Errorf("record path field %s.%s is unavailable", typ, name)
		}
		if i < len(path)-1 {
			if field.Type != "reference" || field.Ref == "" {
				return field, fmt.Errorf("record path field %s.%s is not a single-record reference", typ, name)
			}
			typ = field.Ref
		}
	}
	return field, nil
}

const RecordSetMaxRows = ExprMaxSet
const RecordSetMaxDeclarations = 64

type RecordSet struct {
	BusinessTime *BusinessTime   `json:"businessTime,omitempty"`
	Name         string          `json:"name"`
	From         string          `json:"from"`
	Binding      *AssetBinding   `json:"binding,omitempty"`
	Direction    string          `json:"direction,omitempty"`
	Object       string          `json:"object"`
	Path         []string        `json:"path,omitempty"`
	Where        []Expr          `json:"where,omitempty"`
	Order        []RecordSetSort `json:"order"`
	Limit        int             `json:"limit"`
}
type RecordSetSort struct {
	Expr Expr `json:"expr"`
	Desc bool `json:"desc,omitempty"`
}

type RecordSetResult struct {
	Authorization *AuthzCollection  `json:"authorization,omitempty"`
	Object        string            `json:"object"`
	Records       []json.RawMessage `json:"records"`
	Partial       bool              `json:"partial"`
	Sources       []string          `json:"sources,omitempty"`
	Definition    string            `json:"definition"`
}

type RecordSetRef struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Name   string `json:"name"`
}

func (c Caller) ReadRecordSet(object, id, name string, at time.Time) (RecordSetResult, *Error) {
	if c.rt == nil {
		return RecordSetResult{}, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Declared record sets require the host")
	}
	return c.rt.ReadRecordSet(c, object, id, name, at)
}

func (s *RecordSet) UnmarshalJSON(raw []byte) error {
	type plain RecordSet
	return strictTxn(raw, (*plain)(s))
}
