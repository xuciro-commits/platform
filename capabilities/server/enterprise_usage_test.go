package platformserver

import (
	"encoding/json"
	"platformserver/internal/records"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/core"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

func TestEnterpriseUsageCoreSiteRetainsOldReferencesAndHistory(t *testing.T) {
	const tenant = "enterprise-core-usage"
	compose := func() *Tenant {
		return composeTenant(t, tenant, []Seat{seatOf("admin", "enterprise:admin", "core:steward")}, enterprise.New(tenant, platform.OrgSeed{}), core.New(tenant))
	}
	tn := compose()
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"place", "closed-other"} {
		decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, id, map[string]any{"name": id, "stereotype": enterprise.Location, "from": "2026-01-01"}, at)
	}
	decide(t, tn, "admin", core.ID, core.SiteType+".create", core.SiteType, "SITE", map[string]any{"name": "Site", "code": "SITE", "kind": "plant", "place": "place", "active": true}, at)
	close := map[string]any{"until": "2026-10-20", "reason": "Site retired"}
	if got := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaElementClose, enterprise.ElementType, "place", close, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
		t.Fatalf("active site did not block closure: %s", got)
	}
	decide(t, tn, "admin", core.ID, core.SiteType+".edit", core.SiteType, "SITE", map[string]any{"active": false}, at)
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementClose, enterprise.ElementType, "place", close, at)
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementClose, enterprise.ElementType, "closed-other", close, at)
	decide(t, tn, "admin", core.ID, core.SiteType+".edit", core.SiteType, "SITE", map[string]any{"name": "Renamed retired site", "place": "place"}, at)
	if got := refuse(t, tn, "admin", core.ID, core.SiteType+".edit", core.SiteType, "SITE", map[string]any{"place": "closed-other"}, at); !strings.HasPrefix(got, "ERROR_CODE_INVALID_REFERENCE") && !strings.HasPrefix(got, "ERROR_CODE_INVALID_ARGUMENT") {
		t.Fatalf("new inactive location accepted: %s", got)
	}
	view, err := tn.RecordOf(memberOf(t, tn, "admin"), core.SiteType, "SITE", at)
	if err != nil {
		t.Fatal(err)
	}
	if record := view.Record.(core.Site); record.Place != "place" || record.Name != "Renamed retired site" {
		t.Fatalf("old reference or edited name lost: %+v", record)
	}
	if got := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaElementDelete, enterprise.ElementType, "place", map[string]any{"reason": "Remove unused"}, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
		t.Fatalf("inactive site history did not block deletion: %s", got)
	}
	decide(t, tn, "admin", core.ID, core.SiteType+".edit", core.SiteType, "SITE", map[string]any{"place": ""}, at)
	if got := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaElementDelete, enterprise.ElementType, "place", map[string]any{"reason": "Remove after clearing"}, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
		t.Fatalf("cleared reference erased historical protection: %s", got)
	}
	CheckReplay(t, tn, entries, compose)
}

type privateEnterpriseUse struct {
	platform.Record
	Name   string `json:"name"`
	Place  string `json:"place,omitempty" ref:"enterprise.element" stereo:"ActualLocation" read:"writer"`
	Active bool   `json:"active"`
}
type privateEnterpriseApp struct{ ledger *platform.Ledger }

func privateEnterpriseEntities() []platform.Entity {
	active := platform.ExprReference("boolean", "active")
	return []platform.Entity{{Type: "private-use.item", Title: "Private item", Model: privateEnterpriseUse{}, EnterpriseUse: &active, Standard: platform.Standard{Create: true, Edit: true, Roles: []string{"writer"}}}}
}
func newPrivateEnterpriseApp(tenant string) *privateEnterpriseApp {
	var actions []platform.Action
	for _, entity := range privateEnterpriseEntities() {
		actions = append(actions, platform.EntityActions(entity)...)
	}
	return &privateEnterpriseApp{ledger: platform.NewLedger(tenant, "private-use", platform.NewCatalog(actions...), "private-use.item")}
}
func (a *privateEnterpriseApp) Manifest() platform.Manifest {
	return platform.Manifest{ID: "private-use", Version: "1", Actions: a.ledger.Catalog, Entities: privateEnterpriseEntities()}
}
func (a *privateEnterpriseApp) AcceptedLedger() *platform.Ledger { return a.ledger }
func (a *privateEnterpriseApp) Declarations() []*pb.AuthorityDeclaration {
	return a.ledger.Declarations()
}
func (a *privateEnterpriseApp) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }
func (a *privateEnterpriseApp) Restore(raw json.RawMessage) error  { return a.ledger.Restore(raw) }
func (a *privateEnterpriseApp) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (a *privateEnterpriseApp) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if record, err, ok := a.ledger.Generated(c, s, at, nil, privateEnterpriseEntities()...); ok {
		return record, err
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func TestEnterpriseUsageHiddenOwnerFieldsStillBlockLifecycle(t *testing.T) {
	const tenant = "hidden-enterprise-usage"
	tn := composeTenant(t, tenant, []Seat{seatOf("owner", "enterprise:admin", "private-use:writer"), seatOf("modeler", "enterprise:admin")}, enterprise.New(tenant, platform.OrgSeed{}), newPrivateEnterpriseApp(tenant))
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	decide(t, tn, "owner", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, "secret-place", map[string]any{"name": "Place", "stereotype": enterprise.Location, "from": "2026-01-01"}, at)
	decide(t, tn, "owner", "private-use", "private-use.item.create", "private-use.item", "SECRET", map[string]any{"name": "Confidential site", "place": "secret-place", "active": true}, at)
	usage, err := tn.EnterpriseUsage(memberOf(t, tn, "modeler"), "secret-place", "2026-10-20", at)
	if err != nil {
		t.Fatal(err)
	}
	if usage.CanClose || usage.CanDelete || !usage.HasRestrictedUsage || len(usage.Items) != 0 {
		t.Fatalf("hidden use either leaked or failed to block: %+v", usage)
	}
	for _, schema := range []string{enterprise.SchemaElementClose, enterprise.SchemaElementDelete} {
		if got := refuse(t, tn, "modeler", enterprise.ID, schema, enterprise.ElementType, "secret-place", map[string]any{"until": "2026-10-20", "reason": "Change place"}, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
			t.Fatalf("hidden use did not block %s: %s", schema, got)
		}
	}
	decide(t, tn, "owner", "private-use", "private-use.item.edit", "private-use.item", "SECRET", map[string]any{"place": "", "active": false}, at)
	if got := refuse(t, tn, "modeler", enterprise.ID, enterprise.SchemaElementDelete, enterprise.ElementType, "secret-place", map[string]any{"reason": "Remove after clearing"}, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
		t.Fatalf("hidden before-value was not retained: %s", got)
	}
}

func TestEnterpriseUsageGrantsAndRepresentativeRootBlockLifecycle(t *testing.T) {
	const tenant = "enterprise-directory-usage"
	tn := composeTenant(t, tenant, []Seat{seatOf("admin", "platform:admin", "enterprise:admin", "core:steward"), seatOf("member")}, enterprise.New(tenant, platform.OrgSeed{}), core.New(tenant))
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"grant-unit", "root", "team-unit"} {
		decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, id, map[string]any{"name": id, "stereotype": enterprise.Organization, "from": "2026-01-01"}, at)
	}
	decide(t, tn, "admin", PlatformApp, SchemaGrant, MemberType, "member", map[string]any{"app": core.ID, "role": core.Steward, "unit": "grant-unit", "structure": "management", "from": "2026-10-01", "until": "2026-12-01"}, at)
	decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, PlatformApp+"/enterpriseRoot", map[string]any{"value": "root"}, at)
	for _, id := range []string{"grant-unit", "root"} {
		for _, schema := range []string{enterprise.SchemaElementClose, enterprise.SchemaElementDelete} {
			if got := refuse(t, tn, "admin", enterprise.ID, schema, enterprise.ElementType, id, map[string]any{"until": "2026-10-20", "reason": "Retire"}, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
				t.Fatalf("%s did not block %s: %s", id, schema, got)
			}
		}
	}
	decide(t, tn, "admin", PlatformApp, SchemaRevoke, MemberType, "member", map[string]any{"app": core.ID, "role": core.Steward, "unit": "grant-unit"}, at)
	usage, err := tn.EnterpriseUsage(memberOf(t, tn, "admin"), "grant-unit", "2026-10-20", at)
	if err != nil || usage.CanDelete || !usage.CanClose {
		t.Fatalf("revoked grant must protect history without keeping it current: %+v %v", usage, err)
	}
	decide(t, tn, "admin", enterprise.ID, enterprise.SchemaElementClose, enterprise.ElementType, "grant-unit", map[string]any{"until": "2026-10-20", "reason": "Grant ended"}, at)
	if got := refuse(t, tn, "admin", enterprise.ID, enterprise.SchemaElementDelete, enterprise.ElementType, "grant-unit", map[string]any{"reason": "Old grant removed"}, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
		t.Fatalf("revoked grant erased historical use: %s", got)
	}
	decide(t, tn, "admin", PlatformApp, SchemaTeamSave, TeamType, "team", map[string]any{"name": "Team", "members": []string{}, "grants": []platform.Grant{{App: core.ID, Role: core.Steward, Unit: "team-unit", Structure: "management"}}}, at)
	usage, err = tn.EnterpriseUsage(memberOf(t, tn, "admin"), "team-unit", "2026-10-20", at)
	if err != nil || usage.CanClose {
		t.Fatalf("stored team grant did not prevent closure: %+v %v", usage, err)
	}
	decide(t, tn, "admin", PlatformApp, SchemaTeamRemove, TeamType, "team", map[string]any{}, at)
	usage, err = tn.EnterpriseUsage(memberOf(t, tn, "admin"), "team-unit", "2026-10-20", at)
	if err != nil || usage.CanDelete || !usage.CanClose {
		t.Fatalf("removed team grant lost history or kept current use: %+v %v", usage, err)
	}
}

func TestEnterpriseUsageUnchangedRetiredLineReferences(t *testing.T) {
	type line struct {
		Unit string `json:"unit" ref:"enterprise.element" stereo:"ActualOrganization"`
		Note string `json:"note"`
	}
	type document struct {
		platform.Record
		Lines []line `json:"lines"`
	}
	info, err := platform.Describe("lines", platform.Entity{Type: "lines.document", Model: document{}}, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	previous := reflect.ValueOf(document{Lines: []line{{Unit: "closed", Note: "old"}}})
	value := reflect.ValueOf(document{Lines: []line{{Unit: "closed", Note: "updated"}}})
	// The empty caller resolves no active element: only retained references pass.
	if refusal := records.CheckEnterpriseReferences((platform.Caller{}).Enterprise(), false, info.Fields, value, previous, time.Now()); refusal != nil {
		t.Fatal(refusal)
	}
	value = reflect.ValueOf(document{Lines: []line{{Unit: "closed"}, {Unit: "closed"}}})
	if refusal := records.CheckEnterpriseReferences((platform.Caller{}).Enterprise(), false, info.Fields, value, previous, time.Now()); refusal == nil {
		t.Fatal("duplicating an old closed reference created another use")
	}
	field := info.Fields[0].Fields[0]
	field.Read = []string{"private"}
	if paths := records.MatchingRefs([]platform.FieldInfo{field}, "", reflect.ValueOf(line{Unit: "closed"}), "closed", nil); len(paths) != 0 {
		t.Fatal("nil roles exposed a private reference")
	}
}
