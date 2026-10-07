package platformserver

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestLinkCardinalityFrozenWritesAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("cardinality", NewConsole("cardinality", Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"one"}, Member: platform.Member{ID: "one", Roles: map[string]string{build.ID: build.User}}}, Seat{Subjects: []string{"two"}, Member: platform.Member{ID: "two", Roles: map[string]string{build.ID: build.User}}}), build.New("cardinality"))
		if err != nil {
			t.Fatal(err)
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
			t.Fatalf("%s %s: %s", typ, verb, e.Message)
		}
	}
	for _, name := range []string{"parent", "child"} {
		fields := []build.Field{{Name: "note", Title: "Note", Type: "text"}}
		if name == "child" {
			fields = append(fields, build.Field{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.parent"})
		}
		read := "all"
		if name == "child" {
			read = "own"
		}
		must(builder, build.ObjectType, name, "create", map[string]any{"name": name, "title": name, "fields": fields, "access": []build.Access{{Role: build.User, Read: read, Create: true, Edit: true, Archive: true}}})
		must(builder, build.ObjectType, name, "publish", struct{}{})
	}
	for _, id := range []string{"P", "Q", "R"} {
		must(builder, "build.parent", id, "create", map[string]any{"note": id})
	}
	must(one, "build.child", "PRIVATE-C1", "create", map[string]any{"note": "private competitor", "parent": "P"})
	must(builder, build.LinkTypeType, "L", "create", map[string]any{"name": "children", "title": "Children", "description": "One bounded child per parent", "parent": "build.parent", "child": "build.child", "via": "parent", "forward": "children", "reverse": "parent", "cardinality": "one-to-one"})
	preview, err := tn.PreviewRelease(builder, platform.AssetLinkType, "L")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	id, err := tn.SaveReleaseCandidate(builder, platform.AssetLinkType, "L", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := platform.ReadCandidate(id, tn.releases.candidates[id])
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.Kind == platform.AssetLinkType && asset.ContractVersion != 2 {
			t.Fatal("unique relation used legacy contract")
		}
	}
	failAppend = true
	if e := submit(builder, build.LinkTypeType, "L", "publish", struct{}{}); e == nil {
		t.Fatal("publication accepted failed append")
	}
	failAppend = false
	// Frozen data gets checked again at activation, before any live installation.
	must(two, "build.child", "C2", "create", map[string]any{"note": "second", "parent": "P"})
	if _, e := tn.ActivateRelease(builder, id, "blocked", at); e == nil {
		t.Fatal("activation accepted pre-existing duplicates")
	}
	for _, d := range tn.Definitions(builder) {
		if d.Ref.Kind == platform.AssetLinkType && d.Ref.Name == "children" {
			t.Fatal("failed activation installed relation")
		}
	}
	must(two, "build.child", "C2", "edit", map[string]any{"parent": ""})
	must(builder, build.LinkTypeType, "L", "edit", map[string]any{"cardinality": "one-to-many", "title": "Later draft"})
	if _, e := tn.ActivateRelease(builder, id, "activate", at); e != nil {
		t.Fatal(e)
	}
	// A second weak traversal declaration over the same field must not relax it.
	must(builder, build.LinkTypeType, "WEAK", "create", map[string]any{"name": "weakchildren", "title": "Weak view", "description": "Same original reference", "parent": "build.parent", "child": "build.child", "via": "parent", "forward": "weakchildren", "reverse": "parent"})
	must(builder, build.LinkTypeType, "WEAK", "publish", struct{}{})
	out, traversalErr := tn.TraverseLink(builder, platform.AssetBinding{Ref: platform.AssetRef{App: build.ID, Kind: platform.AssetLinkType, Name: "children"}, SourceVersion: "1.link-1"}, "forward", "P", platform.Query{}, at)
	if traversalErr != nil || out.Total != 1 {
		t.Fatal("original unique traversal failed", traversalErr, out.Total)
	}
	conflict := func(m platform.Member, record, verb string, payload any) {
		t.Helper()
		e := submit(m, "build.child", record, verb, payload)
		if e == nil || e.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || e.Message != linkCardinalityFailure || strings.Contains(e.Message, "PRIVATE-C1") {
			t.Fatalf("conflict exposed data or accepted write: %v", e)
		}
	}
	if _, e := tn.RecordOf(two, "build.child", "PRIVATE-C1", at); e == nil {
		t.Fatal("competitor was not private")
	}
	conflict(two, "C2", "edit", map[string]any{"parent": "P"})
	must(one, "build.child", "PRIVATE-C1", "edit", map[string]any{"parent": "Q"})
	must(two, "build.child", "C2", "edit", map[string]any{"parent": "P"})
	conflict(one, "PRIVATE-C1", "edit", map[string]any{"parent": "P"})
	must(two, "build.child", "C2", "archive", struct{}{})
	conflict(one, "C3", "create", map[string]any{"note": "new", "parent": "P"})
	for _, id := range []string{"C3", "C4"} {
		must(one, "build.child", id, "create", map[string]any{"note": id, "parent": ""})
	}
	if e := submit(builder, build.LinkTypeType, "L", "publish", struct{}{}); e == nil {
		t.Fatal("later weak draft relaxed frozen uniqueness")
	}
	// The original tenant/record locks serialize two competing creations.
	start := make(chan struct{})
	results := make(chan *kernel.Error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{"C5", "C6"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- submit(two, "build.child", id, "create", map[string]any{"note": id, "parent": "R"})
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if e.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatal(e)
		}
	}
	if wins != 1 {
		t.Fatal("concurrent parent was not unique", wins)
	}
	// Direct promotion cannot bypass the same constraint with a forged row image.
	draft := tn.records.forkRecords()
	et := draft.types["build.child"]
	field, _ := et.info.Field("parent")
	et.rows["C4"].value.FieldByIndex(field.Index).SetString("P")
	if tn.records.promoteRecords(draft) == nil {
		t.Fatal("promoted incompatible accepted image")
	}
	CheckReplay(t, tn, entries, compose)
	image, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	tn = restored
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
	conflict(one, "C7", "create", map[string]any{"note": "after restore", "parent": "P"})
	for _, d := range tn.Definitions(builder) {
		if d.Ref.Kind == platform.AssetLinkType && d.Ref.Name == "children" && (d.ContractVersion != 2 || d.LinkType.Cardinality != "one-to-one" || d.LinkType.Title != "Children") {
			t.Fatal("frozen declaration changed", d)
		}
	}
}
