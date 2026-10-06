package platformserver

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A dataset gains a version from a load, a published pipeline runs on the
// outside loop when its input is newer than its last run, quarantines the bad
// rows and writes the rest as the output dataset's next version.
func TestPipelineRunsOnNewDatasetVersion(t *testing.T) {
	seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder}}}
	tn, err := NewTenant("pipelines", NewConsole("pipelines", seat), build.New("pipelines"))
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
	submit("ds-in", build.DatasetType+".create", build.DatasetType, "raw", `{"name":"rawitems","title":"Raw items"}`)
	submit("ds-out", build.DatasetType+".create", build.DatasetType, "clean", `{"name":"cleanitems","title":"Clean items","keep":2}`)
	submit("pipe", build.PipelineType+".create", build.PipelineType, "p1", `{"name":"clean","title":"Clean","input":"raw","outputDataset":"clean",
		"steps":[{"kind":"filter","column":"plant","op":"=","value":"1000"},{"kind":"cast","column":"qty","type":"number"}],
		"expectations":[{"column":"sku","rule":"notnull"},{"column":"qty","rule":"range","value":"0..10"}]}`)
	submit("pipe-publish", build.PipelineType+".publish", build.PipelineType, "p1", `{}`)
	tn.RunPipelines(at) // publish requests a run; with no input version it records why and writes nothing
	submit("load-1", build.SchemaDatasetLoad, build.DatasetType, "raw", `{"rows":[{"sku":"A","plant":"1000","qty":"3"},{"sku":"","plant":"1000","qty":"1"},{"sku":"C","plant":"2000","qty":"1"},{"sku":"D","plant":"1000","qty":"50"}],"producer":"test"}`)
	tn.RunPipelines(at.Add(time.Minute))
	c := tn.automation(build.ID, false)
	out, ok := platform.Get[build.Dataset](c, "clean")
	if !ok || out.Version != 1 || out.Last.Rows != 1 {
		t.Fatalf("output dataset %+v", out)
	}
	rows, _, _ := build.DatasetRows(c, "clean", 0)
	if len(rows) != 1 || rows[0]["sku"] != "A" || rows[0]["qty"] != 3.0 {
		t.Fatalf("rows %+v", rows)
	}
	p, _ := platform.Get[build.Pipeline](c, "p1")
	if p.Last == nil || p.Last.Input != 1 || p.Last.Written != 1 || p.Last.Quarantined != 2 || len(p.Last.Quarantine) != 2 {
		raw, _ := json.Marshal(p.Last)
		t.Fatalf("run %s", raw)
	}
	tn.RunPipelines(at.Add(2 * time.Minute)) // same input version: not due
	if p2, _ := platform.Get[build.Pipeline](c, "p1"); !p2.Last.At.Equal(p.Last.At) {
		t.Fatal("ran again without a new input version")
	}
	// Three loads with keep=2 on the input: the first version is emptied.
	for i := 2; i <= 3; i++ {
		submit("load-"+string(rune('0'+i)), build.SchemaDatasetLoad, build.DatasetType, "raw", `{"rows":[{"sku":"A","plant":"1000","qty":"1"}]}`)
	}
	in, _ := platform.Get[build.Dataset](c, "raw")
	if in.Version != 3 || len(in.Schema) != 3 {
		t.Fatalf("input %+v", in)
	}
	if _, _, err := build.DatasetRows(c, "raw", 1); err != nil {
		t.Fatalf("version 1 should still be kept with default keep 3: %v", err)
	}
}
