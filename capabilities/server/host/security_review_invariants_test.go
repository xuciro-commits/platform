package host

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimekernel "platformserver/kernel"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	agentowner "platformserver/services/ai/delegation"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
	filesowner "platformserver/services/resources/files"
	controlowner "platformserver/services/security/control"
	"platformserver/services/security/idp"
	"platformserver/services/work"
)

func TestInvariantSecurityReviewKeysAndTokens(t *testing.T) {
	root := filepath.Clean("../../..")
	for _, rel := range []string{"capabilities", "frontend/src", "scripts", "deploy"} {
		err := filepath.WalkDir(filepath.Join(root, rel), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "node_modules" || d.Name() == "dist" || d.Name() == ".build" {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.Contains(raw, []byte("development-only")) {
				t.Fatalf("hardcoded fallback key literal remained in %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("PLATFORM_PERSONAL_TOKEN_KEY", "too-short")
	if _, err := controlowner.ConfigurePersonalTokens(""); err == nil {
		t.Fatal("expected short PLATFORM_PERSONAL_TOKEN_KEY to be rejected even without OIDC issuer")
	}
	t.Setenv("PLATFORM_PERSONAL_TOKEN_KEY", "")
	if _, err := controlowner.ConfigurePersonalTokens("https://idp.example.com"); err == nil {
		t.Fatal("expected missing PLATFORM_PERSONAL_TOKEN_KEY with OIDC issuer to be rejected")
	}

	controlowner.UseTokenKey(nil)
	agentowner.UseTokenKey([]byte("too-short"))
	defer func() {
		agentowner.UseTokenKey([]byte("invariant-delegation-signing-key"))
	}()

	seats := []runtimeprotocol.Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, runtimeprotocol.AgentApp: runtimeprotocol.AgentAdmin}}},
	}
	apps := func(id string) []platform.Service {
		return []platform.Service{work.New(id, actions.NewAuthority), agentowner.New(id, actions.NewAuthority)}
	}
	live := newInvariantTenantWithApps(t, "invariant-sec-keys", apps("invariant-sec-keys"), seats...)
	admin, _ := live.Member("admin")

	issueSub := live.submission(controlowner.SchemaTokenIssue, controlowner.TokenType, "tok-nokey", map[string]any{"label": "CI"})
	issueSub.Authority = controlowner.PlatformApp
	issueSub.PrincipalId = admin.ID
	if _, refusal := live.Submit(admin, issueSub, invariantClock); refusal == nil {
		t.Fatal("expected personal token issue without host signing key to be refused")
	}

	wkrSub := live.submission(runtimeprotocol.SchemaDelegationWorkerKey, runtimeprotocol.DelegationType, "wkr-nokey", map[string]any{"name": "worker"})
	wkrSub.Authority = runtimeprotocol.AgentApp
	wkrSub.PrincipalId = admin.ID
	if _, refusal := live.Submit(admin, wkrSub, invariantClock); refusal == nil {
		t.Fatal("expected delegation worker key issue without host signing key to be refused")
	}
	if tok := live.Agents.MintWorkerToken("wkr-nokey"); tok != "" {
		t.Fatalf("minted worker token without host key: %s", tok)
	}

	legacyMac := hmac.New(sha256.New, []byte("development-only"))
	legacyMac.Write([]byte(live.ID + "/tok-1/chg-1"))
	legacyTok := controlowner.TokenPrefix + base64.RawURLEncoding.EncodeToString(legacyMac.Sum(nil)[:24])
	if _, ok := consoleOf(live.Tenant).MemberByToken(legacyTok, invariantClock); ok {
		t.Fatal("legacy development-only token resolved")
	}

	validKey := []byte("0123456789abcdef0123456789abcdef")
	controlowner.UseTokenKey(validKey)
	defer controlowner.UseTokenKey(nil)
	agentowner.UseTokenKey(validKey)

	host, handler := observationHost(live.Tenant)
	now := invariantClock
	host.Now = func() time.Time { return now }

	issueOK := live.submission(controlowner.SchemaTokenIssue, controlowner.TokenType, "tok-1", map[string]any{
		"label": "Deploy script", "until": now.Add(48 * time.Hour).UTC().Format(time.DateOnly),
	})
	issueOK.Authority = controlowner.PlatformApp
	issueOK.PrincipalId = admin.ID
	if _, refusal := live.Submit(admin, issueOK, now); refusal != nil {
		t.Fatalf("issue personal token with valid key refused: %+v", refusal)
	}

	secReq := httptest.NewRequest(http.MethodGet, "/v1/tokens/tok-1/secret", nil)
	secReq.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "admin"))
	secReq.Header.Set(TenantHeader, live.ID)
	secRes := httptest.NewRecorder()
	handler.ServeHTTP(secRes, secReq)
	if secRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on first secret read, got %d: %s", secRes.Code, secRes.Body)
	}
	var minted map[string]string
	_ = json.Unmarshal(secRes.Body.Bytes(), &minted)
	pat := minted["secret"]
	if !strings.HasPrefix(pat, controlowner.TokenPrefix) {
		t.Fatalf("unexpected minted secret: %q", pat)
	}

	secRes2 := httptest.NewRecorder()
	handler.ServeHTTP(secRes2, httptest.NewRequest(http.MethodGet, "/v1/tokens/tok-1/secret", nil))
	secAgain := httptest.NewRequest(http.MethodGet, "/v1/tokens/tok-1/secret", nil)
	secAgain.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "admin"))
	secAgain.Header.Set(TenantHeader, live.ID)
	secAgainRes := httptest.NewRecorder()
	handler.ServeHTTP(secAgainRes, secAgain)
	if secAgainRes.Code != http.StatusNotFound {
		t.Fatalf("expected second secret read to return 404, got %d", secAgainRes.Code)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+pat)
	meReq.Header.Set(TenantHeader, live.ID)
	meRes := httptest.NewRecorder()
	handler.ServeHTTP(meRes, meReq)
	if meRes.Code != http.StatusOK {
		t.Fatalf("personal token /v1/me failed: %d %s", meRes.Code, meRes.Body)
	}

	now = now.Add(72 * time.Hour)
	expRes := httptest.NewRecorder()
	expReq := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	expReq.Header.Set("Authorization", "Bearer "+pat)
	expReq.Header.Set(TenantHeader, live.ID)
	handler.ServeHTTP(expRes, expReq)
	if expRes.Code != http.StatusUnauthorized {
		t.Fatalf("expired personal token must return 401, got %d", expRes.Code)
	}
}

func TestInvariantSecurityReviewIdentityBoundaries(t *testing.T) {
	agentowner.UseTokenKey([]byte("invariant-delegation-signing-key"))
	seats := []runtimeprotocol.Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, work.ID: work.Admin, runtimeprotocol.AgentApp: runtimeprotocol.AgentAdmin}}},
		{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{"invariantproject": "reader"}}},
	}
	apps := func(id string) []platform.Service {
		return []platform.Service{work.New(id, actions.NewAuthority), agentowner.New(id, actions.NewAuthority)}
	}
	live := newInvariantTenantWithApps(t, "invariant-sec-id", apps("invariant-sec-id"), seats...)
	other := newInvariantTenantWithApps(t, "invariant-sec-other", apps("invariant-sec-other"), seats...)
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingLogRequests, "100")
	_, handler := observationHost(live.Tenant, other.Tenant)

	postSub := func(token, tenant string, headers map[string]string, sub *pb.Submission) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := protojson.Marshal(sub)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/submissions?tenant="+other.ID+"&member=admin&principalId=admin&approvedBy=admin&hops=0", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(TenantHeader, tenant)
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	forgedPrincipal := live.submission("build.app.create", "build.app", "FORGED-P", map[string]any{"name": "forgedp", "title": "Forged"})
	forgedPrincipal.PrincipalId = "admin"
	if res := postSub("other-"+live.ID, live.ID, map[string]string{"X-Principal-Id": "admin", "X-Approved-By": "admin"}, forgedPrincipal); res.Code != http.StatusForbidden {
		t.Fatalf("forged principalId in submission must return 403, got %d: %s", res.Code, res.Body)
	}

	forgedTenant := live.submission("build.app.create", "build.app", "FORGED-T", map[string]any{"name": "forgedt", "title": "Forged"})
	forgedTenant.TenantId = other.ID
	if res := postSub("admin-"+live.ID, live.ID, nil, forgedTenant); res.Code != http.StatusForbidden {
		t.Fatalf("forged tenantId in submission must return 403, got %d: %s", res.Code, res.Body)
	}

	for _, internal := range []struct {
		authority, schema, targetType string
		payload                       map[string]any
	}{
		{work.ID, runtimeprotocol.WorkQuestionRequestSchema, runtimeprotocol.WorkQuestionType, map[string]any{
			"requester": "builder", "title": "Forged ask", "options": []string{"approve", "reject"},
			"caller": map[string]any{"kind": "delegation", "name": "forged", "approvedBy": "admin", "hops": 0},
		}},
		{work.ID, work.SchemaRequest, work.ApprovalType, map[string]any{"requester": "builder"}},
		{runtimeprotocol.AgentApp, runtimeprotocol.SchemaDelegationIngest, runtimeprotocol.DelegationEventType, map[string]any{
			"event": map[string]any{"id": "ev-forged", "type": "x", "hops": -99},
		}},
		{runtimeprotocol.AgentApp, runtimeprotocol.SchemaDelegationClaim, runtimeprotocol.DelegationJobType, map[string]any{"claim": "c1"}},
		{runtimeprotocol.AgentApp, runtimeprotocol.SchemaDelegationStarted, runtimeprotocol.DelegationJobType, map[string]any{"token": "t1"}},
		{runtimeprotocol.AgentApp, runtimeprotocol.SchemaDelegationReceipt, runtimeprotocol.DelegationJobType, map[string]any{"token": "t1", "digest": "d1", "receipt": map[string]any{"outcome": "done"}}},
	} {
		sub := live.submission(internal.schema, internal.targetType, "INTERNAL-FORGED", internal.payload)
		sub.Authority = internal.authority
		sub.PrincipalId = "admin"
		res := postSub("admin-"+live.ID, live.ID, nil, sub)
		if res.Code == http.StatusOK {
			t.Fatalf("human admin must not directly invoke host-internal schema %s via /v1/submissions: %s", internal.schema, res.Body)
		}
	}

	simBody, _ := protojson.Marshal(live.submission("build.app.create", "build.app", "SIM-FORGED", map[string]any{"name": "simforged", "title": "Sim"}))
	simReq := httptest.NewRequest(http.MethodPost, "/v1/simulate?as=admin", bytes.NewReader(simBody))
	simReq.Header.Set("Authorization", "Bearer other-"+live.ID)
	simReq.Header.Set(TenantHeader, live.ID)
	simRes := httptest.NewRecorder()
	handler.ServeHTTP(simRes, simReq)
	if simRes.Code != http.StatusForbidden {
		t.Fatalf("non-builder ?as=admin on /v1/simulate must return 403, got %d: %s", simRes.Code, simRes.Body)
	}

	adminPing := observationRequest(t, handler, live.ID, "admin-"+live.ID, "/v1/me", nil)
	if adminPing.Code != http.StatusOK {
		t.Fatal(adminPing.Body.String())
	}
	memberLogs := observationRequest(t, handler, live.ID, "other-"+live.ID, "/v1/monitoring/logs", url.Values{"member": {"admin"}})
	if memberLogs.Code != http.StatusOK {
		t.Fatalf("member logs status=%d body=%s", memberLogs.Code, memberLogs.Body)
	}
	var logPage runtimeprotocol.ObservationPage
	_ = json.Unmarshal(memberLogs.Body.Bytes(), &logPage)
	for _, entry := range logPage.Entries {
		if entry.Member != "other" {
			t.Fatalf("non-admin ?member=admin leaked another member's observation: %+v", entry)
		}
	}

	admin, _ := live.Member("admin")
	wkrSub := live.submission(runtimeprotocol.SchemaDelegationWorkerKey, runtimeprotocol.DelegationType, "wkr-sec", map[string]any{"name": "sec-worker"})
	wkrSub.Authority = runtimeprotocol.AgentApp
	wkrSub.PrincipalId = admin.ID
	if _, refusal := live.Submit(admin, wkrSub, invariantClock); refusal != nil {
		t.Fatalf("worker key setup failed: %+v", refusal)
	}
	wkrTok := live.Agents.MintWorkerToken("wkr-sec")
	evRaw, _ := json.Marshal(map[string]any{"id": "neg-hop-1", "type": "custom.evt", "hops": -50})
	evReq := httptest.NewRequest(http.MethodPost, "/autoloop/"+live.ID+"/v1/events", bytes.NewReader(evRaw))
	evReq.Header.Set("Authorization", "Bearer "+wkrTok)
	evReq.Header.Set("Content-Type", "application/json")
	evRes := httptest.NewRecorder()
	handler.ServeHTTP(evRes, evReq)
	if evRes.Code != http.StatusOK {
		t.Fatalf("autoloop event failed: %d %s", evRes.Code, evRes.Body)
	}
	evPage, refusal := live.objectOwner(live.Projection).Records(admin, runtimeprotocol.DelegationEventType, platform.Query{Limit: 10}, invariantClock)
	if refusal != nil || len(evPage.Records) != 1 {
		t.Fatalf("read delegation event failed: err=%+v count=%d", refusal, len(evPage.Records))
	}
	rawEv, _ := json.Marshal(evPage.Records[0])
	if strings.Contains(string(rawEv), "-50") {
		t.Fatalf("negative hops from payload persisted on delegation event: %s", rawEv)
	}
}

func TestInvariantSecurityReviewOutboundGuard(t *testing.T) {
	actions.SetAllowPrivateOutbound(false)
	packages.SetAllowLoopback(false)
	defer func() {
		actions.SetAllowPrivateOutbound(false)
		packages.SetAllowLoopback(false)
	}()

	restricted := []string{
		"127.0.0.1:443",
		"[::1]:443",
		"[::ffff:127.0.0.1]:443",
		"0.0.0.1:443",
		"10.0.0.8:443",
		"172.16.5.4:443",
		"192.168.1.10:443",
		"169.254.169.254:80",
		"100.64.0.1:443",
		"[fe80::1]:443",
		"[fc00::1]:443",
	}
	dialerDefault := actions.GuardedDialer(false)
	dialerTenantPrivate := actions.GuardedDialer(true)
	for _, addr := range restricted {
		if err := dialerDefault.Control("tcp", addr, nil); !errors.Is(err, actions.ErrPrivate) {
			t.Fatalf("default dialer allowed restricted address %s: %v", addr, err)
		}
		if err := dialerTenantPrivate.Control("tcp", addr, nil); !errors.Is(err, actions.ErrPrivate) {
			t.Fatalf("tenant allowPrivate without operator permission allowed %s: %v", addr, err)
		}
	}
	if err := dialerDefault.Control("tcp", "93.184.216.34:443", nil); err != nil {
		t.Fatalf("public IP should be allowed: %v", err)
	}

	for _, badPkg := range []string{
		"http://127.0.0.1:8080/pkg.json",
		"https://127.0.0.1:8443/pkg.json",
		"https://10.0.0.1/pkg.json",
		"https://169.254.169.254/latest/meta-data/",
		"https://100.64.0.5/pkg.json",
	} {
		if _, _, err := packages.ValidatePackageURL(badPkg); err == nil {
			t.Fatalf("ValidatePackageURL should reject internal/loopback URL %s by default", badPkg)
		}
	}

	redirectTarget, _ := url.Parse("https://packages.example.com/release.json")
	_, dlErr := packages.Download(t.Context(), redirectTarget, false, func(req *http.Request, _ bool) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://169.254.169.254/latest/meta-data/"}},
			Body:       http.NoBody,
		}, nil
	})
	if dlErr == nil {
		t.Fatal("package download redirect to link-local metadata address must be refused")
	}

	packages.SetAllowLoopback(true)
	if _, allowLoopback, err := packages.ValidatePackageURL("http://127.0.0.1:8080/pkg.json"); err != nil || !allowLoopback {
		t.Fatalf("operator -allow-loopback-packages should allow loopback HTTP: err=%v allow=%v", err, allowLoopback)
	}
	if _, _, err := packages.ValidatePackageURL("https://169.254.169.254/latest/meta-data/"); err == nil {
		t.Fatal("operator -allow-loopback-packages must still reject link-local metadata IP")
	}
	if _, _, err := packages.ValidatePackageURL("https://10.0.0.5/pkg.json"); err == nil {
		t.Fatal("operator -allow-loopback-packages must still reject RFC1918 private IP")
	}

	actions.SetAllowPrivateOutbound(true)
	dialerOperatorAllowed := actions.GuardedDialer(true)
	if err := dialerOperatorAllowed.Control("tcp", "10.0.0.8:443", nil); err != nil {
		t.Fatalf("operator-enabled allowPrivate should permit private address when endpoint opts in: %v", err)
	}
	if err := actions.GuardedDialer(false).Control("tcp", "10.0.0.8:443", nil); !errors.Is(err, actions.ErrPrivate) {
		t.Fatalf("operator-enabled allowPrivate must still block endpoints with AllowPrivate=false: %v", err)
	}
}

func TestInvariantSecurityReviewUploadAndRenderIsolation(t *testing.T) {
	seats := []runtimeprotocol.Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin}}},
	}
	apps := func(id string) []platform.Service {
		return []platform.Service{filesowner.New(id, actions.NewAuthority)}
	}
	live := newInvariantTenantWithApps(t, "invariant-sec-upload", apps("invariant-sec-upload"), seats...)
	live.settings.Set(filesowner.ID+"/"+filesowner.SettingMax, "1")
	_, handler := observationHost(live.Tenant)

	live.accept(t, "build.object.create", build.ObjectType, "DOC", invariantObject("doc", "Doc"))
	live.accept(t, "build.object.publish", build.ObjectType, "DOC", nil)
	live.accept(t, "invariantproject.doc.create", "invariantproject.doc", "D1", map[string]any{"note": "hello"})

	upload := func(name, contentType string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/files?name="+url.QueryEscape(name), bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
		req.Header.Set(TenantHeader, live.ID)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if res := upload("empty.bin", "application/octet-stream", nil); res.Code != http.StatusBadRequest {
		t.Fatalf("empty upload should return 400, got %d", res.Code)
	}
	if res := upload("big.bin", "application/octet-stream", bytes.Repeat([]byte("a"), (1<<20)+1)); res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload should return 413, got %d", res.Code)
	}
	for _, spoof := range []struct {
		name, ctype string
		body        []byte
	}{
		{"xss.png", "image/png", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`)},
		{"xss.pdf", "application/pdf", []byte(`<!doctype html><script>alert(1)</script>`)},
		{"xss.jpg", "image/jpeg", []byte(`<html><body>not jpeg</body></html>`)},
		{"evil.svg", "image/svg+xml", []byte(`<svg onload="alert(1)"/>`)},
	} {
		res := upload(spoof.name, spoof.ctype, spoof.body)
		if res.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("spoofed or active SVG/HTML upload %s (%s) must return 415, got %d: %s", spoof.name, spoof.ctype, res.Code, res.Body)
		}
	}

	pdfBytes := []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n%%EOF\n")
	upRes := upload("spec.pdf", "application/pdf", pdfBytes)
	if upRes.Code != http.StatusOK {
		t.Fatalf("valid PDF upload failed: %d %s", upRes.Code, upRes.Body)
	}
	var uploaded Upload
	_ = json.Unmarshal(upRes.Body.Bytes(), &uploaded)
	attachSub := live.submission(filesowner.SchemaAttach, filesowner.FileType, "F-PDF-1", map[string]any{
		"hash": uploaded.Hash, "name": uploaded.Name, "contentType": uploaded.ContentType, "size": uploaded.Size, "target": "invariantproject.doc/D1",
	})
	attachSub.Authority = filesowner.ID
	if _, refusal := live.Submit(live.member(t), attachSub, invariantClock); refusal != nil {
		t.Fatalf("attach PDF failed: %+v", refusal)
	}

	dlReq := httptest.NewRequest(http.MethodGet, "/v1/files/F-PDF-1", nil)
	dlReq.Header.Set("Authorization", "Bearer "+observationToken(live.ID, "builder"))
	dlReq.Header.Set(TenantHeader, live.ID)
	dlRes := httptest.NewRecorder()
	handler.ServeHTTP(dlRes, dlReq)
	if dlRes.Code != http.StatusOK {
		t.Fatalf("download PDF failed: %d", dlRes.Code)
	}
	if csp := dlRes.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("missing sandbox CSP on file download: %q", csp)
	}
	if disp := dlRes.Header().Get("Content-Disposition"); !strings.HasPrefix(disp, "attachment;") {
		t.Fatalf("missing attachment Content-Disposition: %q", disp)
	}
	if dlRes.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header")
	}

	previewSrc, err := os.ReadFile(filepath.Clean("../../../frontend/src/pages/MediaPreview.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(previewSrc, []byte("enableXfa: false")) || !bytes.Contains(previewSrc, []byte("disableFontFace: true")) {
		t.Fatal("frontend MediaPreview.tsx must disable pdfjs XFA and font faces")
	}
}

func TestInvariantSecurityReviewSessionsAndCSRF(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	signer, err := idp.NewLocal(key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		controlowner.UseTokenKey(nil)
		agentowner.UseTokenKey([]byte("invariant-delegation-signing-key"))
	}()

	now := invariantClock
	live := newObservationTenant(t, "invariant-sec-sessions")
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingSessionHours, "2")
	host := NewHost(nil, live.Tenant)
	host.Now = func() time.Time { return now }
	host.SignWith(signer, 24*time.Hour)
	handler := host.Handler()

	tokDesktop, err := signer.Mint("admin", 24*time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	tokMobile, err := signer.Mint("admin", 24*time.Hour, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	tokUnused, err := signer.Mint("admin", 24*time.Hour, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}

	call := func(method, path, tok, ua string, extra map[string]string, body []byte) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "http://workspace.local"+path, bytes.NewReader(body))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		req.Header.Set(TenantHeader, live.ID)
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range extra {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if res := call(http.MethodGet, "/v1/me", tokDesktop, "Chrome/130.0 (Macintosh; Intel Mac OS X)", nil, nil); res.Code != http.StatusOK {
		t.Fatalf("desktop session failed: %d %s", res.Code, res.Body)
	}
	if res := call(http.MethodGet, "/v1/me", tokMobile, "Safari/18.0 (iPhone; CPU iPhone OS)", nil, nil); res.Code != http.StatusOK {
		t.Fatalf("mobile session failed: %d %s", res.Code, res.Body)
	}
	if got := call(http.MethodGet, "/v1/me", tokDesktop, "", nil, nil).Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Fatalf("wildcard Access-Control-Allow-Origin must not be emitted: %q", got)
	}

	if res := call(http.MethodPost, "/v1/sessions/end-others", tokDesktop, "", map[string]string{"Sec-Fetch-Site": "cross-site"}, []byte("{}")); res.Code != http.StatusForbidden {
		t.Fatalf("cross-site Sec-Fetch-Site write must be refused with 403, got %d: %s", res.Code, res.Body)
	}
	if res := call(http.MethodPost, "/v1/sessions/end-others", tokDesktop, "", map[string]string{"Origin": "https://evil.example"}, []byte("{}")); res.Code != http.StatusForbidden {
		t.Fatalf("cross-origin Origin write must be refused with 403, got %d: %s", res.Code, res.Body)
	}
	if res := call(http.MethodPost, "/v1/sessions/end-others", "", "", map[string]string{"Cookie": "session=abc"}, []byte("{}")); res.Code != http.StatusForbidden {
		t.Fatalf("cookie write without same-origin proof must be refused with 403, got %d: %s", res.Code, res.Body)
	}
	if res := call(http.MethodPost, "/v1/sessions/end-others", tokDesktop, "", map[string]string{"Origin": "http://workspace.local", "Sec-Fetch-Site": "same-origin"}, []byte("{}")); res.Code != http.StatusOK {
		t.Fatalf("same-origin write should succeed, got %d: %s", res.Code, res.Body)
	}
	if res := call(http.MethodGet, "/v1/me", tokMobile, "", nil, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("revoked other session must return 401, got %d", res.Code)
	}

	tokTablet, _ := signer.Mint("admin", 24*time.Hour, now.Add(3*time.Second))
	if res := call(http.MethodGet, "/v1/me", tokTablet, "Safari/18.0 (iPad)", nil, nil); res.Code != http.StatusOK {
		t.Fatalf("tablet session failed: %d", res.Code)
	}
	sessRes := call(http.MethodGet, "/v1/sessions", tokDesktop, "", nil, nil)
	var sessions []runtimeprotocol.Session
	_ = json.Unmarshal(sessRes.Body.Bytes(), &sessions)
	tabletID := ""
	for _, s := range sessions {
		if !s.Current && s.Ended.IsZero() {
			tabletID = s.ID
		}
	}
	if tabletID == "" {
		t.Fatalf("expected active tablet session in %+v", sessions)
	}
	if res := call(http.MethodPost, "/v1/sessions/"+tabletID+"/end", tokDesktop, "", nil, []byte("{}")); res.Code != http.StatusOK {
		t.Fatalf("single session revoke failed: %d %s", res.Code, res.Body)
	}
	if res := call(http.MethodGet, "/v1/me", tokTablet, "", nil, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("individually revoked session must return 401, got %d", res.Code)
	}
	if res := call(http.MethodGet, "/v1/me", tokDesktop, "", nil, nil); res.Code != http.StatusOK {
		t.Fatalf("desktop session should remain active after revoking tablet: %d", res.Code)
	}

	now = now.Add(10 * time.Second)
	if res := call(http.MethodPost, "/v1/sessions/end-all", tokDesktop, "", nil, []byte("{}")); res.Code != http.StatusOK {
		t.Fatalf("end-all sessions failed: %d %s", res.Code, res.Body)
	}
	if res := call(http.MethodGet, "/v1/me", tokDesktop, "", nil, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("current session must be revoked after end-all, got %d", res.Code)
	}
	if res := call(http.MethodGet, "/v1/me", tokUnused, "", nil, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("previously minted unused JWT must be rejected after end-all, got %d", res.Code)
	}

	now = now.Add(time.Second)
	tokFresh, _ := signer.Mint("admin", 24*time.Hour, now)
	if res := call(http.MethodGet, "/v1/me", tokFresh, "Chrome/130.0", nil, nil); res.Code != http.StatusOK {
		t.Fatalf("fresh post-revocation token should work: %d %s", res.Code, res.Body)
	}
	now = now.Add(2*time.Hour + time.Minute)
	if res := call(http.MethodGet, "/v1/me", tokFresh, "Chrome/130.0", nil, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("session exceeding SettingSessionHours must expire with 401, got %d", res.Code)
	}
}

func TestInvariantSecurityReviewLogRedaction(t *testing.T) {
	live := newObservationTenant(t, "invariant-sec-logs")
	live.observeOwnerRun(
		invariantClock,
		"worker",
		"run-redact-1",
		"data",
		"https://svc_user:SuperSecretPass!@internal.example.com/v1/export/crm.lead?X-Amz-Signature=abcdef123456&token=tok_999",
		"failed",
		"Delivery code=482910",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJhZG1pbiJ9.sigvalue pat_AbCdEfGhIjKlMnOpQrStUvWx dwkr_t_w_1234567890abcdef dmcp_t_1_1_1234567890abcdef secret=my-raw-secret otp=739182 /v1/ai/training/export?format=sft",
	)
	page := storedObservations(t, live, runtimeprotocol.ObservationQuery{
		From: invariantClock.Add(-time.Minute), To: invariantClock.Add(time.Minute), Limit: 10,
	})
	if len(page.Entries) != 1 {
		t.Fatalf("expected 1 observation, got %d", len(page.Entries))
	}
	raw, _ := json.Marshal(page.Entries[0])
	text := string(raw)
	for _, forbidden := range []string{
		"SuperSecretPass!",
		"abcdef123456",
		"tok_999",
		"eyJhbGciOiJIUzI1NiJ9",
		"pat_AbCdEfGhIjKlMnOpQrStUvWx",
		"dwkr_t_w_1234567890abcdef",
		"dmcp_t_1_1_1234567890abcdef",
		"my-raw-secret",
		"482910",
		"739182",
		"/v1/export/crm.lead",
		"/v1/ai/training/export",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("sensitive value %q leaked into stored observation: %s", forbidden, text)
		}
	}
	redacted := runtimekernel.RedactLogText("exported to https://ak:sk@s3.local/bucket/v1/export/deals?code=654321&secret=topsecret")
	for _, forbidden := range []string{"ak:sk", "654321", "topsecret", "/v1/export/deals"} {
		if strings.Contains(redacted, forbidden) {
			t.Fatalf("RedactLogText leaked %q: %s", forbidden, redacted)
		}
	}
}
