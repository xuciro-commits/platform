package host

import (
	"encoding/json"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/objects"
	"time"
)

type RecordPage = objects.RecordPage

func (t *Tenant) recordsRead(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.objectOwner(t.Projection).Records(m, typ, q, now)
}

func (t *Tenant) recordsFrom(store *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.objectOwner(store).RecordsFrom(store, m, typ, q, now)
}

func (t *Tenant) recordSelectionFrom(store *records.Store, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return t.objectOwner(store).SelectionFrom(store, m, typ, q, now)
}

func (t *Tenant) readRecordFrom(store *records.Store, c platform.Caller, typ, id string, now time.Time) (json.RawMessage, *kernel.Error) {
	return t.objectOwner(store).ReadRecordFrom(store, c, typ, id, now)
}
