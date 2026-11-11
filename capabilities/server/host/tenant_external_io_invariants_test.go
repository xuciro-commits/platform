package host

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	agentowner "platformserver/services/ai/runtime"
	build "platformserver/services/resources/authoring"
	filesowner "platformserver/services/resources/files"
)

type heldUploadDelete struct {
	platform.FileStore
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *heldUploadDelete) Delete(ctx context.Context, key string) error {
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.release:
	}
	return s.FileStore.Delete(ctx, key)
}

func TestInvariantK13UploadCleanupKeepsReferencesAndReleasesLane(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-upload-cleanup", []platform.App{filesowner.New("invariant-upload-cleanup")})
	store := &heldUploadDelete{FileStore: live.files(), started: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(store.release) })
	live.Files = store
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "before"})
	member := live.member(t)
	old := invariantClock.Add(-25 * time.Hour)
	upload := func(body string, at time.Time) Upload {
		t.Helper()
		value, status, err := live.Upload(member, "fixture.txt", "text/plain", strings.NewReader(body), at)
		if err != nil || status != 200 {
			t.Fatalf("upload %d: %v", status, err)
		}
		return value
	}
	attach := func(id string, value Upload) *kernel.Error {
		submission := live.submission(filesowner.SchemaAttach, platform.FileType, id, map[string]any{"hash": value.Hash, "size": value.Size, "name": value.Name, "contentType": value.ContentType, "target": "build.alpha/A1"})
		submission.Authority = platform.FileApp
		_, refusal := live.Submit(member, submission, invariantClock)
		return refusal
	}
	retained := upload("retained attachment", old)
	if refusal := attach("KEEP", retained); refusal != nil {
		t.Fatal(refusal)
	}
	ephemeral := upload("unattached upload", old)
	finished := make(chan struct{})
	go func() { live.SweepUploads(invariantClock); close(finished) }()
	select {
	case <-store.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the upload was not selected for deletion")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	_, refusal := live.RecordOf(ctx, member, "build.alpha", "A1", invariantClock)
	cancel()
	if refusal != nil || live.Lane.State().Holder != "" {
		t.Fatalf("object bytes deletion held the tenant lane: %v %+v", refusal, live.Lane.State())
	}
	if refusal := attach("RACING", ephemeral); refusal == nil {
		t.Fatal("a pending deletion accepted a new reference")
	}
	_, status, err := live.Upload(member, ephemeral.Name, ephemeral.ContentType, strings.NewReader("unattached upload"), invariantClock)
	if err == nil || status != 409 {
		t.Fatalf("upload replaced bytes during deletion: %d %v", status, err)
	}
	release.Do(func() { close(store.release) })
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the upload deletion did not finish")
	}
	if !store.Exists(t.Context(), live.ID+"/"+retained.Hash) || store.Exists(t.Context(), live.ID+"/"+ephemeral.Hash) {
		t.Fatal("cleanup lost a referenced attachment or kept the unused bytes")
	}
	restored := upload("unattached upload", invariantClock)
	if refusal := attach("AFTER", restored); refusal != nil {
		t.Fatalf("reupload could not be attached: %v", refusal)
	}
}

func TestInvariantK13AgentReadDoesNotAcquireTenantLane(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-agent-read", []platform.App{agentowner.New("invariant-agent-read")})
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "snapshot"})
	if err := live.Lane.Hold(t.Context(), "background"); err != nil {
		t.Fatal(err)
	}
	finished := make(chan *kernel.Error, 1)
	go func() {
		finished <- (hostView{t: live.Tenant}).WithAgentRead(func(runtime platform.AgentEvaluationRuntime) *kernel.Error {
			if _, found := runtime.Host.Record("build.alpha/A1"); !found {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "missing snapshot record")
			}
			return nil
		})
	}()
	select {
	case refusal := <-finished:
		if refusal != nil {
			t.Errorf("agent snapshot read: %v", refusal)
		}
	case <-time.After(time.Second):
		t.Error("the agent read waited for the tenant lane")
	}
	live.Lane.Unlock()
}
