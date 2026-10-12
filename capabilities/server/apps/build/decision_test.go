package build

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"platformserver/platform"
)

func TestActionDecisionCoreTagsLinesBranchesAndFormulas(t *testing.T) {
	o := Object{
		Name:   "lot",
		Title:  "Lot",
		States: []State{{Name: "queued", Title: "Queued"}, {Name: "active", Title: "Active"}, {Name: "hold", Title: "Hold"}, {Name: "done", Title: "Done"}, {Name: "scrapped", Title: "Scrapped"}},
		Fields: []Field{
			{Name: "operation", Title: "Operation", Type: "integer", Required: true},
			{Name: "maxop", Title: "Max operation", Type: "integer", Required: true},
			{Name: "capabilities", Title: "Capabilities", Type: "tags"},
			{Name: "signatures", Title: "Signatures", Type: "lines", Columns: []LineColumn{
				{Name: "action", Title: "Action", Type: "choice", Choices: "rework,scrap,use"},
				{Name: "meaning", Title: "Meaning", Type: "choice", Choices: "reviewed,approved"},
				{Name: "by", Title: "By", Type: "text"},
			}},
		},
		Actions: []Action{
			{
				New:   true,
				Name:  "release",
				Title: "Release lot",
				To:    "queued",
				Inputs: []Input{
					{Name: "maxop", Title: "Max op", Type: "integer", Required: true},
					{Name: "caps", Title: "Capabilities", Type: "tags"},
				},
				Sets: []Set{
					{Field: "operation", From: "=10"},
					{Field: "maxop", From: "input.maxop"},
					{Field: "capabilities", From: "input.caps"},
				},
			},
			{
				Name:  "start",
				Title: "Start",
				From:  []string{"queued"},
				To:    "active",
				Inputs: []Input{
					{Name: "required", Title: "Required", Type: "tags"},
				},
				Conditions: []Condition{
					{Field: "record.capabilities", Operator: "contains all", ValueField: "input.required", Message: "Missing capability"},
				},
			},
			{
				Name:  "complete",
				Title: "Complete",
				From:  []string{"active"},
				To:    "done",
				Branches: []ActionBranch{
					{
						When: []Condition{{Field: "record.operation", Operator: "<", ValueField: "record.maxop"}},
						To:   "queued",
						Sets: []Set{{Field: "operation", From: "formula:operation + 10"}},
					},
				},
			},
			{
				Name:  "sign",
				Title: "Sign disposition",
				From:  []string{"hold"},
				To:    "hold",
				Inputs: []Input{
					{Name: "action", Title: "Action", Type: "choice", Choices: "rework,scrap,use", Required: true},
					{Name: "meaning", Title: "Meaning", Type: "choice", Choices: "reviewed,approved", Required: true},
					{Name: "reworkop", Title: "Rework op", Type: "integer", Required: true, When: "action=rework"},
				},
				Conditions: []Condition{
					{Quantifier: "none", Collection: "signatures", Where: []Condition{{Field: "by", Operator: "=", Value: "$me"}}, Message: "Already signed"},
					{Quantifier: "none", Collection: "signatures", Where: []Condition{{Field: "action", Operator: "=", ValueField: "input.action"}, {Field: "meaning", Operator: "=", ValueField: "input.meaning"}}, Message: "Meaning already used"},
				},
				Sets: []Set{
					{Field: "signatures", From: "append:action=input.action,meaning=input.meaning,by=$me"},
				},
				Branches: []ActionBranch{
					{
						When: []Condition{
							{Quantifier: "count", Collection: "signatures", Where: []Condition{{Field: "action", Operator: "=", ValueField: "input.action"}}, Operator: ">=", Value: "2"},
							{Field: "input.action", Operator: "=", Value: "rework"},
						},
						To:   "queued",
						Sets: []Set{{Field: "operation", From: "input.reworkop"}, {Field: "signatures", From: "="}},
					},
					{
						When: []Condition{
							{Quantifier: "count", Collection: "signatures", Where: []Condition{{Field: "action", Operator: "=", ValueField: "input.action"}}, Operator: ">=", Value: "2"},
							{Field: "input.action", Operator: "=", Value: "scrap"},
						},
						To:   "scrapped",
						Sets: []Set{{Field: "signatures", From: "="}},
					},
				},
			},
		},
	}

	if err := checkFields(o.Fields, nil); err != nil {
		t.Fatalf("checkFields: %v", err)
	}
	if err := checkProcess(o, nil); err != nil {
		t.Fatalf("checkProcess: %v", err)
	}
	entity := Entity(o)
	info, err := platform.Describe(ID, entity, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if f, ok := info.Field("capabilities"); !ok || f.Type != "tags" {
		t.Fatalf("expected tags field, got %+v", f)
	}
	if f, ok := info.Field("signatures"); !ok || f.Type != "lines" || len(f.Fields) != 3 {
		t.Fatalf("expected lines field with 3 columns, got %+v", f)
	}

	// Test start capability matching and conditional branch step progression.
	type sigLine struct {
		Action  string `json:"action,omitempty"`
		Meaning string `json:"meaning,omitempty"`
		By      string `json:"by,omitempty"`
	}
	type lotRecord struct {
		platform.Record
		Operation    int       `json:"operation,omitempty"`
		Maxop        int       `json:"maxop,omitempty"`
		Capabilities []string  `json:"capabilities,omitempty"`
		Signatures   []sigLine `json:"signatures,omitempty"`
		State        string    `json:"state,omitempty"`
	}

	rec := &lotRecord{Record: platform.Record{ID: "L1"}, Operation: 10, Maxop: 20, Capabilities: []string{"assembly", "torque"}, State: "queued"}
	// Start fails when required capability is missing.
	if err := take(o, o.Actions[1], platform.Caller{Member: platform.Member{ID: "op1"}}, rec, json.RawMessage(`{"required":["assembly","welding"]}`), time.Now(), nil, nil); err == nil {
		t.Fatal("expected start with missing capability to fail")
	}
	// Start succeeds when all required capabilities are present.
	if err := take(o, o.Actions[1], platform.Caller{Member: platform.Member{ID: "op1"}}, rec, json.RawMessage(`{"required":["assembly","torque"]}`), time.Now(), nil, nil); err != nil {
		t.Fatalf("start failed: %v", err)
	}
	rec.State = "active"

	// Complete at operation 10 (< maxop 20) branches to queued and advances operation to 20.
	if err := take(o, o.Actions[2], platform.Caller{Member: platform.Member{ID: "op1"}}, rec, json.RawMessage(`{}`), time.Now(), nil, nil); err != nil {
		t.Fatalf("complete at 10 failed: %v", err)
	}
	if rec.State != "queued" || rec.Operation != 20 {
		t.Fatalf("expected state=queued operation=20, got state=%s operation=%d", rec.State, rec.Operation)
	}

	// Complete at operation 20 (== maxop 20) takes default To="done".
	rec.State = "active"
	if err := take(o, o.Actions[2], platform.Caller{Member: platform.Member{ID: "op1"}}, rec, json.RawMessage(`{}`), time.Now(), nil, nil); err != nil {
		t.Fatalf("complete at 20 failed: %v", err)
	}
	if rec.State != "done" {
		t.Fatalf("expected state=done, got %s", rec.State)
	}

	// Test dual-signature quorum with conditional input `reworkop` (only required when action=rework).
	rec.State = "hold"
	// First signature by qa1 (reviewed, scrap — notice reworkop is omitted and not required because action!=rework!).
	if err := take(o, o.Actions[3], platform.Caller{Member: platform.Member{ID: "qa1"}}, rec, json.RawMessage(`{"action":"scrap","meaning":"reviewed"}`), time.Now(), nil, nil); err != nil {
		t.Fatalf("first signature failed: %v", err)
	}
	if rec.State != "hold" || len(rec.Signatures) != 1 {
		t.Fatalf("expected to stay in hold with 1 signature, got state=%s sigs=%d", rec.State, len(rec.Signatures))
	}
	// Same person signing again is refused.
	if err := take(o, o.Actions[3], platform.Caller{Member: platform.Member{ID: "qa1"}}, rec, json.RawMessage(`{"action":"scrap","meaning":"approved"}`), time.Now(), nil, nil); err == nil {
		t.Fatal("expected duplicate signer to be refused")
	}
	// Second person with same meaning is refused.
	if err := take(o, o.Actions[3], platform.Caller{Member: platform.Member{ID: "qa2"}}, rec, json.RawMessage(`{"action":"scrap","meaning":"reviewed"}`), time.Now(), nil, nil); err == nil {
		t.Fatal("expected duplicate meaning to be refused")
	}
	// Second person with approved completes quorum and branches to scrapped, clearing signatures.
	if err := take(o, o.Actions[3], platform.Caller{Member: platform.Member{ID: "qa2"}}, rec, json.RawMessage(`{"action":"scrap","meaning":"approved"}`), time.Now(), nil, nil); err != nil {
		t.Fatalf("second signature failed: %v", err)
	}
	if rec.State != "scrapped" || len(rec.Signatures) != 0 {
		t.Fatalf("expected state=scrapped and cleared signatures, got state=%s sigs=%d", rec.State, len(rec.Signatures))
	}
}
