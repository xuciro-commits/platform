package platformserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/files"
	"platformserver/apps/knowledge"
	"platformserver/platform"
)

// ADR-0028 11a: files uploaded to the store, attached to a record by a
// decision naming their hash, readable exactly when the record is; replay
// reads no bytes; what nobody attached is swept; text files are knowledge.
func TestFiles(t *testing.T) {
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	var journal []Entry
	build := func() *Tenant {
		dir := NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"shop": "clerk", "knowledge": knowledge.Editor}}},
			Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{}}})
		tn, err := NewTenant("t-1", dir, files.New("t-1"), knowledge.New("t-1"), newShop("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	ana, bo := member("ana"), member("bo")
	n := 0
	decide := func(m platform.Member, app, schema, typ, id string, payload any) string {
		n++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(m, &pb.Submission{TenantId: "t-1", PrincipalId: m.ID, Authority: app, IdempotencyKey: fmt.Sprint("f", n),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			return err.Code.String()
		}
		return "ok"
	}
	upload := func(name, text string) Upload {
		up, _, err := tn.Upload(ana, name, "", strings.NewReader(text), now)
		if err != nil {
			t.Fatal(err)
		}
		return up
	}
	attach := func(m platform.Member, id string, up Upload, target string) string {
		return decide(m, files.ID, files.SchemaAttach, files.FileType, id, map[string]any{"hash": up.Hash, "name": up.Name, "contentType": up.ContentType, "size": up.Size, "target": target})
	}
	if got := decide(ana, "shop", "shop.order.place", "shop.order", "O1", map[string]string{"item": "pen"}); got != "ok" {
		t.Fatal(got)
	}

	photo := upload("damage.txt", "the pen arrived bent")
	if got := attach(ana, "F1", photo, "shop.order/O1"); got != "ok" {
		t.Fatalf("attach: %s", got)
	}
	// Only what one may read can be attached to, and only uploaded bytes.
	if got := attach(bo, "F2", photo, "shop.order/O1"); got != "ERROR_CODE_NOT_FOUND" {
		t.Fatalf("bo attached to an order he may not read: %s", got)
	}
	if got := attach(ana, "F3", Upload{Hash: strings.Repeat("0", 64), Name: "x", Size: 1}, "shop.order/O1"); got != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("attached bytes never uploaded: %s", got)
	}
	// The order's page lists it; its readers download it, nobody else sees it.
	if v, _ := tn.RecordOf(ana, "shop.order", "O1", now); len(v.Files) != 1 {
		t.Fatalf("files of the order: %+v", v.Files)
	}
	get := func(m platform.Member) (int, string) {
		w := httptest.NewRecorder()
		tn.Download(w, m, "F1", now)
		return w.Code, w.Body.String()
	}
	if code, body := get(ana); code != 200 || body != "the pen arrived bent" {
		t.Fatalf("ana downloads: %d %q", code, body)
	}
	if code, _ := get(bo); code != 404 {
		t.Fatalf("bo downloads: %d", code)
	}
	if page, _ := tn.Records(bo, files.FileType, platform.Query{}, now); page.Total != 0 {
		t.Fatalf("bo lists %d files", page.Total)
	}

	// A text file attached to a knowledge document is found and cited.
	if got := decide(ana, knowledge.ID, knowledge.DocumentType+".create", knowledge.DocumentType, "D1", map[string]string{"title": "Returns", "text": "See the attached policy."}); got != "ok" {
		t.Fatal(got)
	}
	policy := upload("returns.md", "# Returns\n\nA bent pen is replaced within fourteen days.")
	if got := attach(ana, "F4", policy, knowledge.DocumentType+"/D1"); got != "ok" {
		t.Fatal(got)
	}
	found := tn.Knowledge(&ana, "", "bent pen replaced", 3, now)
	if len(found) == 0 || found[0].Document != files.FileType+"/F4" {
		t.Fatalf("knowledge found %+v", found)
	}

	// Detached, it stays in the history; bytes nobody attached are swept after a day.
	if got := decide(bo, files.ID, files.SchemaDetach, files.FileType, "F1", struct{}{}); got != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("bo detached ana's file: %s", got)
	}
	stray := upload("stray.txt", "nobody wants me")
	tn.SweepUploads(now.Add(25 * time.Hour))
	if tn.files().Exists(context.Background(), "t-1/"+stray.Hash) || !tn.files().Exists(context.Background(), "t-1/"+photo.Hash) {
		t.Fatal("the sweep removed the wrong bytes")
	}

	// A replay reads no bytes: a tenant with an empty store rebuilds the same records.
	CheckReplay(t, tn, journal, build)
}
