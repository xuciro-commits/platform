package platformserver

import (
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A confidential dataset cannot be poured into an object whose fields every
// role reads; once the fields name their readers the pipeline runs and the
// output dataset inherits the marking. Restricted data never leaves as CSV.
func TestMarkingsTravelAndGate(t *testing.T) {
	seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder}}}
	tn, err := NewTenant("marks", NewConsole("marks", seat), build.New("marks"))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tn.Member("dana")
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	try := func(key, schema, typ, target, payload string) *kernel.Error {
		_, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at)
		return err
	}
	submit := func(key, schema, typ, target, payload string) {
		t.Helper()
		if err := try(key, schema, typ, target, payload); err != nil {
			t.Fatalf("%s: %s %s", key, err.Code, err.Message)
		}
	}
	submit("obj", build.ObjectType+".create", build.ObjectType, "O1", `{"name":"employee","title":"Employee","fields":[{"name":"salary","title":"Salary","type":"decimal"}]}`)
	submit("obj-publish", build.SchemaPublish, build.ObjectType, "O1", `{}`)
	submit("ds", build.DatasetType+".create", build.DatasetType, "hr", `{"name":"hr","title":"HR","marking":"confidential"}`)
	submit("ds-out", build.DatasetType+".create", build.DatasetType, "hrclean", `{"name":"hrclean","title":"HR clean"}`)
	submit("p", build.PipelineType+".create", build.PipelineType, "P1", `{"name":"hrtoemployee","title":"HR to employees","input":"hr","outputObject":"build.employee","key":"pernr"}`)
	if err := try("p-publish", build.PipelineType+".publish", build.PipelineType, "P1", `{}`); err == nil || !strings.Contains(err.Message, "salary") {
		t.Fatalf("publishing into open fields should be refused naming them: %v", err)
	}
	submit("obj-edit", build.ObjectType+".edit", build.ObjectType, "O1", `{"fields":[{"name":"salary","title":"Salary","type":"decimal","read":["hr"]}],"access":[{"role":"hr","read":"all","create":true,"edit":true}]}`)
	submit("obj-publish-2", build.SchemaPublish, build.ObjectType, "O1", `{}`)
	submit("p-publish-2", build.PipelineType+".publish", build.PipelineType, "P1", `{}`)
	submit("p2", build.PipelineType+".create", build.PipelineType, "P2", `{"name":"hrclean","title":"HR clean","input":"hr","outputDataset":"hrclean"}`)
	submit("p2-publish", build.PipelineType+".publish", build.PipelineType, "P2", `{}`)
	submit("load", build.SchemaDatasetLoad, build.DatasetType, "hr", `{"rows":[{"pernr":"1","salary":5000}]}`)
	tn.RunPipelines(at)
	c := tn.automation(build.ID, false)
	if p, _ := platform.Get[build.Pipeline](c, "P1"); p.Last == nil || p.Last.Error != "" || p.Marking != "confidential" {
		t.Fatalf("P1 %+v marking %q", p.Last, p.Marking)
	}
	if out, _ := platform.Get[build.Dataset](c, "hrclean"); out.Marking != "confidential" || out.Version != 1 {
		t.Fatalf("output dataset inherits the marking: %+v", out)
	}
	submit("ds-restrict", build.DatasetType+".edit", build.DatasetType, "hr", `{"marking":"restricted"}`)
	if _, err := tn.Export(member, build.DatasetVersionType, platform.Query{}, at); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		t.Fatalf("restricted datasets never leave as CSV: %v", err)
	}
	if _, err := tn.Export(member, "build.employee", platform.Query{}, at); err != nil {
		t.Fatalf("the object is only confidential so far: %v", err)
	}
	submit("load-2", build.SchemaDatasetLoad, build.DatasetType, "hr", `{"rows":[{"pernr":"1","salary":5100}]}`)
	tn.RunPipelines(at.Add(time.Hour))
	if _, err := tn.Export(member, "build.employee", platform.Query{}, at); err == nil {
		t.Fatal("after a restricted run the object does not leave as CSV either")
	}
}
