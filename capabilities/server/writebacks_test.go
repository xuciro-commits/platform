package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A published writeback turns the object's accepted create into an effect on
// the connection: an outage queues it (retry with the same key), the next
// attempt delivers it, and the answer's document number lands on the record.
func TestWritebackQueuesAndReplaysOnce(t *testing.T) {
	seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder}}}
	tn, err := NewTenant("writebacks", NewConsole("writebacks", seat), build.New("writebacks"))
	if err != nil {
		t.Fatal(err)
	}
	tn.Secrets = func(name string) ([]byte, bool) { return []byte("Basic c2FwOnNlY3JldA=="), name == "sap-writer" }
	var calls []string
	down := true
	tn.Outbound = func(req *http.Request, _ bool) (*http.Response, error) {
		if req.Method == http.MethodGet { // the connection check
			return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`{"d":{"EntitySets":["A_MaterialDocumentHeader"]}}`))}, nil
		}
		body, _ := io.ReadAll(req.Body)
		calls = append(calls, req.Method+" "+req.URL.String()+" "+req.Header.Get("Idempotency-Key")+" "+req.Header.Get("Authorization")+" "+string(body))
		if down {
			return &http.Response{StatusCode: 503, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 201, Status: "201 Created", Body: io.NopCloser(strings.NewReader(`{"d":{"MaterialDocument":"5000001"}}`))}, nil
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
	submit("obj", build.ObjectType+".create", build.ObjectType, "O1", `{"name":"goodsreceipt","title":"Goods receipt","fields":[{"name":"sku","title":"SKU","type":"text"},{"name":"qty","title":"Qty","type":"decimal"},{"name":"docno","title":"SAP document","type":"text"}]}`)
	submit("obj-publish", build.SchemaPublish, build.ObjectType, "O1", `{}`)
	submit("conn", build.ConnectionType+".create", build.ConnectionType, "sap", `{"name":"sap","title":"SAP","kind":"odata","address":"https://sap.example.com/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/","secret":"sap-writer"}`)
	submit("conn-check", build.ConnectionType+".check", build.ConnectionType, "sap", `{}`)
	tn.CheckConnections(at) // ready only once the host's check answered and was journaled
	if c0, _ := platform.Get[build.Connection](tn.automation(build.ID, false), "sap"); c0.State != "ready" {
		t.Fatalf("connection %+v", c0)
	}
	submit("wb", build.WritebackType+".create", build.WritebackType, "W1", `{"name":"grtosap","title":"Goods receipt to SAP","connection":"sap","object":"build.goodsreceipt","on":"create","path":"A_MaterialDocumentHeader",
		"mapping":[{"from":"sku","to":"Material"},{"from":"qty","to":"QuantityInEntryUnit","convert":"string"},{"from":"id","to":"ReferenceDocument"}],
		"result":[{"from":"MaterialDocument","to":"docno"}]}`)
	submit("wb-publish", build.WritebackType+".publish", build.WritebackType, "W1", `{}`)
	submit("gr-1", "build.goodsreceipt.create", "build.goodsreceipt", "GR1", `{"sku":"A100","qty":12}`)
	effects := tn.Effects(at)
	if len(effects) != 1 || effects[0].Endpoint != "connection:sap" || effects[0].State != "pending" {
		t.Fatalf("effects %+v", effects)
	}
	tn.Dispatch(at) // SAP is down: retry with the same key, nothing lost
	if x := tn.Effects(at)[0]; x.State != "retrying" || x.Attempts != 1 {
		t.Fatalf("after outage %+v", x)
	}
	down = false
	tn.Dispatch(at.Add(time.Minute))
	x := tn.Effects(at)[0]
	if x.State != "delivered" || x.Attempts != 2 {
		t.Fatalf("after recovery %+v", x)
	}
	if len(calls) != 2 || calls[0] != calls[1] || !strings.Contains(calls[1], "POST https://sap.example.com/sap/opu/odata/sap/API_MATERIAL_DOCUMENT_SRV/A_MaterialDocumentHeader "+x.ID+" Basic c2FwOnNlY3JldA== ") ||
		!strings.Contains(calls[1], `"Material":"A100"`) || !strings.Contains(calls[1], `"QuantityInEntryUnit":"12"`) || !strings.Contains(calls[1], `"ReferenceDocument":"GR1"`) {
		t.Fatalf("calls %q", calls)
	}
	tn.mu.Lock()
	c := tn.automation(build.ID, false)
	w, _ := platform.Get[build.Writeback](c, "W1")
	tn.mu.Unlock()
	if w.Sent != 1 || len(w.Answers) != 1 || w.Answers[0].Result != "delivered" {
		t.Fatalf("writeback %+v", w)
	}
	view, verr := tn.RecordOf(member, "build.goodsreceipt", "GR1", at)
	raw, _ := json.Marshal(view.Record)
	if verr != nil || !strings.Contains(string(raw), `"docno":"5000001"`) {
		t.Fatalf("record %v %s", verr, raw)
	}
}
