package platformserver

import (
	"encoding/json"

	"platformserver/kernel/records"
	"platformserver/platform"

	"strings"
)

func (t *Tenant) Changed(e platform.Event) []string {
	t.records.Lock()
	defer t.records.Unlock()
	return t.records.Changed(e.App + "/" + e.Record.GetChangeId())
}

func (t *Tenant) held(ref string) (any, bool) {
	typ, id, _ := strings.Cut(ref, "/")
	t.records.Lock()
	defer t.records.Unlock()
	et := t.records.Type(typ)
	if et == nil {
		return nil, false
	}
	row := et.Rows().Get(id)
	if row == nil {
		return nil, false
	}
	return records.Copy(et.Info().Go, row.Value.Interface()).Interface(), true
}

func jsonOf(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
