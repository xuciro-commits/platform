package host

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode"

	"platformserver/kernel/protocol"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ai"
	agentruntime "platformserver/services/ai/runtime"
	automateruntime "platformserver/services/automate/runtime"
	"platformserver/services/devops/packages"
	"platformserver/services/resources/monitoring"
	"platformserver/services/security"
	controlowner "platformserver/services/security/control"
	enterprisemodel "platformserver/services/security/organization"
	"platformserver/services/work"
	"platformserver/services/work/activity"
)

var allowedEnglishTerms = map[string]bool{
	"AI":                          true,
	"CRM":                         true,
	"OIDC":                        true,
	"OAuth":                       true,
	"MCP":                         true,
	"A2A":                         true,
	"HTTP":                        true,
	"JSON":                        true,
	"SQL":                         true,
	"SMTP":                        true,
	"Webhook":                     true,
	"15m":                         true,
	"1h":                          true,
	"24h":                         true,
	"7d":                          true,
	"30d":                         true,
	"zh-CN":                       true,
	"en":                          true,
	"USD":                         true,
	"CNY":                         true,
	"EUR":                         true,
	"GBP":                         true,
	"JPY":                         true,
	"user:<email> or client:<id>": true,
}

func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func TestI18nGateAllNamedReadsClassifiedAndBuiltinManifestsTranslated(t *testing.T) {
	adminSeat := protocol.Seat{Subjects: []string{"admin-i18n"}, Member: platform.Member{ID: "admin-i18n", Language: "zh-CN", Roles: map[string]string{"platform": "admin"}}}
	tn := newInvariantTenantWithApps(
		t,
		"i18n-gate",
		[]platform.Service{
			work.New("i18n-gate", actions.NewAuthority),
			activity.New("i18n-gate", actions.NewAuthority),
			monitoring.New("i18n-gate", actions.NewAuthority),
			enterprisemodel.New("i18n-gate", actions.NewAuthority, platform.OrgSeed{}),
			automateruntime.New("i18n-gate", actions.NewAuthority, PublicationResolver()),
			ai.New("i18n-gate", actions.NewAuthority),
			agentruntime.New("i18n-gate", actions.NewAuthority),
		},
		adminSeat,
	)

	var unclassifiedReads []string
	var untranslated []string
	checkText := func(ctx, src string) {
		src = strings.TrimSpace(src)
		if src == "" || allowedEnglishTerms[src] || !strings.ContainsFunc(src, unicode.IsLetter) || hasCJK(src) {
			return
		}
		said := tn.Say("zh-CN", src)
		if said == src {
			untranslated = append(untranslated, ctx+": "+src)
		}
	}

	for _, svc := range tn.apps {
		mf := svc.Manifest()
		for _, readName := range mf.Reads {
			if _, ok := transport.ReadTranslationPolicies[readName]; !ok && !strings.Contains(readName, ".") {
				unclassifiedReads = append(unclassifiedReads, mf.ID+":"+readName)
			}
		}
		checkText(mf.ID+".Title", mf.Title)
		for _, role := range mf.Roles {
			if role != "" && role != "*" {
				checkText(mf.ID+".Role."+role, role)
			}
		}
		for _, setting := range mf.Settings {
			checkText(mf.ID+".Setting."+setting.Name+".Title", setting.Title)
			checkText(mf.ID+".Setting."+setting.Name+".Description", setting.Description)
			for _, c := range setting.Choices {
				checkText(mf.ID+".Setting."+setting.Name+".Choice", c)
			}
		}
		for _, page := range mf.Pages {
			checkText(mf.ID+".Page."+page.Name+".Title", page.Title)
			checkText(mf.ID+".Page."+page.Name+".Description", page.Description)
		}
		if mf.Actions != nil {
			for _, act := range mf.Actions.All() {
				checkText(mf.ID+".Action."+act.Schema+".Title", act.Title)
				checkText(mf.ID+".Action."+act.Schema+".Description", act.Description)
			}
		}
	}

	if len(unclassifiedReads) > 0 {
		slices.Sort(unclassifiedReads)
		t.Fatalf("named reads missing from transport.ReadTranslationPolicies: %v", unclassifiedReads)
	}
	if len(untranslated) > 0 {
		slices.Sort(untranslated)
		t.Fatalf("untranslated zh-CN builtin manifest texts (%d):\n%s", len(untranslated), strings.Join(untranslated, "\n"))
	}
}

func TestI18nGatePackagesPermissionsAndWordsEndToEnd(t *testing.T) {
	adminSeat := protocol.Seat{Subjects: []string{"admin-e2e"}, Member: platform.Member{ID: "admin-e2e", Language: "zh-CN", Roles: map[string]string{"platform": "admin", "build": "builder"}}}
	tn := newInvariantTenantWithApps(
		t,
		"i18n-packages",
		[]platform.Service{
			work.New("i18n-packages", actions.NewAuthority),
			enterprisemodel.New("i18n-packages", actions.NewAuthority, platform.OrgSeed{}),
		},
		adminSeat,
	)
	adminMember, _ := tn.Member("admin-e2e")

	installFixture := func(rel string) platform.PackageDescriptor {
		t.Helper()
		raw, err := os.ReadFile(fixturePath(t, "frontend-fixtures/"+rel))
		if err != nil {
			t.Fatalf("read fixture %s: %v", rel, err)
		}
		preview, err := tn.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: raw})
		if err != nil {
			t.Fatalf("inspect %s: %v", rel, err)
		}
		importSub := tn.submission(packages.SchemaPackageImport, protocol.PackageType, preview.Descriptor.ID, map[string]any{
			"id": preview.Descriptor.ID, "digest": preview.Digest,
		})
		importSub.Authority, importSub.PrincipalId = controlowner.PlatformApp, adminMember.ID
		if _, refusal := tn.Submit(adminMember, importSub, invariantClock); refusal != nil {
			t.Fatalf("import %s: %v", rel, refusal)
		}
		installSub := tn.submission(packages.SchemaPackageInstall, protocol.PackageType, preview.Descriptor.ID, map[string]any{
			"id": preview.Descriptor.ID, "version": preview.Descriptor.Version,
		})
		installSub.Authority, installSub.PrincipalId = controlowner.PlatformApp, adminMember.ID
		if _, refusal := tn.Submit(adminMember, installSub, invariantClock); refusal != nil {
			t.Fatalf("install %s: %v", rel, refusal)
		}
		return preview.Descriptor
	}

	installFixture("geo-0.1.2.json")
	installFixture("party-0.2.2.json")
	installFixture("call-0.1.0.json")
	installFixture("phone-0.1.1.json")

	// 1. Verify /v1/packages translates all 4 package titles & descriptions in zh-CN
	rawPkgs, refusal := tn.read(adminMember, "packages")
	if refusal != nil {
		t.Fatalf("read packages: %v", refusal)
	}
	pkgViews := tn.i18n.ForRead("packages", rawPkgs, "zh-CN").([]protocol.PackageView)
	wantPkgTitles := map[string]string{"geo": "地点", "party": "往来单位", "call": "语音通话", "phone": "电话"}
	for id, wantTitle := range wantPkgTitles {
		idx := slices.IndexFunc(pkgViews, func(v protocol.PackageView) bool { return v.Descriptor.ID == id })
		if idx < 0 {
			t.Fatalf("package %s missing from PackageViews", id)
		}
		got := pkgViews[idx].Descriptor
		if got.Title != wantTitle {
			t.Errorf("package %s zh-CN title = %q, want %q", id, got.Title, wantTitle)
		}
		if !hasCJK(got.Description) {
			t.Errorf("package %s zh-CN description stayed English: %q", id, got.Description)
		}
	}

	// 2. Verify /v1/permissions translates phone roles & generated/custom actions in zh-CN
	rawPerms, refusal := tn.read(adminMember, "permissions")
	if refusal != nil {
		t.Fatalf("read permissions: %v", refusal)
	}
	translatedPerms := tn.i18n.ForRead("permissions", rawPerms, "zh-CN")
	encoded, _ := json.Marshal(translatedPerms)
	var perms []security.AppPermissions
	_ = json.Unmarshal(encoded, &perms)
	phoneIdx := slices.IndexFunc(perms, func(p security.AppPermissions) bool { return p.App == "phone" })
	if phoneIdx < 0 {
		t.Fatalf("phone missing from permissions")
	}
	phonePerms := perms[phoneIdx]
	if phonePerms.Title != "电话" {
		t.Errorf("phone permissions title = %q, want 电话", phonePerms.Title)
	}
	for _, r := range phonePerms.Roles {
		if !hasCJK(r.Title) {
			t.Errorf("phone role %s title stayed English: %q", r.Role, r.Title)
		}
		for _, act := range r.Actions {
			if !hasCJK(act.Title) {
				t.Errorf("phone role %s action %s title stayed English: %q", r.Role, act.ID, act.Title)
			}
		}
	}

	// 3. Verify /v1/words translates project area ("Places" -> "地点") and Origin ("county" -> "app")
	words := tn.Words("zh-CN")
	if slices.ContainsFunc(words.Words, func(w transport.Word) bool { return w.Area == "Places" }) {
		t.Errorf("expected geo words area to translate Places -> 地点, but found English area Places")
	}
	countyIdx := slices.IndexFunc(words.Words, func(w transport.Word) bool { return w.Text == "county" })
	if countyIdx < 0 {
		t.Fatalf("expected county word in geo vocabulary")
	}
	if gotOrigin := words.Words[countyIdx].Says["zh-CN"].Origin; gotOrigin != "app" {
		t.Errorf("expected county zh-CN origin = app, got %q", gotOrigin)
	}
}
