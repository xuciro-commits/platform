package platformserver

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestReferenceArchivePolicyFrozenWritesAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		tn, e := NewTenant("archive-links", NewConsole("archive-links", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"one"}, Member: platform.Member{ID: "one", Roles: map[string]string{build.ID: build.User}}}, Seat{Subjects: []string{"two"}, Member: platform.Member{ID: "two", Roles: map[string]string{build.ID: build.User}}}), build.New("archive-links"))
		if e != nil {
			t.Fatal(e)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	one, _ := tn.Member("one")
	two, _ := tn.Member("two")
	at := time.Now().UTC()
	var keys atomic.Int64
	var entries []Entry
	failAppend := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if failAppend {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(m platform.Member, typ, id, verb string, payload any) *kernel.Error {
		_, e := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(keys.Add(1)), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		return e
	}
	must := func(m platform.Member, typ, id, verb string, payload any) {
		t.Helper()
		if e := submit(m, typ, id, verb, payload); e != nil {
			t.Fatal(typ, verb, e.Message)
		}
	}
	for _, name := range []string{"parent", "child"} {
		fields := []build.Field{{Name: "note", Title: "Note", Type: "text"}}
		read := "all"
		if name == "child" {
			fields = append(fields, build.Field{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.parent"})
			read = "own"
		}
		must(builder, build.ObjectType, name, "create", map[string]any{"name": name, "title": name, "fields": fields, "access": []build.Access{{Role: build.User, Read: read, Create: true, Edit: true, Archive: true}}})
		must(builder, build.ObjectType, name, "publish", struct{}{})
	}
	for _, id := range []string{"P", "Q", "BAD", "J", "R"} {
		must(two, "build.parent", id, "create", map[string]any{"note": id})
	}
	must(two, "build.parent", "BAD", "archive", struct{}{})
	must(one, "build.child", "BAD-CHILD", "create", map[string]any{"note": "bad", "parent": "BAD"})
	must(one, "build.child", "PRIVATE-C1", "create", map[string]any{"note": "private blocker", "parent": "P"})
	must(one, "build.child", "J-CHILD", "create", map[string]any{"note": "append check", "parent": "J"})
	must(builder, build.LinkTypeType, "L", "create", map[string]any{"name": "children", "title": "Protected children", "description": "Original owner archive protection", "parent": "build.parent", "child": "build.child", "via": "parent", "forward": "children", "reverse": "parent", "deletePolicy": "restrict-active"})
	preview, e := tn.PreviewRelease(builder, platform.AssetLinkType, "L")
	if e == nil && preview.Diagnostic == "" {
		t.Fatal("preview accepted active reference to archived parent")
	}
	must(one, "build.child", "BAD-CHILD", "edit", map[string]any{"parent": ""})
	failAppend = true
	if e := submit(builder, build.LinkTypeType, "L", "publish", struct{}{}); e == nil {
		t.Fatal("accepted failed append")
	}
	failAppend = false
	must(two, "build.parent", "J", "archive", struct{}{})
	must(one, "build.child", "J-CHILD", "archive", struct{}{}) // no protection installed by failed append
	preview, e = tn.PreviewRelease(builder, platform.AssetLinkType, "L")
	if e != nil || preview.Diagnostic != "" {
		t.Fatal(preview, e)
	}
	saved, e := tn.SaveReleaseCandidate(builder, platform.AssetLinkType, "L", preview.CandidateID, "save", at)
	if e != nil {
		t.Fatal(e)
	}
	must(builder, build.LinkTypeType, "L", "edit", map[string]any{"deletePolicy": "owner", "title": "Later weak draft"})
	if _, e := tn.ActivateRelease(builder, saved, "activate", at); e != nil {
		t.Fatal(e)
	}
	conflict := func(m platform.Member, typ, id, verb string, payload any) {
		t.Helper()
		e := submit(m, typ, id, verb, payload)
		if e == nil || e.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || e.Message != linkArchiveFailure {
			t.Fatal("archive constraint was bypassed or exposed data", e)
		}
	}
	if _, e := tn.RecordOf(two, "build.child", "PRIVATE-C1", at); e == nil {
		t.Fatal("blocker was not private")
	}
	conflict(two, "build.parent", "P", "archive", struct{}{})
	must(one, "build.child", "PRIVATE-C1", "edit", map[string]any{"parent": "Q"})
	must(two, "build.parent", "P", "archive", struct{}{})
	conflict(one, "build.child", "NEW", "create", map[string]any{"note": "blocked", "parent": "P"})
	conflict(one, "build.child", "PRIVATE-C1", "edit", map[string]any{"parent": "P"})
	must(one, "build.child", "PRIVATE-C1", "archive", struct{}{})
	must(two, "build.parent", "Q", "archive", struct{}{})
	// Native restoration and final accepted images obey the same record owner.
	draft := tn.records.forkRecords()
	et := draft.types["build.child"]
	v := copyOf(et.info.Go, et.rows["PRIVATE-C1"].value.Interface())
	recordOf(v).Archived = false
	if e := draft.check(platform.Caller{App: build.ID}, v.Interface()); e == nil || e.Message != linkArchiveFailure {
		t.Fatal("restoration bypassed protection", e)
	}
	et.rows["PRIVATE-C1"].value = v
	if tn.records.promoteRecords(draft) == nil {
		t.Fatal("promoted incompatible accepted image")
	}
	if e := submit(builder, build.LinkTypeType, "L", "publish", struct{}{}); e == nil {
		t.Fatal("weak draft relaxed retained protection")
	}
	must(builder, build.LinkTypeType, "WEAK", "create", map[string]any{"name": "weakview", "title": "Weak view", "description": "Same reference", "parent": "build.parent", "child": "build.child", "via": "parent", "forward": "weakchildren", "reverse": "parent"})
	must(builder, build.LinkTypeType, "WEAK", "publish", struct{}{})
	conflict(one, "build.child", "NEW2", "create", map[string]any{"note": "blocked", "parent": "P"})
	// Parent archive and child creation compete on the original locks: one wins.
	start := make(chan struct{})
	answers := make(chan *kernel.Error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; answers <- submit(two, "build.parent", "R", "archive", struct{}{}) }()
	go func() {
		defer wg.Done()
		<-start
		answers <- submit(one, "build.child", "R-CHILD", "create", map[string]any{"note": "r", "parent": "R"})
	}()
	close(start)
	wg.Wait()
	close(answers)
	wins := 0
	for e := range answers {
		if e == nil {
			wins++
		} else if e.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatal("race created an invalid archive state", wins)
	}
	CheckReplay(t, tn, entries, compose)
	image, _, e := tn.Snapshot(func() int64 { return 0 })
	if e != nil {
		t.Fatal(e)
	}
	restored := compose()
	if e := restored.Restore(image); e != nil {
		t.Fatal(e)
	}
	tn = restored
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	conflict(one, "build.child", "AFTER", "create", map[string]any{"note": "after", "parent": "P"})
	for _, d := range tn.Definitions(builder) {
		if d.Ref.Kind == platform.AssetLinkType && d.Ref.Name == "children" && (d.ContractVersion != 3 || d.LinkType.DeletePolicy != "restrict-active" || d.LinkType.Title != "Protected children") {
			t.Fatal("frozen policy changed", d)
		}
	}
}

func TestArchiveProtectionUsesProspectiveSelfState(t *testing.T) {
	type node struct {
		platform.Record
		Parent string
	}
	makeValue := func(id, parent string, archived bool) reflect.Value {
		return copyOf(reflect.TypeOf(node{}), node{Record: platform.Record{ID: id, Archived: archived}, Parent: parent})
	}
	et := &entityType{info: platform.EntityInfo{App: "same", Type: "same.node", Go: reflect.TypeOf(node{}), Fields: []platform.FieldInfo{{Name: "parent", Type: "reference", Ref: "same.node", Index: []int{1}}}}, rows: map[string]*row{"A": {value: makeValue("A", "A", false)}, "B": {value: makeValue("B", "A", false)}}}
	l := platform.LinkType{Parent: platform.AssetRef{App: "same", Kind: platform.AssetObject, Name: "same.node"}, Child: platform.AssetRef{App: "same", Kind: platform.AssetObject, Name: "same.node"}, Via: "parent", DeletePolicy: "restrict-active"}
	s := newRecordStore()
	s.types["same.node"] = et
	s.archiveLinks = map[string]platform.LinkType{"self": l}
	if s.checkArchiveWriteLocked(et, makeValue("A", "A", true)) == nil {
		t.Fatal("ignored another active child")
	}
	et.rows["B"].value = makeValue("B", "A", true)
	if e := s.checkArchiveWriteLocked(et, makeValue("A", "A", true)); e != nil {
		t.Fatal("self prevented its own archive", e)
	}
	et.rows["A"].value = makeValue("A", "A", true)
	if e := s.validateArchiveLinkLocked(l); e != nil {
		t.Fatal(e)
	}
	if s.checkArchiveWriteLocked(et, makeValue("B", "A", false)) == nil {
		t.Fatal("restored child of archived parent")
	}
	if e := s.checkArchiveWriteLocked(et, makeValue("A", "A", false)); e != nil {
		t.Fatal("prospective restored self was not active", e)
	}
}
