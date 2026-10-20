package platformserver

import "platformserver/internal/construction"

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"platformserver/apps/build"
	"platformserver/apps/enterprise"
	enterpriseapi "platformserver/internal/host/enterpriseapi"
	"platformserver/platform"
)

func tenantLinkPreviewSetup(t *testing.T) (*Tenant, *enterprise.Enterprise, *httptest.Server, *atomic.Int64) {
	t.Helper()
	source := composeTenant(t, "source", []Seat{seatOf("publisher", "enterprise:admin")}, enterprise.New("source", platform.OrgSeed{Units: []platform.Unit{{ID: "remote", Name: "Remote company", From: "2026-01-01"}}}))
	decide(t, source, "publisher", enterprise.ID, enterprise.SchemaElementEdit, enterprise.ElementType, "remote", map[string]any{"published": true}, txnAt)
	handler := NewHost(Tokens(map[string]string{"source-token": "publisher"}), source).Handler()
	calls := new(atomic.Int64)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		if r.URL.Path != "/v1/enterprise-published" || r.Header.Get("Authorization") != "Bearer source-token" || r.Header.Get("Platform-Tenant") != "source" {
			t.Errorf("preview left its explicit source connection: %s", r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(remote.Close)
	model := enterprise.New("group", platform.OrgSeed{Units: []platform.Unit{{ID: "group", Name: "Group", From: "2026-01-01"}}})
	tn := composeTenant(t, "group", []Seat{seatOf("owner", "enterprise:admin", "build:integrator"), seatOf("modeladmin", "enterprise:admin"), seatOf("integrator", "build:integrator")}, build.New("group"), model)
	tn.Secrets = func(name string) ([]byte, bool) { return []byte("Bearer source-token"), name == "source-access" }
	decide(t, tn, "owner", construction.ID, construction.ConnectionType+".create", construction.ConnectionType, "source", map[string]any{"name": "subsidiarysource", "title": "Subsidiary source", "kind": "http", "address": remote.URL, "secret": "source-access", "allowPrivate": true}, txnAt)
	decide(t, tn, "owner", construction.ID, construction.ConnectionType+".check", construction.ConnectionType, "source", map[string]any{}, txnAt)
	tn.CheckConnections(txnAt)
	decide(t, tn, "owner", enterprise.ID, enterprise.SchemaTenantLinkSave, enterprise.TenantLinkType, "subsidiary", map[string]any{"organization": "group", "tenant": "source", "root": "remote", "connection": "source"}, txnAt)
	return tn, model, remote, calls
}

func TestEnterpriseLinkPreviewUsesOriginalConnectionWithoutWriting(t *testing.T) {
	tn, model, _, calls := tenantLinkPreviewSetup(t)
	before := model.Model().Fingerprint()
	var writes atomic.Int64
	tn.Record = func(Entry) { writes.Add(1) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { writes.Add(1); return e.Body, nil }
	handler := NewHost(Tokens(map[string]string{"owner": "owner", "modeladmin": "modeladmin", "integrator": "integrator"}), tn).Handler()
	post := func(who string) (int, []byte) {
		request := httptest.NewRequest(http.MethodPost, "/v1/enterprise-links/subsidiary/preview", nil)
		request.Header.Set("Authorization", "Bearer "+who)
		reply := httptest.NewRecorder()
		handler.ServeHTTP(reply, request)
		return reply.Code, reply.Body.Bytes()
	}
	for _, who := range []string{"modeladmin", "integrator"} {
		if status, _ := post(who); status != http.StatusForbidden {
			t.Fatalf("%s preview returned %d", who, status)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized preview reached the source")
	}
	limited := memberOf(t, tn, "owner")
	limited.Scopes = []string{"enterprise.element.*"}
	if _, err := tn.PreviewEnterpriseLink(context.Background(), limited, "subsidiary", txnAt); err == nil || calls.Load() != 0 {
		t.Fatal("a token without import scope reached the source")
	}
	status, body := post("owner")
	var preview EnterpriseLinkPreview
	if status != http.StatusOK || json.Unmarshal(body, &preview) != nil {
		t.Fatalf("preview %d: %s", status, body)
	}
	if preview.Link != "subsidiary" || preview.Base != before || preview.Content != preview.Slice.Fingerprint() || preview.Slice.Tenant != "source" || len(preview.Changes) == 0 || preview.ConnectionRevision == 0 {
		t.Fatalf("preview lost its source or confirmation identity: %+v", preview)
	}
	if preview.Slice.Elements[0].ID != "remote" || calls.Load() != 1 || writes.Load() != 0 || model.Model().Fingerprint() != before {
		t.Fatal("preview changed its content, fetched twice or wrote tenant state")
	}
	payload := map[string]any{"link": preview.Link, "base": preview.Base, "content": preview.Content, "connectionRevision": preview.ConnectionRevision, "slice": preview.Slice}
	altered := preview.Slice
	altered.Elements = append([]enterpriseapi.Element(nil), altered.Elements...)
	altered.Elements[0].Name = "Not the reviewed name"
	payload["slice"] = altered
	if got := refuse(t, tn, "owner", enterprise.ID, enterprise.SchemaSliceImport, enterprise.ModelType, "altered-import", payload, txnAt); !strings.Contains(got, "differs from its preview") {
		t.Fatalf("modified source content was accepted: %s", got)
	}
	payload["slice"] = preview.Slice
	decide(t, tn, "owner", enterprise.ID, enterprise.SchemaSliceImport, enterprise.ModelType, "source-import", payload, txnAt)
	imported := model.Model()
	element := imported.ElementOf(enterpriseapi.ImportedElementID("source", "remote"))
	if element == nil || element.Owner != "tenant:source" || calls.Load() != 1 {
		t.Fatal("confirmation did not import the reviewed content through its original owner")
	}
	if got := refuse(t, tn, "owner", enterprise.ID, enterprise.SchemaSliceImport, enterprise.ModelType, "stale-import", payload, txnAt); !strings.Contains(got, "changed") {
		t.Fatalf("stale preview was accepted: %s", got)
	}
}

func TestEnterpriseLinkPreviewRejectsChangedConnectionAndModel(t *testing.T) {
	for _, change := range []string{"connection", "model", "tenant", "root", "uaf", "credential", "draft"} {
		t.Run(change, func(t *testing.T) {
			tn, model, _, _ := tenantLinkPreviewSetup(t)
			if change == "credential" {
				tn.Secrets = func(string) ([]byte, bool) { return nil, false }
			}
			if change == "draft" {
				decide(t, tn, "owner", construction.ID, construction.ConnectionType+".retire", construction.ConnectionType, "source", map[string]any{}, txnAt)
			}
			if change == "tenant" || change == "root" {
				tenant, root := "source", "remote"
				if change == "tenant" {
					tenant = "another"
				} else {
					root = "not-shared"
				}
				decide(t, tn, "owner", enterprise.ID, enterprise.SchemaTenantLinkSave, enterprise.TenantLinkType, "subsidiary", map[string]any{"organization": "group", "tenant": tenant, "root": root, "connection": "source"}, txnAt)
			}
			tn.Outbound = func(request *http.Request, private bool) (*http.Response, error) {
				// A decision can enter while the source is being fetched. If preview
				// held the tenant lock, this original owner decision would deadlock.
				if change == "connection" {
					decide(t, tn, "owner", construction.ID, construction.ConnectionType+".edit", construction.ConnectionType, "source", map[string]any{"title": "Changed after preview"}, txnAt)
				}
				if change == "model" {
					decide(t, tn, "owner", enterprise.ID, enterprise.SchemaElementEdit, enterprise.ElementType, "group", map[string]any{"name": "Changed group"}, txnAt)
				}
				if change == "tenant" || change == "uaf" {
					slice := enterpriseapi.Slice{Tenant: "source", UAF: model.Model().UAF, Elements: []enterpriseapi.Element{{ID: "remote", Name: "Remote", Stereotype: enterpriseapi.Organization}}, Relationships: []enterpriseapi.Relationship{}}
					if change == "tenant" {
						slice.Tenant = "another-wrong-source"
					} else {
						slice.UAF = "unrelated-version"
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(platform.Raw(slice)))}, nil
				}
				return guarded(request, private)
			}
			if _, err := tn.PreviewEnterpriseLink(context.Background(), memberOf(t, tn, "owner"), "subsidiary", txnAt); err == nil {
				t.Fatalf("%s changed but preview remained usable", change)
			}
		})
	}
}

func TestEnterpriseLinkImportRejectsConnectionChangedAfterPreview(t *testing.T) {
	tn, model, _, calls := tenantLinkPreviewSetup(t)
	preview, err := tn.PreviewEnterpriseLink(context.Background(), memberOf(t, tn, "owner"), "subsidiary", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	before := model.Model().Fingerprint()
	decide(t, tn, "owner", construction.ID, construction.ConnectionType+".edit", construction.ConnectionType, "source", map[string]any{"title": "Revised connection"}, txnAt)
	answer := refuse(t, tn, "owner", enterprise.ID, enterprise.SchemaSliceImport, enterprise.ModelType, "stale-connection", map[string]any{"link": preview.Link, "base": preview.Base, "content": preview.Content, "connectionRevision": preview.ConnectionRevision, "slice": preview.Slice}, txnAt)
	if !strings.Contains(answer, "source connection changed") || model.Model().Fingerprint() != before || calls.Load() != 1 {
		t.Fatalf("confirmation accepted a changed connection or refetched the source: %s", answer)
	}
}
