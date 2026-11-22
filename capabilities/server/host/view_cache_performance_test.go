package host

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	build "platformserver/services/resources/authoring"
)

func TestMetadataViewCacheSurvivesRecordWrites(t *testing.T) {
	live := newInvariantTenant(t, "view-cache-perf")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	member, ok := live.Member("builder")
	if !ok {
		t.Fatal("missing builder member")
	}

	call := func(etag string) (*httptest.ResponseRecorder, time.Duration) {
		req := httptest.NewRequest(http.MethodGet, "/v1/entities", nil)
		req.Header.Set("Accept-Language", "zh-CN")
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		rec := httptest.NewRecorder()
		started := time.Now()
		if err := live.WriteView("entities", member, "zh-CN", rec, req); err != nil {
			t.Fatal(err)
		}
		return rec, time.Since(started)
	}

	var wg sync.WaitGroup
	etags := make([]string, 2)
	coldDur := make([]time.Duration, 2)
	for i := range 2 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rec, d := call("")
			if rec.Code != http.StatusOK {
				t.Errorf("cold call %d status=%d", idx, rec.Code)
			}
			etags[idx] = rec.Header().Get("ETag")
			coldDur[idx] = d
		}(i)
	}
	wg.Wait()
	if etags[0] == "" || etags[0] != etags[1] {
		t.Fatalf("concurrent cold calls returned differing etags: %q vs %q", etags[0], etags[1])
	}

	warmRec, warmDur := call(etags[0])
	if warmRec.Code != http.StatusNotModified {
		t.Fatalf("warm call status=%d, want 304", warmRec.Code)
	}

	live.accept(t, "invariantproject.alpha.create", "invariantproject.alpha", "A1", map[string]any{"note": "business-write"})
	afterRec, afterDur := call(etags[0])
	if afterRec.Code != http.StatusNotModified {
		t.Fatalf("post-write call status=%d, want 304", afterRec.Code)
	}
	t.Logf("GET /v1/entities (zh-CN): cold=%v/%v warm304=%v afterRecordWrite304=%v", coldDur[0], coldDur[1], warmDur, afterDur)
	if afterDur > 10*time.Millisecond {
		t.Fatalf("post-write 304 took %v; business write likely busted ViewCache", afterDur)
	}
}
