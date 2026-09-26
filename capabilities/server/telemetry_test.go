package platformserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/flow"
	"platformserver/platform"
)

// ADR-0027 10c: one trace follows a submission through the delivery that
// starts a flow, whose step causes an effect, to the effect's attempt.
func TestTraceFollowsWork(t *testing.T) {
	spans := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans)))
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer sink.Close()
	dir := NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"shop": "clerk", PlatformApp: Admin}}})
	tn, err := NewTenant("t-1", dir, flow.New("t-1"), newShop("t-1", fulfil(1)))
	if err != nil {
		t.Fatal(err)
	}
	tn.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	ana, _ := dir.Member("ana")
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	decide := func(app, schema, typ, id string, payload any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(ana, &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: app, IdempotencyKey: schema + id,
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			t.Fatal(err)
		}
	}
	decide(PlatformApp, SchemaEndpointAdd, EndpointType, "hook", map[string]any{"url": sink.URL, "secret": "hook", "events": []string{"shop.order.reserve"}, "allowPrivate": true})
	spans.Reset()
	decide("shop", "shop.order.place", "shop.order", "O1", map[string]string{"item": "pen"})
	tn.Work(now)
	tn.Dispatch(now)

	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range spans.GetSpans().Snapshots() {
		byName[s.Name()] = s
	}
	submit, deliver, send := byName["submit shop.order.place"], byName["deliver shop.order.place to flow"], byName["send shop.order.reserve to hook"]
	if submit == nil || deliver == nil || send == nil {
		t.Fatalf("spans %v", keys(byName))
	}
	trace := submit.SpanContext().TraceID()
	if deliver.SpanContext().TraceID() != trace || send.SpanContext().TraceID() != trace {
		t.Fatal("the delivery or the effect left the submission's trace")
	}
	if deliver.Parent().SpanID() != submit.SpanContext().SpanID() || send.Parent().SpanID() != deliver.SpanContext().SpanID() {
		t.Fatal("the spans are not each other's children")
	}
	for _, s := range []sdktrace.ReadOnlySpan{submit, deliver, send} {
		if !has(s, "platform.outcome", "ok") || !has(s, "platform.tenant", "t-1") {
			t.Fatalf("%s: attributes %v", s.Name(), s.Attributes())
		}
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func has(s sdktrace.ReadOnlySpan, key, value string) bool {
	for _, a := range s.Attributes() {
		if string(a.Key) == key && a.Value.AsString() == value {
			return true
		}
	}
	return false
}
