package journal

import (
	"testing"
	"time"
)

func TestMemoryPurgeBeforeFirstTranscript(t *testing.T) {
	var store Memory
	now := time.Now().UTC()
	store.PurgeTranscripts("a", now)
	store.PurgeTranscripts("a", now)
	store.SaveTranscript(Transcript{Tenant: "a", At: now.Add(-time.Hour)})
	store.SaveTranscript(Transcript{Tenant: "a", At: now})
	store.SaveTranscript(Transcript{Tenant: "b", At: now.Add(-time.Hour)})
	store.PurgeTranscripts("a", now)
	if got := store.Transcripts("a", "", 10); len(got) != 1 || !got[0].At.Equal(now) {
		t.Fatalf("purge must retain the cutoff transcript: %+v", got)
	}
	if got := store.Transcripts("b", "", 10); len(got) != 1 {
		t.Fatalf("purge crossed tenants: %+v", got)
	}
	store.PurgeTranscripts("a", now.Add(time.Second))
	store.PurgeTranscripts("a", now.Add(time.Second))
	store.SaveTranscript(Transcript{Tenant: "a", At: now})
	if len(store.Transcripts("a", "", 10)) != 1 {
		t.Fatal("saving after an empty purge failed")
	}
}
