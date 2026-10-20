package records

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"reflect"
)

func Write(info platform.EntityInfo, receipt *pb.ChangeRecord, prev *Row, v reflect.Value) (*Row, *kernel.Error) {
	rec := Header(v)
	stamp := platform.Stamp{By: receipt.GetSubmission().GetPrincipalId(), At: receipt.GetRecordedTime().AsTime(), Change: receipt.GetChangeId()}
	change := RecordChange{Change: receipt.GetChangeId(), Schema: receipt.GetSubmission().GetSchema().GetName(), By: stamp.By, At: stamp.At, Fields: []FieldChange{}}
	// The revision is the kernel's (K4 C12): the decisions naming the record as
	// their target, which clients send back as the revision they saw.
	rec.Changed, rec.Created, rec.Revision = stamp, stamp, 0
	target := receipt.GetSubmission().GetTarget()
	named := target.GetType() == info.Type && target.GetId() == rec.ID
	if l := info.Lifecycle; prev == nil && l != nil { // a new record starts in the initial state
		if f, _ := info.Field(l.Field); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(l.Initial)
		}
	}
	if owner := info.Scope.Owner; prev == nil && owner != "" && owner != platform.OwnerCreated { // a new record belongs to its creator unless the rules say who
		if f, _ := info.Field(owner); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(stamp.By)
		}
	}
	if prev != nil {
		old := Header(prev.Value)
		rec.Created, rec.Revision = old.Created, old.Revision
		if old.Archived != rec.Archived {
			change.Fields = append(change.Fields, FieldChange{Field: "archived", Before: rowJSON(old.Archived), After: rowJSON(rec.Archived)})
		}
	}
	for _, f := range info.Fields {
		after := rowJSON(v.FieldByIndex(f.Index).Interface())
		var before json.RawMessage
		if prev != nil {
			before = rowJSON(prev.Value.FieldByIndex(f.Index).Interface())
		}
		if string(before) != string(after) && !(prev == nil && v.FieldByIndex(f.Index).IsZero()) {
			change.Fields = append(change.Fields, FieldChange{Field: f.Name, Before: before, After: after})
		}
	}
	if named {
		rec.Revision = receipt.GetRevision()
	}
	var projection *Projection
	var extraction *Extraction
	if prev != nil {
		extraction = prev.Extraction
		projection = CloneProjection(prev.Projection)
		if err := RefreshProjection(projection, v.Interface()); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
	}
	next := &Row{Value: v, Format: info.RecordFormat, Projection: projection, Extraction: extraction}
	if err := AppendRowHistory(next, prev, []RecordChange{change}); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return next, nil
}
