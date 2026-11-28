package host

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
	filesowner "platformserver/services/resources/files"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newUsageTenant(tb testing.TB, id string) *invariantTenant {
	tb.Helper()
	seats := []protocol.Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, ai.ID: ai.Admin}}},
		{Subjects: []string{"auditor"}, Member: platform.Member{ID: "auditor", Roles: map[string]string{controlowner.PlatformApp: protocol.Auditor}}},
	}
	apps := []platform.Service{
		ai.New(id, actions.NewAuthority),
		filesowner.New(id, actions.NewAuthority),
		monitoring.New(id, actions.NewAuthority),
	}
	live := newInvariantTenantWithApps(tb, id, apps, seats...)
	live.settings.Set(controlowner.PlatformApp+"/"+controlowner.SettingLogRequests, "0")
	return live
}

func readTenantUsageHTTP(tb testing.TB, handler http.Handler, tenant, token string) *httptest.ResponseRecorder {
	tb.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/monitoring/usage", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(TenantHeader, tenant)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func decodeTenantUsage(tb testing.TB, res *httptest.ResponseRecorder) protocol.TenantUsage {
	tb.Helper()
	if res.Code != http.StatusOK {
		tb.Fatalf("usage HTTP %d: %s", res.Code, res.Body.String())
	}
	var out protocol.TenantUsage
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		tb.Fatal(err)
	}
	return out
}

func TestInvariantK11UsageProjectionRebuildAndIsolation(t *testing.T) {
	first := newUsageTenant(t, "usage-first")
	second := newUsageTenant(t, "usage-second")

	dayOne := invariantClock
	dayTwo := invariantClock.Add(24 * time.Hour)

	first.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("ticket", "Ticket"))
	first.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	first.accept(t, "invariantproject.ticket.create", "invariantproject.ticket", "T1", map[string]any{"note": "first active"})
	first.accept(t, "invariantproject.ticket.create", "invariantproject.ticket", "T2", map[string]any{"note": "second active"})
	first.accept(t, "invariantproject.ticket.create", "invariantproject.ticket", "T3", map[string]any{"note": "archived soon"})
	first.accept(t, "invariantproject.ticket.archive", "invariantproject.ticket", "T3", map[string]any{})

	builder := first.member(t)
	badCreate := first.submission("invariantproject.ticket.create", "invariantproject.ticket", "T1", map[string]any{"note": "duplicate id"})
	if _, refusal := first.Submit(builder, badCreate, dayOne); refusal == nil {
		t.Fatal("duplicate create should be refused")
	}

	content := strings.Repeat("x", 4096)
	uploaded, status, err := first.Upload(builder, "manual.txt", "text/plain", strings.NewReader(content), dayTwo)
	if err != nil || status != http.StatusOK {
		t.Fatalf("upload %d: %v", status, err)
	}
	attach := first.submission(filesowner.SchemaAttach, platform.FileType, "F1", map[string]any{
		"hash": uploaded.Hash, "size": uploaded.Size, "name": uploaded.Name,
		"contentType": uploaded.ContentType, "target": "invariantproject.ticket/T1",
	})
	attach.Authority = platform.FileApp
	if _, refusal := first.Submit(builder, attach, dayTwo); refusal != nil {
		t.Fatalf("attach: %v", refusal)
	}

	admin, _ := first.Member("admin")
	if refusal := first.acceptUsage(admin, platform.AIUsage{
		Call: "call-1", At: dayOne.Add(time.Minute), Member: admin.ID, Model: "openai/gpt-4o",
		Input: 1200, Output: 300, TokensReported: true, Cost: 0.015, CostReported: true, Outcome: "ok",
	}); refusal != nil {
		t.Fatalf("accept usage 1: %v", refusal)
	}
	if refusal := first.acceptUsage(admin, platform.AIUsage{
		Call: "call-2", At: dayTwo.Add(2 * time.Minute), Member: admin.ID, Model: "openai/gpt-4o",
		Input: 500, Output: 500, TokensReported: true, Cost: 0.010, CostReported: true, Outcome: "ok",
	}); refusal != nil {
		t.Fatalf("accept usage 2: %v", refusal)
	}

	pkgSource := newInvariantTenant(t, "usage-pkg-source")
	pkgSource.accept(t, "build.object.create", build.ObjectType, "POBJ", invariantObject("alpha", "Alpha"))
	pkgSource.accept(t, "build.app.create", protocol.AppType, "PAPP", map[string]any{
		"name": "alphaapp", "title": "Alpha app",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.alpha"}},
	})
	candID := invariantPublishData(t, pkgSource, pkgSource.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "POBJ"},
		{Kind: platform.AssetApp, ID: "PAPP"},
	}, "usage-pkg")
	cand, err := PublicationResolver().ReadCandidate(candID, pkgSource.releases.Raw(candID))
	if err != nil {
		t.Fatal(err)
	}
	desc := platform.PackageDescriptor{
		ID: "alpha-pack", Version: "1.0.0", Namespace: "alphapack", Title: "Alpha Pack",
		Blueprint: &platform.PackageBlueprint{
			Roots:  []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "alphaapp"}},
			Assets: cand.Assets,
		},
	}
	inspected, err := first.InspectPackage(context.Background(), protocol.PackageInspectRequest{Content: platform.Raw(desc)})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		schema  string
		payload any
	}{
		{packages.SchemaPackageImport, map[string]any{"id": desc.ID, "digest": inspected.Digest}},
		{packages.SchemaPackageInstall, map[string]any{"id": desc.ID, "version": desc.Version, "namespace": desc.Namespace}},
	} {
		sub := first.submission(step.schema, protocol.PackageType, desc.ID, step.payload)
		sub.Authority, sub.PrincipalId = controlowner.PlatformApp, admin.ID
		if _, refusal := first.Submit(admin, sub, dayTwo); refusal != nil {
			t.Fatalf("%s: %v", step.schema, refusal)
		}
	}

	host := NewHost(Tokens(map[string]string{
		"admin-first":   "admin",
		"auditor-first": "auditor",
		"builder-first": "builder",
		"admin-second":  "admin",
	}), first.Tenant, second.Tenant)
	host.Now = func() time.Time { return dayTwo.Add(10 * time.Minute) }
	handler := host.Handler()

	denied := readTenantUsageHTTP(t, handler, first.ID, "builder-first")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("ordinary member read usage: HTTP %d: %s", denied.Code, denied.Body.String())
	}

	entriesBefore := append([]Entry(nil), first.entries...)
	recordsBefore := first.recordImage(t)

	usage := decodeTenantUsage(t, readTenantUsageHTTP(t, handler, first.ID, "admin-first"))
	auditorUsage := decodeTenantUsage(t, readTenantUsageHTTP(t, handler, first.ID, "auditor-first"))
	if !reflect.DeepEqual(usage, auditorUsage) {
		t.Fatalf("admin and auditor usage differed: %+v vs %+v", usage, auditorUsage)
	}
	if !reflect.DeepEqual(entriesBefore, first.entries) {
		t.Fatal("reading usage altered accepted journal entries")
	}
	if changed := differingTypes(recordsBefore, first.recordImage(t)); len(changed) != 0 {
		t.Fatalf("reading usage altered records: %v", changed)
	}

	if usage.AITokens != 2500 || usage.AIInputTokens != 1700 || usage.AIOutputTokens != 800 || usage.AICalls != 2 || usage.AICost != 0.025 {
		t.Fatalf("unexpected AI usage projection: %+v", usage)
	}
	if usage.InstalledPackages != 1 {
		t.Fatalf("installed packages = %d, want 1", usage.InstalledPackages)
	}
	if usage.StorageBytes <= 4096 {
		t.Fatalf("storageBytes = %d, want > 4096 (file attachment + active record images)", usage.StorageBytes)
	}
	if usage.WritesToday != 3 {
		t.Fatalf("writesToday = %d, want 3 (attach + package import + package install)", usage.WritesToday)
	}
	dayOneKey := dayOne.UTC().Format(time.DateOnly)
	dayTwoKey := dayTwo.UTC().Format(time.DateOnly)
	writesByDay := map[string]int64{}
	for _, d := range usage.DailyWrites {
		writesByDay[d.Date] = d.Value
	}
	if writesByDay[dayOneKey] != 7 || writesByDay[dayTwoKey] != 3 {
		t.Fatalf("dailyWrites = %+v, want %s:7 and %s:3 (refused duplicate excluded)", usage.DailyWrites, dayOneKey, dayTwoKey)
	}

	for _, metric := range usage.Metrics {
		var sum int64
		for _, src := range metric.Sources {
			sum += src.Value
		}
		if sum != metric.Used {
			t.Fatalf("metric %s sources sum %d != used %d: %+v", metric.Kind, sum, metric.Used, metric.Sources)
		}
	}

	first.Monitoring.DiscardUsage()
	rebuilt := decodeTenantUsage(t, readTenantUsageHTTP(t, handler, first.ID, "admin-first"))
	if !reflect.DeepEqual(usage, rebuilt) {
		t.Fatalf("discarding and rebuilding usage projection changed output:\ngot:  %+v\nwant: %+v", rebuilt, usage)
	}

	secondUsage := decodeTenantUsage(t, readTenantUsageHTTP(t, handler, second.ID, "admin-second"))
	if secondUsage.AITokens != 0 || secondUsage.AICost != 0 || secondUsage.InstalledPackages != 0 || secondUsage.WritesToday != 0 {
		t.Fatalf("second tenant saw first tenant's usage: %+v", secondUsage)
	}
}
