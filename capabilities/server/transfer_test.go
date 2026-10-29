package platformserver

import (
	"encoding/json"
	"platformserver/platform"
	"platformserver/studio/build"
	"strings"
	"testing"
	"time"
)

func TestRecordImportChoosesPartialEditsAndRetainsOriginalFileReceipts(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "import-owner", []platform.Seat{seatOf("builder", "build:builder"), seatOf("user", "build:user"), seatOf("other", "build:user")}, build.New("import-owner"))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	typ := publishObject(t, tn, "builder", "object", map[string]any{"name": "intake", "title": "Intake", "fields": []map[string]any{{"name": "name", "title": "Name", "type": "text", "required": true}, {"name": "quantity", "title": "Quantity", "type": "integer", "required": true}, {"name": "active", "title": "Active", "type": "boolean", "required": true}}, "access": []map[string]any{{"role": "user", "read": "own", "create": true, "edit": true}}}, at)
	user := memberOf(t, tn, "user")
	file := []byte("id,name,quantity,active\nR1,Original,0,false\n")
	check := func(file []byte, preview bool, action, outcome string) {
		t.Helper()
		rows, problem := tn.Import(user, typ, file, preview, at)
		if problem != nil || len(rows) != 1 || rows[0].Action != typ+action || rows[0].Outcome != outcome {
			t.Fatalf("import preview=%v: %+v %v", preview, rows, problem)
		}
	}
	check(file, true, ".create", "ok")
	if rows, problem := tn.Records(user, typ, platform.Query{}, at); problem != nil || rows.Total != 0 {
		t.Fatal("import preview changed live records")
	}
	check(file, false, ".create", "ok")
	read := func() map[string]any {
		t.Helper()
		view, problem := tn.RecordOf(user, typ, "R1", at)
		if problem != nil {
			t.Fatal(problem)
		}
		var row map[string]any
		if err := json.Unmarshal(platform.Raw(view.Record), &row); err != nil {
			t.Fatal(err)
		}
		return row
	}
	if row := read(); row["revision"] != float64(1) {
		t.Fatalf("created revision: %+v", row)
	}
	edit := []byte("id,name\nR1,Revised\n")
	check(edit, true, ".edit", "ok")
	check(edit, false, ".edit", "ok")
	check(edit, false, ".edit", "ok")
	check(file, false, ".create", "ok")
	if row := read(); row["name"] != "Revised" || row["revision"] != float64(2) {
		t.Fatalf("a repeated file rewrote the record: %+v", row)
	}
	check([]byte("id,name\nR2,Missing required inputs\n"), false, ".create", "ERROR_CODE_INVALID_ARGUMENT")
	rows, problem := tn.Import(memberOf(t, tn, "other"), typ, []byte("id,name,quantity,active\nR1,Unauthorized,1,true\n"), false, at)
	if problem != nil || len(rows) != 1 || rows[0].Outcome == "ok" || read()["name"] != "Revised" {
		t.Fatal("an import edited an unreadable record")
	}
	exported, problem := tn.Export(user, typ, platform.Query{}, at)
	if problem != nil || !strings.Contains(string(exported), "Revised") {
		t.Fatal("authorized export lost the imported edit")
	}
	CheckReplay(t, tn, entries, compose)
}
