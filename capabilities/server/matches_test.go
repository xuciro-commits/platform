package platformserver

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// SAP materials and MES parts are one material: a published matching rule
// makes MES rows an edit of the SAP record when the part number agrees by
// digits, and keeps SAP's description while MES may fill the weight.
func TestMatchingRuleConvergesMaterials(t *testing.T) {
	seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder}}}
	tn, err := NewTenant("mdm", NewConsole("mdm", seat), build.New("mdm"))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tn.Member("dana")
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	submit := func(key, schema, typ, target, payload string) {
		t.Helper()
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	submit("obj", build.ObjectType+".create", build.ObjectType, "O1", `{"name":"material","title":"Material","fields":[{"name":"partno","title":"Part no","type":"text"},{"name":"description","title":"Description","type":"text"},{"name":"weight","title":"Weight","type":"decimal"}]}`)
	submit("obj-publish", build.SchemaPublish, build.ObjectType, "O1", `{}`)
	submit("rule", build.MatchType+".create", build.MatchType, "M1", `{"name":"materialmatch","title":"Same material","object":"build.material","keys":[{"fields":["partno"],"normalize":"digits"}],"prefer":[{"field":"description","producer":"sapmaterials"}]}`)
	submit("rule-publish", build.MatchType+".publish", build.MatchType, "M1", `{}`)
	for _, ds := range []string{"sap", "mes"} {
		submit("ds-"+ds, build.DatasetType+".create", build.DatasetType, ds, `{"name":"`+ds+`","title":"`+ds+`"}`)
	}
	submit("p-sap", build.PipelineType+".create", build.PipelineType, "P1", `{"name":"sapmaterials","title":"SAP materials","input":"sap","outputObject":"build.material","key":"matnr"}`)
	submit("p-sap-publish", build.PipelineType+".publish", build.PipelineType, "P1", `{}`)
	submit("p-mes", build.PipelineType+".create", build.PipelineType, "P2", `{"name":"mesparts","title":"MES parts","input":"mes","outputObject":"build.material","key":"part_id"}`)
	submit("p-mes-publish", build.PipelineType+".publish", build.PipelineType, "P2", `{}`)
	submit("load-sap", build.SchemaDatasetLoad, build.DatasetType, "sap", `{"rows":[{"matnr":"000000123","partno":"000000123","description":"Hex bolt M8"},{"matnr":"000000456","partno":"000000456","description":"Washer"}]}`)
	tn.RunPipelines(at)
	later := at.Add(time.Hour)
	if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "load-mes",
		Target: &pb.EntityRef{Type: build.DatasetType, Id: "mes"}, Schema: &pb.SchemaRef{Name: build.SchemaDatasetLoad, Version: 1},
		Payload: []byte(`{"rows":[{"part_id":"MES-1","partno":"M-123","description":"BOLT HEX M8 (MES)","weight":0.012},{"part_id":"MES-2","partno":"789","description":"Nut"}]}`)}, later); err != nil {
		t.Fatalf("%s: %s", err.Code, err.Message)
	}
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
	p, _ := platform.Get[build.Pipeline](tn.automation(build.ID, false), "P2")
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
