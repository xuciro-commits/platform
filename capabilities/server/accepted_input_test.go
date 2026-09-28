package platformserver

import (
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

func TestDurableInputWithoutAcceptedOwnerFailsClosed(t *testing.T) {
	var entries []Entry
	tn, app, _ := setup(t, func(entry Entry) { entries = append(entries, entry) })
	member, _ := tn.Member("ana")
	now := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if answer, refusal := tn.Input(member, "a-feed", []byte("uncommitted"), now); answer != nil ||
		refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("unmigrated durable input was not refused: %v %v", answer, refusal)
	}
	if len(entries) != 0 || len(app.texts) != 0 || len(tn.Audit()) != 0 {
		t.Fatal("unmigrated input escaped the accepted-result boundary")
	}
}
