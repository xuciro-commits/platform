package host

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
	"reflect"
	"time"
)

func (t *Tenant) recordSetBinding(binding platform.AssetBinding) (*platform.Definition, bool) {
	selected := objects.RecordSetBinding(t.definitions, &binding)
	return selected, selected != nil
}

func (t *Tenant) ValidateRecordSet(info platform.EntityInfo, set platform.RecordSet) error {
	return t.objectOwner(t.Projection).ValidateRecordSet(info, set)
}

func (t *Tenant) checkRecordSetDeclarations(info platform.EntityInfo, engines ...platform.RuleEngine) error {
	return t.objectOwner(t.Projection).CheckRecordSetDeclarations(info, engines...)
}

func (r runtime) ReadRecordSet(c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	return r.t.readRecordSetFrom(r.t.Projection, c, object, id, name, at)
}

func (d *stagedDecision) ReadRecordSet(c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	return d.tenant.readRecordSetFrom(d.Records, c, object, id, name, at)
}

func (d *stagedDecision) ReadRecordSetCandidate(c platform.Caller, object, id, name string, raw json.RawMessage, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	store := d.Records.ForkRecords()
	et := store.Type(object)
	if !c.Staging() || et == nil || et.Info().App != c.App || id == "" {
		return platform.RecordSetResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Collection candidate is outside its owner")
	}
	value := reflect.New(et.Info().Go).Elem()
	if json.Unmarshal(raw, value.Addr().Interface()) != nil || records.Header(value).ID != id {
		return platform.RecordSetResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Collection candidate is invalid")
	}
	if before := et.Rows().Get(id); before != nil {
		records.Header(value).Created = records.Header(before.Value).Created
	} else {
		records.Header(value).Created.By = c.ID
	}
	et.SetRow(id, &records.Row{Value: value})
	return d.tenant.readRecordSetFrom(store, c, object, id, name, at)
}

func (t *Tenant) readRecordSetFrom(store *records.Store, c platform.Caller, object, id, name string, at time.Time) (platform.RecordSetResult, *kernel.Error) {
	return t.objectOwner(store).ReadRecordSetFrom(store, c, object, id, name, at)
}
