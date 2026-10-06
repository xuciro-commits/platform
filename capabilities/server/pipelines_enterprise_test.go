package platformserver

import (
	"slices"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

// SAP OM organisational units land in the enterprise model through a
// pipeline: elements owned on the system's behalf, placed under the company
// in the management kind; a later sync that drops a unit closes it today and
// keeps its history, while a renamed one is edited in place.
func TestPipelineLandsEnterpriseSlice(t *testing.T) {
	seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder, enterprise.ID: enterprise.Admin}}}
	ent := enterprise.New("landing", platform.OrgSeed{})
	tn, err := NewTenant("landing", NewConsole("landing", seat), ent, build.New("landing"))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tn.Member("dana")
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	submit := func(key, app, schema, typ, target, payload string) {
		t.Helper()
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: app, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	submit("seed", enterprise.ID, enterprise.SchemaSeed, enterprise.ModelType, "model", `{"scale":"S","name":"Acme"}`)
	model := ent.Model()
	company := model.Elements[slices.IndexFunc(model.Elements, func(e enterprise.Element) bool { return e.Stereotype == enterprise.Organization })].ID
	kind := model.Kinds[0].ID
	submit("ds", build.ID, build.DatasetType+".create", build.DatasetType, "om", `{"name":"sapom","title":"SAP OM units"}`)
	submit("pipe", build.ID, build.PipelineType+".create", build.PipelineType, "p1", `{"name":"omtoorg","title":"OM to org tree","input":"om",
		"outputEnterprise":{"source":"sap-om","stereotype":"ActualOrganization","id":"objid","name":"stext","kind":"=department","parent":"parent","root":"`+company+`","in":"`+kind+`","relation":"part of","properties":["costcenter"]}}`)
	submit("pipe-publish", build.ID, build.PipelineType+".publish", build.PipelineType, "p1", `{}`)
	submit("load-1", build.ID, build.SchemaDatasetLoad, build.DatasetType, "om", `{"rows":[{"objid":"50000001","stext":"Production","parent":"","costcenter":"CC100"},{"objid":"50000002","stext":"Assembly","parent":"50000001","costcenter":"CC110"},{"objid":"50000003","stext":"Paint shop","parent":"50000001"}]}`)
	tn.RunPipelines(at)
	if p0, _ := platform.Get[build.Pipeline](tn.automation(build.ID, false), "p1"); p0.Last == nil || p0.Last.Error != "" {
		t.Fatalf("first run %+v", p0.Last)
	}
	model = ent.Model()
	day := at.Format(time.DateOnly)
	el := func(id string) *enterprise.Element {
		i := slices.IndexFunc(model.Elements, func(e enterprise.Element) bool { return e.ID == id })
		if i < 0 {
			return nil
		}
		return &model.Elements[i]
	}
	if p := el("sap-om:50000001"); p == nil || p.Owner != "source:sap-om" || p.Kind != "department" || p.From != day || p.Properties["source:sap-om"].(map[string]any)["costcenter"] != "CC100" {
		t.Fatalf("production %+v", p)
	}
	if parents := model.Of("sap-om:50000002", enterprise.Placement, true, day); len(parents) != 1 || parents[0] != "sap-om:50000001" {
		t.Fatalf("assembly placed under %v", parents)
	}
	if parents := model.Of("sap-om:50000001", enterprise.Placement, true, day); len(parents) != 1 || parents[0] != company {
		t.Fatalf("production placed under %v, want the company %s", parents, company)
	}
	p, _ := platform.Get[build.Pipeline](tn.automation(build.ID, false), "p1")
	if p.Last == nil || p.Last.Error != "" || p.Last.Written != 3 {
		t.Fatalf("run %+v", p.Last)
	}
	// Second sync: the paint shop is gone, assembly renamed.
	later := at.Add(48 * time.Hour)
	if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "load-2",
		Target: &pb.EntityRef{Type: build.DatasetType, Id: "om"}, Schema: &pb.SchemaRef{Name: build.SchemaDatasetLoad, Version: 1},
		Payload: []byte(`{"rows":[{"objid":"50000001","stext":"Production","parent":""},{"objid":"50000002","stext":"Final assembly","parent":"50000001"}]}`)}, later); err != nil {
		t.Fatal(err)
	}
	tn.RunPipelines(later)
	model = ent.Model()
	if paint := el("sap-om:50000003"); paint == nil || paint.Until != later.Format(time.DateOnly) || paint.Closed == "" {
		t.Fatalf("paint shop should be closed on the second sync: %+v", paint)
	}
	if asm := el("sap-om:50000002"); asm.Name != "Final assembly" || asm.From != day {
		t.Fatalf("assembly %+v", asm)
	}
	if live := model.Of("sap-om:50000001", enterprise.Placement, false, later.Format(time.DateOnly)); len(live) != 1 || live[0] != "sap-om:50000002" {
		t.Fatalf("units under production later: %v", live)
	}
}
