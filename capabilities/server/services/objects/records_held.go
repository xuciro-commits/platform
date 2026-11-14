package objects

import (
	"encoding/json"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (o *Owner) HeldRecordJSON(c platform.Caller, typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
	entity := o.BaseStore.Type(typ)
	if entity == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path object is unavailable")
	}
	visible, refusal := o.visibleForCallerIn(o.BaseStore, c, entity, at)
	if refusal != nil {
		return nil, refusal
	}
	row := entity.Rows().Get(id)
	if row == nil || visible != nil && !visible(row.Value) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path record is unavailable")
	}
	_, hidden := ViewOf(c.Member, entity)
	record := records.Mask(entity.Info(), row.Value, hidden)
	if entity.Info().HasReadSources() || len(entity.Info().VersionPins) > 0 {
		narrowed, _ := o.narrowerIn(o.BaseStore, c.Member, at, true).Derive(entity, reflect.ValueOf(record))
		record = narrowed.Interface()
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return raw, nil
}
