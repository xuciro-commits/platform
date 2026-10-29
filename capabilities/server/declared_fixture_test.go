package platformserver

import (
	"encoding/json"
	"platformserver/platform"
	"testing"
	"time"
)

func declaredRecord(t *testing.T, tn *Tenant, member, typ, id string, at time.Time) map[string]any {
	t.Helper()
	answer, err := tn.RecordOf(memberOf(t, tn, member), typ, id, at)
	if err != nil {
		t.Fatalf("%s/%s: %v", typ, id, err)
	}
	var record map[string]any
	if err := json.Unmarshal(platform.Raw(answer.Record), &record); err != nil {
		t.Fatal(err)
	}
	return record
}
