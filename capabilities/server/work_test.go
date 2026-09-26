package platformserver

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// ADR-0019 D4: a member's saved views are their own data; nobody else reads or changes them.
func TestSavedViews(t *testing.T) {
	var journal []Entry
	build := func() *Tenant {
		tn, err := NewTenant("t-1", NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{}}},
			Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{}}}), NewWork("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, schema, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: WorkApp, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: ViewType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	views := func(who string) string {
		out, _ := tn.Read(member(who), "views")
		var titles []string
		for _, v := range out.([]SavedView) {
			titles = append(titles, v.Title)
		}
		return fmt.Sprint(titles)
	}
	state := `{"view":"pivot","group":"stage","measure":"sum:amount"}`
	for _, c := range []struct{ got, want string }{
		{do("ana", SchemaViewSave, "V1", map[string]string{"title": "Pipeline", "entity": "crm.opportunity", "state": state}), "ok"},
		{do("bo", SchemaViewSave, "V2", map[string]string{"title": "Mine", "entity": "hcm.leave"}), "ok"},
		{views("ana") + views("bo"), "[Pipeline][Mine]"},
		{do("bo", SchemaViewSave, "V1", map[string]string{"title": "Taken", "entity": "crm.opportunity"}), "ERROR_CODE_POLICY_DENIED"},
		{do("bo", SchemaViewRemove, "V1", struct{}{}), "ERROR_CODE_POLICY_DENIED"},
		{do("ana", SchemaViewSave, "V1", map[string]string{"title": "Pipeline by stage", "entity": "crm.opportunity", "state": state}), "ok"},
		{do("ana", SchemaViewSave, "V1", map[string]string{"title": "Other type", "entity": "hcm.leave"}), "ERROR_CODE_INVALID_ARGUMENT"},
		{do("ana", SchemaViewSave, "V3", map[string]string{"entity": "hcm.leave"}), "ERROR_CODE_INVALID_ARGUMENT"}, // a view has a name
		{views("ana"), "[Pipeline by stage]"},
		{do("ana", SchemaViewRemove, "V1", struct{}{}), "ok"},
		{views("ana"), "[]"},
	} {
		if c.got != c.want {
			t.Fatalf("got %s, want %s", c.got, c.want)
		}
	}
	CheckReplay(t, tn, journal, build)
}
