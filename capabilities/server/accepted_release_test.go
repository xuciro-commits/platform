package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestAcceptedReleaseCandidateCommitRetryAndRecovery(t *testing.T) {
	const tenantID = "release-candidate"
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
			Roles: map[string]string{build.ID: build.Builder}}}
		tenant, err := NewTenant(tenantID, NewConsole(tenantID, seat), build.New(tenantID))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	live := compose()
	member, _ := live.Member("dana")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	var entries []Entry
	committed := map[string]Entry{}
	fail := false
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		id := e.App + "/" + key
		if prior, ok := committed[id]; ok {
			priorHash, err := resultRequestHash(prior.Body)
			if err != nil || priorHash != hash {
				return nil, fmt.Errorf("idempotency conflict")
			}
			return prior.Body, nil
		}
		committed[id] = e
		entries = append(entries, e)
		return e.Body, nil
	}
	if _, err := live.Submit(member, &pb.Submission{TenantId: tenantID, PrincipalId: member.ID,
		Authority: build.ID, IdempotencyKey: "create", Target: &pb.EntityRef{Type: build.ObjectType, Id: "O1"},
		Schema:  &pb.SchemaRef{Name: build.ObjectType + ".create", Version: 1},
		Payload: []byte(`{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`)}, at); err != nil {
		t.Fatal(err)
	}
	preview, err := live.PreviewRelease(member, platform.AssetObject, "O1")
	if err != nil || preview.CandidateID == "" || preview.Diagnostic != "" {
		t.Fatalf("expected a valid saved draft candidate: %+v, %v", preview, err)
	}
	fail = true
	if _, err := live.SaveReleaseCandidate(member, platform.AssetObject, "O1", preview.CandidateID, "save-1", at); err == nil {
		t.Fatal("append failure should be returned")
	}
	if len(live.releaseCandidates) != 0 || len(entries) != 1 {
		t.Fatal("failed append exposed a candidate")
	}
	fail = false
	savedID, err := live.SaveReleaseCandidate(member, platform.AssetObject, "O1", preview.CandidateID, "save-1", at)
	if err != nil || savedID != preview.CandidateID || len(entries) != 2 {
		t.Fatalf("candidate was not committed once: %s, %v", savedID, err)
	}
	if _, err := platform.ReadCandidate(savedID, live.releaseCandidates[savedID]); err != nil {
		t.Fatalf("saved bytes are not the exact candidate: %v", err)
	}
	if _, err := live.SaveReleaseCandidate(member, platform.AssetObject, "O1", savedID, "save-1", at.Add(time.Hour)); err != nil || len(entries) != 2 {
		t.Fatalf("retry must return the first committed result: %v, entries=%d", err, len(entries))
	}
	other := member
	other.Roles = map[string]string{}
	if _, err := live.SaveReleaseCandidate(other, platform.AssetObject, "O1", savedID, "unauthorized", at); err == nil || len(entries) != 2 {
		t.Fatal("unprivileged member saved a candidate")
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered.releaseCandidates[savedID], live.releaseCandidates[savedID]) {
		t.Fatal("recovery did not use the committed candidate bytes")
	}
	raw, _, err := live.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	fromSnapshot := compose()
	if err := fromSnapshot.Restore(raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fromSnapshot.releaseCandidates[savedID], live.releaseCandidates[savedID]) {
		t.Fatal("snapshot lost the immutable candidate")
	}
	// A saved revision does not change the installed development definition or
	// activate itself; those are separate, still-pending release operations.
	if live.activeRelease != "" || recovered.activeRelease != "" {
		t.Fatal("saving a candidate silently activated it")
	}
	CheckReplay(t, live, entries, compose)
}

func TestSaveReleaseCandidateRouteChecksBuilderBeforeReadingDraft(t *testing.T) {
	const id = "release-route"
	seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
		Roles: map[string]string{build.ID: build.Builder}}}
	reader := Seat{Subjects: []string{"erin"}, Member: platform.Member{ID: "erin",
		Roles: map[string]string{build.ID: build.User}}}
	tenant, err := NewTenant(id, NewConsole(id, seat, reader), build.New(id))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tenant.Member("dana")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	if _, err := tenant.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID,
		Authority: build.ID, IdempotencyKey: "create", Target: &pb.EntityRef{Type: build.ObjectType, Id: "O1"},
		Schema:  &pb.SchemaRef{Name: build.ObjectType + ".create", Version: 1},
		Payload: []byte(`{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`)}, at); err != nil {
		t.Fatal(err)
	}
	preview, err := tenant.PreviewRelease(member, platform.AssetObject, "O1")
	if err != nil || preview.CandidateID == "" {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) { return entry.Body, nil }
	host := NewHost(Tokens(map[string]string{"builder": "dana", "reader": "erin"}), tenant)
	host.Now = func() time.Time { return at }
	handler := host.Handler()
	post := func(token, candidate string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(ReleaseSaveRequest{Kind: platform.AssetObject, ID: "O1", CandidateID: candidate, Key: "k1"})
		req := httptest.NewRequest(http.MethodPost, "/v1/releases/candidates", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := post("reader", preview.CandidateID); rec.Code != http.StatusForbidden || rec.Body.Len() != 0 {
		t.Fatalf("non-builder learned about a private candidate: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("builder", "sha256-v1:stale"); rec.Code != http.StatusConflict || len(tenant.releaseCandidates) != 0 {
		t.Fatalf("stale preview was saved: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post("builder", preview.CandidateID); rec.Code != http.StatusOK ||
		!bytes.Contains(rec.Body.Bytes(), []byte(preview.CandidateID)) {
		t.Fatalf("builder could not save reviewed bytes: %d %s", rec.Code, rec.Body.String())
	}
}

func TestJournalAcceptedReleaseCandidateCrashBeforeApplication(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("release-candidate-%d", time.Now().UnixNano())
	defer journal.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
			Roles: map[string]string{build.ID: build.Builder}}}
		tenant, err := NewTenant(id, NewConsole(id, seat), build.New(id))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live := compose()
	member, _ := live.Member("dana")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	live.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, entry, key, hash)
	}
	if _, err := live.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID,
		Authority: build.ID, IdempotencyKey: "create", Target: &pb.EntityRef{Type: build.ObjectType, Id: "O1"},
		Schema:  &pb.SchemaRef{Name: build.ObjectType + ".create", Version: 1},
		Payload: []byte(`{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`)}, at); err != nil {
		t.Fatal(err)
	}
	preview, err := live.PreviewRelease(member, platform.AssetObject, "O1")
	if err != nil || preview.CandidateID == "" {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	// PostgreSQL commits, but the caller loses the reply before applying the
	// result in memory. The next process must recover the exact saved bytes.
	live.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, entry, key, hash); err != nil {
			return nil, err
		}
		return nil, errors.New("connection lost after append")
	}
	if _, err := live.SaveReleaseCandidate(member, platform.AssetObject, "O1", preview.CandidateID, "save", at); err == nil ||
		len(live.releaseCandidates) != 0 {
		t.Fatal("lost acknowledgement exposed an unapplied candidate")
	}
	reopened, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 2 {
		t.Fatalf("expected two durable entries, got %d: %v", len(entries), err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.ReadCandidate(preview.CandidateID, recovered.releaseCandidates[preview.CandidateID]); err != nil {
		t.Fatal(err)
	}
	recovered.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		return reopened.AppendAccepted(ctx, id, entry, key, hash)
	}
	if _, err := recovered.SaveReleaseCandidate(member, platform.AssetObject, "O1", preview.CandidateID, "save", at.Add(time.Hour)); err != nil ||
		reopened.Position(id) != 2 {
		t.Fatalf("retry appended again or failed: %v", err)
	}
}

func TestAcceptedReleaseCandidateRejectsCorruption(t *testing.T) {
	candidate, err := platform.Candidate([]platform.AssetRef{{App: "sample", Kind: platform.AssetObject, Name: "sample.item"}},
		[]platform.ReleaseAsset{{Ref: platform.AssetRef{App: "sample", Kind: platform.AssetObject, Name: "sample.item"},
			ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"sample.item"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	saved := acceptedRelease{Version: 1, Kind: "release-result", Tenant: "t", App: build.ID,
		Member: "dana", Key: "release:k", At: time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC),
		CandidateID: candidate.ID, Bytes: candidate.Bytes}
	saved.RequestHash, _ = releaseRequestHash(saved.Tenant, saved.Member, saved.Key, saved.CandidateID, false)
	raw, err := encodeAcceptedRelease(saved)
	if err != nil {
		t.Fatal(err)
	}
	var altered acceptedRelease
	if err := json.Unmarshal(raw, &altered); err != nil {
		t.Fatal(err)
	}
	altered.Bytes = []byte(`{"format":1,"assets":[]}`)
	altered.Digest, _ = releaseDigest(altered)
	bad, _ := json.Marshal(altered)
	if _, err := decodeAcceptedRelease(bad); err == nil {
		t.Fatal("valid envelope digest must not excuse a changed candidate")
	}
	if _, err := decodeAcceptedRelease(append(raw, []byte(`{}`)...)); err == nil {
		t.Fatal("trailing release data was accepted")
	}
}

// The in-memory development host journals the saved candidate through Record;
// replaying that journal restores the same bytes without activating them.
func TestInMemoryReleaseCandidateSaveReplays(t *testing.T) {
	const tenantID = "release-memory"
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
			Roles: map[string]string{build.ID: build.Builder}}}
		tenant, err := NewTenant(tenantID, NewConsole(tenantID, seat), build.New(tenantID))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	live := compose()
	var entries []Entry
	live.Record = func(e Entry) { entries = append(entries, e) }
	member, _ := live.Member("dana")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	if _, err := live.Submit(member, &pb.Submission{TenantId: tenantID, PrincipalId: member.ID,
		Authority: build.ID, IdempotencyKey: "create", Target: &pb.EntityRef{Type: build.ObjectType, Id: "O1"},
		Schema:  &pb.SchemaRef{Name: build.ObjectType + ".create", Version: 1},
		Payload: []byte(`{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`)}, at); err != nil {
		t.Fatal(err)
	}
	preview, err := live.PreviewRelease(member, platform.AssetObject, "O1")
	if err != nil || preview.CandidateID == "" {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	id, err := live.SaveReleaseCandidate(member, platform.AssetObject, "O1", preview.CandidateID, "k", at)
	if err != nil || id != preview.CandidateID {
		t.Fatalf("in-memory save: %s, %v", id, err)
	}
	count := len(entries)
	if again, err := live.SaveReleaseCandidate(member, platform.AssetObject, "O1", id, "k", at); err != nil || again != id || len(entries) != count {
		t.Fatalf("in-memory retry recorded again: %v, %d/%d", err, len(entries), count)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recovered.releaseCandidates[id], live.releaseCandidates[id]) || recovered.activeRelease != "" {
		t.Fatal("replay lost the saved candidate or activated it")
	}
}

// Activation moves the single pointer only to a saved candidate that equals the
// running definitions; an unpublished draft's candidate is refused, the pointer
// survives replay, and a non-builder cannot move it (ADR-0039 D2).
func TestActivateReleaseMatchesRunningDefinitions(t *testing.T) {
	const tenantID = "release-activate"
	compose := func() *Tenant {
		seats := []Seat{{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
			Roles: map[string]string{build.ID: build.Builder}}},
			{Subjects: []string{"erin"}, Member: platform.Member{ID: "erin", Roles: map[string]string{build.ID: build.User}}}}
		tenant, err := NewTenant(tenantID, NewConsole(tenantID, seats...), build.New(tenantID))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	live := compose()
	var entries []Entry
	live.Record = func(e Entry) { entries = append(entries, e) }
	member, _ := live.Member("dana")
	user, _ := live.Member("erin")
	at := time.Date(2026, 9, 28, 18, 0, 0, 0, time.UTC)
	submit := func(key, schema string, payload string) {
		t.Helper()
		if _, err := live.Submit(member, &pb.Submission{TenantId: tenantID, PrincipalId: member.ID,
			Authority: build.ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: build.ObjectType, Id: "O1"},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at); err != nil {
			t.Fatal(err)
		}
	}
	submit("create", build.ObjectType+".create", `{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`)
	draft, _ := live.PreviewRelease(member, platform.AssetObject, "O1")
	if _, err := live.SaveReleaseCandidate(member, platform.AssetObject, "O1", draft.CandidateID, "s1", at); err != nil {
		t.Fatal(err)
	}
	if _, err := live.ActivateRelease(member, draft.CandidateID, "a1", at); err == nil || live.ActiveRelease() != "" {
		t.Fatal("a candidate operators do not run was activated")
	}
	submit("publish", build.SchemaPublish, `{}`)
	if _, err := live.ActivateRelease(user, draft.CandidateID, "a2", at); err == nil {
		t.Fatal("a non-builder moved the release pointer")
	}
	if id, err := live.ActivateRelease(member, draft.CandidateID, "a3", at); err != nil || id != draft.CandidateID {
		t.Fatalf("activate published candidate: %s, %v", id, err)
	}
	if _, err := live.ActivateRelease(member, "sha256-v1:missing", "a4", at); err == nil {
		t.Fatal("an unsaved candidate was activated")
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recovered.ActiveRelease() != draft.CandidateID {
		t.Fatalf("replay lost the active release: %q", recovered.ActiveRelease())
	}
}
