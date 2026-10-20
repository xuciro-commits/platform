package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"platformserver/apps/relations"
	"platformserver/platform"
)

func TestOwnedRelationMigrationPreservesHistoryAndOneActiveTruth(t *testing.T) {
	const tenant = "relation-migration"
	compose := func() *Tenant { return builderTenant(t, tenant, relations.New(tenant)) }
	tn := compose()
	for _, name := range []string{"parent", "child"} {
		typ := publishObject(t, tn, "builder", strings.ToUpper(name), map[string]any{"name": name, "title": name, "fields": []construction.Field{{Name: "name", Title: "Name", Type: "text"}}}, txnAt)
		decide(t, tn, "builder", construction.ID, typ+".create", typ, name, map[string]any{"name": name}, txnAt)
	}
	legacy := tn.app(relations.ID).(*relations.Relations)
	oldLink := platform.RelationLink{From: "build.parent/parent", To: "build.child/child", By: "historical-builder", At: txnAt}
	// A pre-migration owner snapshot is a historical fixture, not a new write
	// through the frozen ad-hoc entry point.
	raw, _ := legacy.Snapshot()
	var state map[string]json.RawMessage
	_ = json.Unmarshal(raw, &state)
	state["own"] = platform.Raw(map[string]any{"links": []platform.RelationLink{oldLink}, "notes": []platform.RelationNote{}})
	if err := legacy.Restore(platform.Raw(state)); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "builder", construction.ID, "build.linktype.create", construction.LinkTypeType, "LINK", map[string]any{"name": "children", "title": "Children", "description": "Owned historical children", "storage": "edge", "parent": "build.parent", "child": "build.child", "forward": "children", "reverse": "parent", "cardinality": "one-to-many"}, txnAt)
	decide(t, tn, "builder", construction.ID, "build.linktype.publish", construction.LinkTypeType, "LINK", nil, txnAt)
	before, _, snapshotErr := tn.Snapshot(func() int64 { return 0 })
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	legacyBefore, _ := legacy.Snapshot()
	binding := platform.AssetBinding{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetLinkType, Name: "children"}, SourceVersion: "1.link-1"}
	plan, err := tn.relationMigrationPlan(tn.records, tn.caller(memberOf(t, tn, "builder"), tn.app(construction.ID), false), binding, txnAt)
	if err != nil || plan.Partial || !reflect.DeepEqual(plan.Links, []platform.RelationLink{oldLink}) {
		t.Fatalf("migration preview: %+v %v", plan, err)
	}
	var entries []Entry
	fail := true
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if got := refuse(t, tn, "builder", construction.ID, "build.linktype.migrate", construction.LinkTypeType, "LINK", nil, txnAt); got == "ok" {
		t.Fatal("failed append accepted migration")
	}
	if tn.records.types[platform.OwnedRelationType(construction.ID)].rows.len() != 0 {
		t.Fatal("failed append left migration rows")
	}
	fail = false
	decide(t, tn, "builder", construction.ID, "build.linktype.migrate", construction.LinkTypeType, "LINK", nil, txnAt)
	legacyAfter, _ := legacy.Snapshot()
	if string(legacyBefore) != string(legacyAfter) {
		t.Fatal("migration rewrote historical relation state")
	}
	links, refusal := tn.Read(memberOf(t, tn, "builder"), "links")
	if refusal != nil || !reflect.DeepEqual(links, []platform.RelationLink{oldLink}) {
		t.Fatalf("migration left two active truths: %+v %v", links, refusal)
	}
	restored := compose()
	if err := restored.Restore(before); err != nil {
		t.Fatal(err)
	}
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if snapshot(tn) != snapshot(restored) {
		t.Fatal("migration recovery changed saved rows or source history")
	}
	if got := refuse(t, tn, "builder", relations.ID, relations.SchemaUnlink, relations.LinkType, "old-unlink", map[string]any{"from": oldLink.From, "to": oldLink.To}, txnAt); !strings.Contains(got, "declared relationship transaction") {
		t.Fatalf("ad-hoc owner write remained live: %s", got)
	}
}
