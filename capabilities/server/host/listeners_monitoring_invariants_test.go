package host

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	dataowner "platformserver/services/data"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
)

type listenerProbe struct{ ledger *platform.Ledger }

type listenerProbeRecord struct {
	platform.Record
	Title string `json:"title"`
}

func newListenerProbe(tenant string) *listenerProbe {
	return &listenerProbe{ledger: platform.NewLedger(tenant, "listenerprobe", platform.NewCatalog(platform.Action{Schema: "listenerprobe.receive", Target: "listenerprobe.event", Title: "Receive event", Description: "Accept one connector event.", Roles: []string{platform.AnyMember}, Payload: []platform.Field{{Name: "title", Type: "string", Required: true, Description: "Event title"}}}), "listenerprobe.event")}
}

func (p *listenerProbe) AcceptedLedger() *platform.Ledger { return p.ledger }
func (p *listenerProbe) ForkRead(ledger *platform.Ledger) (platform.App, error) {
	return &listenerProbe{ledger: ledger}, nil
}
func (p *listenerProbe) Declarations() []*pb.AuthorityDeclaration { return p.ledger.Declarations() }
func (p *listenerProbe) SnapshotFormat() string                   { return "platform.ledger.v1" }
func (p *listenerProbe) Snapshot() (json.RawMessage, error)       { return p.ledger.Snapshot() }
func (p *listenerProbe) Restore(raw json.RawMessage) error        { return p.ledger.Restore(raw) }
func (p *listenerProbe) Manifest() platform.Manifest {
	return platform.Manifest{ID: "listenerprobe", Title: "Listener probe", Version: "1", Roles: []string{"sender"}, Actions: p.ledger.Catalog,
		Entities: []platform.Entity{{Type: "listenerprobe.event", Title: "Event", Model: listenerProbeRecord{}}}, Inputs: map[string]bool{"listenerprobe": true}}
}
func (p *listenerProbe) AcceptedInputs() []string { return []string{"listenerprobe"} }
func (p *listenerProbe) DecodeAcceptedInput(raw json.RawMessage) (any, error) {
	record := &pb.ChangeRecord{}
	err := protojson.Unmarshal(raw, record)
	return record, err
}
func (p *listenerProbe) Input(c platform.Caller, _ string, body []byte, at time.Time) (any, *kernel.Error) {
	var input struct{ ID, Title string }
	if json.Unmarshal(body, &input) != nil || input.ID == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	return platform.Decide(c, p, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: "listenerprobe", IdempotencyKey: input.ID,
		Schema: &pb.SchemaRef{Name: "listenerprobe.receive", Version: 1}, Target: &pb.EntityRef{Type: "listenerprobe.event", Id: input.ID}, Payload: platform.Raw(map[string]string{"title": input.Title})}, at)
}
func (p *listenerProbe) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return p.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct{ Title string }
		if json.Unmarshal(s.GetPayload(), &payload) != nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		return func(r *pb.ChangeRecord) {
			c.Put(r, listenerProbeRecord{Record: platform.Record{ID: s.GetTarget().GetId()}, Title: payload.Title})
		}, nil
	})
}

func TestInvariantK2ListenersAndMonitoringViews(t *testing.T) {
	const tenant = "listeners-monitoring"
	seats := []platform.Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin"}}},
		{Subjects: []string{"sender"}, Member: platform.Member{ID: "sender", Roles: map[string]string{"listenerprobe": "sender"}}},
		{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{}}},
	}
	fresh := func() *invariantTenant {
		return newInvariantTenantWithApps(t, tenant, []platform.App{monitoring.New(tenant), newListenerProbe(tenant)}, seats...)
	}
	replay := func() *invariantTenant {
		return newInvariantTenantReplayWithApps(t, tenant, []platform.App{monitoring.New(tenant), newListenerProbe(tenant)}, seats...)
	}
	live := fresh()
	live.Secrets = func(name string) ([]byte, bool) { return []byte("shared-value"), name == "listener-secret" }
	live.accept(t, dataowner.ListenerType+".create", dataowner.ListenerType, "LISTENER", map[string]any{"name": "incoming", "title": "Incoming", "connector": "listenerprobe", "path": "incoming-token", "secret": "listener-secret"})
	live.accept(t, dataowner.ListenerType+".publish", dataowner.ListenerType, "LISTENER", map[string]any{})
	host := NewHost(Tokens(map[string]string{"builder": "builder", "sender": "sender", "other": "other", "admin": "admin"}), live.Tenant)
	host.Now = func() time.Time { return invariantClock }
	handler := host.Handler()
	request := func(method, path, member, secret string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if member != "" {
			r.Header.Set("Authorization", "Bearer "+member)
		}
		r.Header.Set(TenantHeader, tenant)
		r.Header.Set("X-Listener-Secret", secret)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response
	}
	t.Setenv("PLATFORM_CREDENTIAL_KEY", strings.Repeat("01", 32))
	seal := platform.CredentialSealRequest{CredentialWrite: platform.CredentialWrite{CredentialDefinition: platform.CredentialDefinition{
		ID: "listener-secret", Title: "Inbound verification", Kind: "api-key", Source: "managed",
		Bindings: []platform.CredentialBinding{{Consumer: "listener/LISTENER", Origin: "http://example.com"}},
	}}, Value: "shared-value"}
	sealed := request(http.MethodPost, "/v1/credentials/seal", "admin", "", platform.Raw(seal))
	var credential platform.CredentialWrite
	if sealed.Code != http.StatusOK || json.Unmarshal(sealed.Body.Bytes(), &credential) != nil || credential.Sealed == "" {
		t.Fatalf("managed listener credential sealing: HTTP %d: %s", sealed.Code, sealed.Body)
	}
	admin, _ := live.Member("admin")
	credentialSubmission := live.submission(controlowner.SchemaCredentialSave, controlowner.CredentialType, credential.ID, credential)
	credentialSubmission.Authority, credentialSubmission.PrincipalId = "platform", "admin"
	if _, err := live.Submit(admin, credentialSubmission, invariantClock); err != nil {
		t.Fatal(err)
	}
	live.Secrets = func(string) ([]byte, bool) { return nil, false }
	response := request(http.MethodGet, "/v1/records/build.listener", "builder", "", nil)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("incoming-token")) {
		t.Fatalf("listener generic records: HTTP %d: %s", response.Code, response.Body)
	}
	body := []byte(`{"id":"event1","title":"Inbound payload"}`)
	before := len(live.entries)
	for _, member := range []string{"", "sender"} {
		secret := "shared-value"
		if member == "sender" {
			secret = "wrong"
		}
		response := request(http.MethodPost, "/v1/listeners/incoming-token", member, secret, body)
		if response.Code == http.StatusOK {
			t.Fatalf("unverified delivery was accepted: %s", response.Body)
		}
	}
	if len(live.entries) != before {
		t.Fatal("refused listener delivery changed accepted state")
	}
	for range 2 {
		response := request(http.MethodPost, "/v1/listeners/incoming-token", "sender", "shared-value", body)
		if response.Code != http.StatusOK {
			t.Fatalf("listener delivery: HTTP %d: %s", response.Code, response.Body)
		}
	}
	if len(live.entries) != before+1 {
		t.Fatalf("duplicate delivery produced %d accepted results", len(live.entries)-before)
	}
	owner := live.app(build.ID).(*build.Build)
	if !slices.Contains(owner.AssetKinds(), platform.AssetListener) {
		t.Fatal("listener has no asset lifecycle owner")
	}
	var original platform.ReleaseAsset
	_, refusal := coordinatedRecordRead(live.Tenant, context.Background(), func(view *Tenant) (any, *kernel.Error) {
		assets, err := view.app(build.ID).(*build.Build).ReleaseAssets()
		if err != nil {
			t.Fatal(err)
		}
		for _, asset := range assets {
			if asset.Ref.Kind == platform.AssetListener {
				original = asset
			}
		}
		if original.Ref.Name != "incoming" || original.SourceVersion == "" {
			t.Fatal("published listener was not enumerated")
		}
		return nil, nil
	})
	if refusal != nil {
		t.Fatal(refusal)
	}
	live.accept(t, dataowner.ListenerType+".edit", dataowner.ListenerType, "LISTENER", map[string]any{"title": "Revised inbound", "path": "incoming-next", "connector": "missing-input", "secret": "next-secret"})
	if response := request(http.MethodPost, "/v1/listeners/incoming-token", "sender", "shared-value", body); response.Code != http.StatusOK {
		t.Fatalf("ordinary edit changed accepted listener: HTTP %d: %s", response.Code, response.Body)
	}
	if response := request(http.MethodPost, "/v1/listeners/incoming-next", "sender", "shared-value", body); response.Code == http.StatusOK {
		t.Fatal("unpublished listener path became active")
	}
	live.accept(t, dataowner.ListenerType+".edit", dataowner.ListenerType, "LISTENER", map[string]any{"connector": "listenerprobe", "secret": "listener-secret"})
	drafts := []build.JointDraftRef{{Kind: platform.AssetListener, ID: "LISTENER"}}
	preview, err := live.PreviewReleaseDrafts(live.member(t), drafts, "", invariantClock)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("listener candidate: %+v: %v", preview, err)
	}
	if !slices.Contains(preview.Included, original.Ref) {
		t.Fatal("listener missing from candidate closure")
	}
	candidate, err := live.SaveReleaseCandidates(live.member(t), drafts, preview.CandidateID, "listener-release", invariantClock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := live.ActivateRelease(live.member(t), candidate, "listener-activate", invariantClock); err != nil {
		t.Fatal(err)
	}
	if response := request(http.MethodPost, "/v1/listeners/incoming-next", "sender", "shared-value", body); response.Code != http.StatusOK {
		t.Fatalf("released listener not installed: HTTP %d: %s", response.Code, response.Body)
	}
	if response := request(http.MethodPost, "/v1/listeners/incoming-token", "sender", "shared-value", body); response.Code == http.StatusOK {
		t.Fatal("old published path remained active after release installation")
	}
	_, refusal = coordinatedRecordRead(live.Tenant, context.Background(), func(view *Tenant) (any, *kernel.Error) {
		retained, err := view.app(build.ID).(*build.Build).ResolvePinned(original.Ref, original.SourceVersion)
		if err != nil || !bytes.Equal(retained.Body, original.Body) {
			t.Fatalf("original listener version not retained: %v", err)
		}
		return nil, nil
	})
	if refusal != nil {
		t.Fatal(refusal)
	}
	live.accept(t, dataowner.ListenerType+".pause", dataowner.ListenerType, "LISTENER", map[string]any{})
	if response := request(http.MethodPost, "/v1/listeners/incoming-next", "sender", "shared-value", body); response.Code == http.StatusOK {
		t.Fatal("paused listener accepted a delivery")
	}
	for _, member := range []string{"builder", "other"} {
		submission := live.submission(monitoring.SchemaViewSave, monitoring.ViewType, member+"-view", map[string]string{"title": "Failures", "type": "sync", "status": "failed"})
		submission.Authority, submission.PrincipalId = monitoring.ID, member
		m, _ := live.Member(member)
		if _, err := live.Submit(m, submission, invariantClock); err != nil {
			t.Fatal(err)
		}
		response := request(http.MethodGet, "/v1/monitoring/views", member, "", nil)
		var views []platform.MonitoringView
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &views) != nil || len(views) != 1 || views[0].Owner != member || views[0].Type != "sync" || views[0].Status != "failed" {
			t.Fatalf("owner-scoped monitoring views: HTTP %d: %s", response.Code, response.Body)
		}
	}
	other, _ := live.Member("other")
	for _, schema := range []string{monitoring.SchemaViewSave, monitoring.SchemaViewRemove} {
		submission := live.submission(schema, monitoring.ViewType, "builder-view", map[string]string{"title": "Not mine"})
		submission.Authority, submission.PrincipalId = monitoring.ID, "other"
		if _, err := live.Submit(other, submission, invariantClock); err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
			t.Fatalf("another owner's view changed: %v", err)
		}
	}
	page, recordRefusal := live.Records(t.Context(), other, monitoring.ViewType, platform.Query{}, invariantClock)
	if recordRefusal != nil || len(page.Records) != 1 || page.Records[0].(platform.MonitoringView).Owner != "other" {
		t.Fatalf("generic records leaked a view: %+v: %v", page, recordRefusal)
	}
	remove := live.submission(monitoring.SchemaViewRemove, monitoring.ViewType, "other-view", map[string]any{})
	remove.Authority, remove.PrincipalId = monitoring.ID, "other"
	if _, err := live.Submit(other, remove, invariantClock); err != nil {
		t.Fatal(err)
	}
	if response := request(http.MethodGet, "/v1/monitoring/views", "other", "", nil); response.Code != http.StatusOK || response.Body.String() != "[]\n" {
		t.Fatalf("removed view still visible: %s", response.Body)
	}
	restored := replay()
	if err := restored.recoverEntries(live.entries); err != nil {
		t.Fatal(err)
	}
	if differing := differingTypes(live.recordImage(t), restored.recordImage(t)); len(differing) != 0 {
		t.Fatalf("replay changed records: %v", differing)
	}
	_, refusal = coordinatedRecordRead(restored.Tenant, context.Background(), func(view *Tenant) (any, *kernel.Error) {
		record, found := platform.Get[listenerProbeRecord](view.automation("listenerprobe", false), "event1")
		if !found || record.Title != "Inbound payload" {
			t.Fatal("accepted connector payload was not restored")
		}
		return record, nil
	})
	if refusal != nil {
		t.Fatal(refusal)
	}
}
