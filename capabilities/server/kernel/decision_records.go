package kernel

import (
	"encoding/json"
	"reflect"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (d *Decision) SavedUniqueChanges() []persistence.StateUnique {
	d.Records.Lock()
	defer d.Records.Unlock()
	return UniqueReplacements(d.Records.UniqueChanges())
}

func (d *Decision) FlowVersions() map[string]int {
	if private, ok := d.PrivateApps[platform.FlowApp].(platform.Processes); ok {
		return private.Versions()
	}
	return nil
}

func (d *Decision) SavedAuthorizations() ([]acceptedformat.AuthorizationEvidence, error) {
	var saved []acceptedformat.AuthorizationEvidence
	seen := map[string]bool{}
	for _, item := range d.Authorizations {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		key := string(raw)
		if !seen[key] {
			seen[key] = true
			saved = append(saved, item)
		}
	}
	return saved, nil
}

func (d *Decision) Get(c platform.Caller, typ reflect.Type, id string) (any, bool) {
	return d.Records.Get(c, typ, id)
}

func (d *Decision) Check(c platform.Caller, entity any) *kernel.Error {
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	return d.Records.Check(c, entity, at)
}
