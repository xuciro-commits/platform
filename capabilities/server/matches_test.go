package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

// SAP materials and MES parts are one material: a published matching rule
// makes MES rows an edit of the SAP record when the part number agrees by
// digits, and keeps SAP's description while MES may fill the weight.
func TestMatchingRuleConvergesMaterials(t *testing.T) {
	tn := composeTenant(t, "mdm", []Seat{seatOf("dana", "build:builder")}, build.New("mdm"))
	member := memberOf(t, tn, "dana")
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	submit := func(schema, typ, target, payload string) {
		t.Helper()
		decide(t, tn, member.ID, construction.ID, schema, typ, target, json.RawMessage(payload), at)
	}
	submit(construction.ObjectType+".create", construction.ObjectType, "O1", `{"name":"material","title":"Material","fields":[{"name":"partno","title":"Part no","type":"text"},{"name":"description","title":"Description","type":"text"},{"name":"weight","title":"Weight","type":"decimal"}]}`)
	submit(construction.SchemaPublish, construction.ObjectType, "O1", `{}`)
	submit(build.MatchType+".create", build.MatchType, "M1", `{"name":"materialmatch","title":"Same material","object":"build.material","keys":[{"fields":["partno"],"normalize":"digits"}],"prefer":[{"field":"description","producer":"sapmaterials"}]}`)
	submit(build.MatchType+".publish", build.MatchType, "M1", `{}`)
	if refuse(t, tn, member.ID, construction.ID, build.MatchType+".edit", build.MatchType, "M1", json.RawMessage(`{"keys":[{"fields":["unknown"],"normalize":"digits"}]}`), at) == "ok" {
		t.Fatal("edited a published matching rule past its field validation")
	}
	for _, ds := range []string{"sap", "mes"} {
		submit(construction.DatasetType+".create", construction.DatasetType, ds, `{"name":"`+ds+`","title":"`+ds+`"}`)
	}
	submit(construction.PipelineType+".create", construction.PipelineType, "P1", `{"name":"sapmaterials","title":"SAP materials","input":"sap","outputObject":"build.material","key":"matnr"}`)
	submit(construction.PipelineType+".publish", construction.PipelineType, "P1", `{}`)
	submit(construction.PipelineType+".create", construction.PipelineType, "P2", `{"name":"mesparts","title":"MES parts","input":"mes","outputObject":"build.material","key":"part_id"}`)
	submit(construction.PipelineType+".publish", construction.PipelineType, "P2", `{}`)
	submit(construction.SchemaDatasetLoad, construction.DatasetType, "sap", `{"rows":[{"matnr":"000000123","partno":"000000123","description":"Hex bolt M8"},{"matnr":"000000456","partno":"000000456","description":"Washer"}]}`)
	submit(construction.PipelineType+".run", construction.PipelineType, "P1", `{}`)
	tn.RunPipelines(at)
	later := at.Add(time.Hour)
	decide(t, tn, member.ID, construction.ID, construction.SchemaDatasetLoad, construction.DatasetType, "mes", json.RawMessage(`{"rows":[{"part_id":"MES-1","partno":"M-123","description":"BOLT HEX M8 (MES)","weight":0.012},{"part_id":"MES-2","partno":"789","description":"Nut"}]}`), later)
	decide(t, tn, member.ID, construction.ID, construction.PipelineType+".run", construction.PipelineType, "P2", map[string]any{}, later)
	tn.RunPipelines(later)
	page, rerr := tn.Records(member, "build.material", platform.Query{Limit: 50, Sort: []string{"id"}}, later)
	if rerr != nil {
		t.Fatalf("records: %s %s", rerr.Code, rerr.Message)
	}
	if page.Total != 3 {
		t.Fatalf("want 3 materials (123 merged, 456, MES-2 new), got %d: %+v", page.Total, page.Records)
	}
	var bolt map[string]any
	for _, r := range page.Records {
		m := toMap(r)
		if m["id"] == "000000123" {
			bolt = m
		}
	}
	if bolt == nil || bolt["description"] != "Hex bolt M8" || bolt["weight"] != 0.012 {
		t.Fatalf("the SAP record keeps SAP's description and gains MES's weight: %+v", bolt)
	}
	p, _ := platform.Get[construction.Pipeline](tn.automation(construction.ID, false), "P2")
	if p.Last == nil || p.Last.Merged != 1 || p.Last.Written != 2 {
		t.Fatalf("MES run %+v", p.Last)
	}
}

func toMap(v any) map[string]any {
	raw, _ := json.Marshal(v)
	var m map[string]any
	json.Unmarshal(raw, &m)
	return m
}
