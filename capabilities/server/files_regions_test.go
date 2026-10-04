package platformserver

import (
	"bytes"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/files"
	"platformserver/platform"
	"reflect"
	"testing"
	"time"
)

func TestRasterRegionsPreserveFileIdentityAuthorRevisionAndRecovery(t *testing.T) {
	compose := func() *Tenant {
		tn, err := NewTenant("regions", NewConsole("regions", Seat{Subjects: []string{"author"}, Member: platform.Member{ID: "author", Roles: map[string]string{"shop": "clerk"}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{"shop": "clerk"}}}), files.New("regions"), newShop("regions"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	author, _ := tn.Member("author")
	reader, _ := tn.Member("reader")
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	var entries []Entry
	tn.Record = func(e Entry) {
		if e.Kind != "accepted-result" {
			entries = append(entries, e)
		}
	}
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	key := 0
	submit := func(m platform.Member, app, schema, typ, id string, rev uint32, payload any) *pb.ChangeRecord {
		t.Helper()
		key++
		r, err := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: app, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, ExpectedRevision: proto.Uint32(rev), Payload: platform.Raw(payload)}, now)
		if err != nil {
			t.Fatal(err.Message)
		}
		return r
	}
	submit(author, "shop", "shop.order.place", "shop.order", "O", 0, map[string]string{"item": "image"})
	up, _, err := tn.Upload(author, "drawing.png", "image/png", bytes.NewReader([]byte("bounded raster fixture")), now)
	if err != nil {
		t.Fatal(err)
	}
	submit(author, files.ID, files.SchemaAttach, files.FileType, "F", 0, map[string]any{"hash": up.Hash, "name": up.Name, "contentType": up.ContentType, "size": up.Size, "target": "shop.order/O"})
	view, issue := tn.RecordOf(author, files.FileType, "F", now)
	if issue != nil {
		t.Fatal(issue)
	}
	file := view.Record.(files.File)
	regions := []files.ImageRegion{{ID: "R1", Label: "Original <b>label</b>", X: .1, Y: .2, Width: .25, Height: .5}}
	payload := map[string]any{"hash": up.Hash, "regions": regions}
	prior := tn.AcceptResult
	tn.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("injected append failure") }
	_, failed := tn.Submit(author, &pb.Submission{TenantId: tn.ID, PrincipalId: author.ID, Authority: files.ID, IdempotencyKey: "failed-regions", Target: &pb.EntityRef{Type: files.FileType, Id: "F"}, Schema: &pb.SchemaRef{Name: files.SchemaAnnotate, Version: 1}, ExpectedRevision: proto.Uint32(file.Revision), Payload: platform.Raw(payload)}, now)
	if failed == nil {
		t.Fatal("append failure accepted regions")
	}
	unchanged, _ := tn.RecordOf(author, files.FileType, "F", now)
	if unchanged.Record.(files.File).Revision != file.Revision || len(unchanged.Record.(files.File).Regions) != 0 {
		t.Fatal("failed append leaked regions")
	}
	tn.AcceptResult = prior
	submit(author, files.ID, files.SchemaAnnotate, files.FileType, "F", file.Revision, payload)
	refused := func(m platform.Member, revision uint32, p any, code pb.ErrorCode) {
		t.Helper()
		key++
		_, err := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: files.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: files.FileType, Id: "F"}, Schema: &pb.SchemaRef{Name: files.SchemaAnnotate, Version: 1}, ExpectedRevision: proto.Uint32(revision), Payload: platform.Raw(p)}, now)
		if err == nil || err.Code != code {
			t.Fatal("annotation refusal", err)
		}
	}
	refused(author, file.Revision, payload, pb.ErrorCode_ERROR_CODE_CONFLICT)
	view, _ = tn.RecordOf(author, files.FileType, "F", now)
	revision := view.Record.(files.File).Revision
	refused(reader, revision, payload, pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
	refused(author, revision, map[string]any{"hash": "changed", "regions": regions}, pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	for _, bad := range []any{nil, []files.ImageRegion{{ID: "X", X: .9, Width: .2, Height: .1}}, append(regions, regions...), []map[string]any{{"id": "R", "x": 0, "y": 0, "width": .1, "height": .1, "execute": "script"}}} {
		refused(author, revision, map[string]any{"hash": up.Hash, "regions": bad}, pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	}
	check := func(current *Tenant) {
		t.Helper()
		for _, m := range []platform.Member{author, reader} {
			v, e := current.RecordOf(m, files.FileType, "F", now)
			if e != nil {
				t.Fatal(e)
			}
			f := v.Record.(files.File)
			if f.Hash != up.Hash || f.Target != "shop.order/O" || f.By != author.ID || !reflect.DeepEqual(f.Regions, regions) {
				t.Fatal("original file or regions changed", f)
			}
		}
	}
	check(tn)
	tn.SweepUploads(now.Add(48 * time.Hour))
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	check(restored)
	submit(author, files.ID, files.SchemaAnnotate, files.FileType, "F", revision, map[string]any{"hash": up.Hash, "regions": []files.ImageRegion{}})
	view, _ = tn.RecordOf(author, files.FileType, "F", now)
	if len(view.Record.(files.File).Regions) != 0 {
		t.Fatal("clear did not remove original regions")
	}
}
