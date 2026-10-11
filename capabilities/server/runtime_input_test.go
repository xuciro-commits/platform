package platformserver

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type runtimeInputOwner struct {
	*notes
	decisions *int
	refuse    bool
	blocked   bool
	cancel    *pb.ChangeRecord
	operation string
}

func (a *runtimeInputOwner) ForkAcceptedState() (platform.App, error) {
	fork, err := a.notes.ForkAcceptedState()
	if err != nil {
		return nil, err
	}
	copy := *a
	copy.notes = fork.(*notes)
	return &copy, nil
}

func (a *runtimeInputOwner) Input(c platform.Caller, name string, body []byte, at time.Time) (any, *kernel.Error) {
	if a.blocked {
		panic("saved input called decision code")
	}
	*a.decisions++
	if a.cancel != nil {
		if refusal := c.CancelOperation(a.cancel, a.operation); refusal != nil {
			return nil, refusal
		}
	}
	answer, refusal := a.notes.Input(c, name, body, at)
	if a.refuse {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The input was refused")
	}
	return answer, refusal
}

func TestRecordedInputStagesInEveryStorageMode(t *testing.T) {
	for _, mode := range []string{"memory", "record", "accepted-store"} {
		for _, refused := range []bool{false, true} {
			t.Run(mode+"/"+map[bool]string{false: "accept", true: "refuse"}[refused], func(t *testing.T) {
				calls := 0
				owner := &runtimeInputOwner{notes: newNotes("input-runtime", "a"), decisions: &calls, refuse: refused}
				tn := composeTenant(t, "input-runtime", []Seat{seatOf("ana", "a:writer")}, owner)
				member := memberOf(t, tn, "ana")
				at := time.Date(2026, 10, 11, 21, 0, 0, 0, time.UTC)
				var entries []Entry
				appendResult := func(entry Entry) {
					if len(owner.texts) != 0 || len(tn.Audit()) != 0 {
						t.Fatal("private state or audit became visible before acceptance")
					}
					if entry.Kind != "accepted-result" {
						t.Fatalf("new input wrote legacy history: %s", entry.Kind)
					}
					entries = append(entries, entry)
				}
				if mode == "record" {
					tn.Record = appendResult
				} else if mode == "accepted-store" {
					tn.Record = func(Entry) { t.Fatal("accepted input also used Record") }
					tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
						appendResult(entry)
						return entry.Body, nil
					}
				}
				_, refusal := tn.Input(member, "a-feed", []byte("accepted"), at)
				if (refusal != nil) != refused || calls != 1 {
					t.Fatalf("input answer: %v, decisions=%d", refusal, calls)
				}
				if refused && (len(owner.texts) != 0 || len(tn.Audit()) != 0) ||
					!refused && (owner.texts["feed"] != "accepted" || len(tn.Audit()) != 1) {
					t.Fatal("accepted input applied the wrong authoritative state")
				}
				owner.blocked = true
				_, retryRefusal := tn.Input(member, "a-feed", []byte("accepted"), at.Add(time.Hour))
				if (retryRefusal != nil) != refused || calls != 1 {
					t.Fatal("retry did not reuse the accepted answer")
				}
				if mode == "memory" && tn.AcceptResult != nil {
					t.Fatal("input silently changed the tenant's storage configuration")
				}
				if mode != "memory" {
					if len(entries) != 1 {
						t.Fatalf("input accepted %d journal entries", len(entries))
					}
					freshOwner := &runtimeInputOwner{notes: newNotes(tn.ID, "a"), decisions: &calls, blocked: true}
					fresh := composeTenant(t, tn.ID, []Seat{seatOf("ana", "a:writer")}, freshOwner)
					if err := fresh.Replay(entries); err != nil {
						t.Fatal(err)
					}
					if calls != 1 || freshOwner.texts["feed"] != owner.texts["feed"] {
						t.Fatal("recovery reran the input or omitted its saved state")
					}
					if _, replayRefusal := fresh.Input(member, "a-feed", []byte("accepted"), at.Add(time.Hour)); (replayRefusal != nil) != refused || calls != 1 {
						t.Fatal("recovered input retry reran its decision")
					}
				}
			})
		}
	}
}

func TestRecordedInputRecordFailureLeavesPrivateState(t *testing.T) {
	tn, owner, _ := setup(t, nil)
	tn.Record = func(Entry) { panic("append failed") }
	var failure any
	func() {
		defer func() { failure = recover() }()
		_, _ = tn.Input(memberOf(t, tn, "ana"), "a-feed", []byte("private"), time.Now())
	}()
	if failure == nil || len(owner.texts) != 0 || len(tn.Audit()) != 0 || len(tn.committed.inputs) != 0 {
		t.Fatal("failed Record append exposed private state or swallowed its fatal failure")
	}
}

func TestRecordedInputLegacyHistoryStillReplays(t *testing.T) {
	tn, owner, _ := setup(t, nil)
	member := memberOf(t, tn, "ana")
	principal, _ := json.Marshal(member)
	at := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)
	legacy := Entry{App: "a", Kind: "a-feed", Principal: principal, Body: []byte("historical"), At: at}
	if err := tn.Replay([]Entry{legacy}); err != nil || owner.texts["feed"] != "historical" {
		t.Fatalf("legacy recorded input no longer replays: %v, %v", err, owner.texts)
	}
	var entry Entry
	tn.Record = func(saved Entry) { entry = saved }
	if _, refusal := tn.Input(member, "a-feed", []byte("new-result"), at.Add(time.Hour)); refusal != nil ||
		entry.Kind != "accepted-result" || owner.texts["feed"] != "new-result" {
		t.Fatalf("legacy history did not admit the new result path: %v, %s", refusal, entry.Kind)
	}
	fresh, freshOwner, _ := setup(t, nil)
	if err := fresh.Replay([]Entry{legacy, entry}); err != nil || freshOwner.texts["feed"] != "new-result" {
		t.Fatalf("mixed legacy/new history did not recover: %v, %v", err, freshOwner.texts)
	}
}

func TestRecordedInputCannotDropAnOperationCancellation(t *testing.T) {
	for _, mode := range []string{"memory", "record", "accepted-store"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			owner := &runtimeInputOwner{notes: newNotes("input-cancel", "a"), decisions: &calls, operation: "input-cancel:operation"}
			tn := composeTenant(t, "input-cancel", []Seat{seatOf("ana", "a:writer")}, owner)
			member := memberOf(t, tn, "ana")
			owner.cancel = &pb.ChangeRecord{Submission: submission(tn, member, "a", "a.note", "a.topic", "feed", "")}
			tn.outbound = append(tn.outbound, &effect{Effect: platform.Effect{ID: owner.operation, Endpoint: operationEndpoint,
				State: "pending", Body: string(platform.Raw(operationBinding{Member: member.ID}))}})
			if mode == "record" {
				tn.Record = func(Entry) { t.Fatal("unsupported cancellation wrote a result") }
			} else if mode == "accepted-store" {
				tn.AcceptResult = func(Entry, string, string) ([]byte, error) {
					t.Fatal("unsupported cancellation wrote a result")
					return nil, nil
				}
			}
			_, refusal := tn.Input(member, "a-feed", []byte("private"), time.Now())
			if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || calls != 1 ||
				len(owner.texts) != 0 || len(tn.committed.inputs) != 0 || len(tn.Audit()) != 0 || tn.outbound[0].State != "pending" {
				t.Fatalf("input dropped an unstored cancellation: %v, decisions=%d", refusal, calls)
			}
		})
	}
}

// This private owner deliberately omits its publication while retaining all
// other named staged-runtime methods, so the writer sees the unreported log.
type unpublishedInputRuntime struct{ platform.StagedRuntime }

func (unpublishedInputRuntime) Publish(platform.Caller, *pb.ChangeRecord) {}

func TestRecordedInputCannotDropAnUnpublishedDecision(t *testing.T) {
	calls := 0
	owner := &runtimeInputOwner{notes: newNotes("input-unpublished", "a"), decisions: &calls}
	tn := composeTenant(t, "input-unpublished", []Seat{seatOf("ana", "a:writer")}, owner)
	member := memberOf(t, tn, "ana")
	at := time.Date(2026, 10, 11, 22, 0, 0, 0, time.UTC)
	draft := tn.newStagedDecision()
	caller := platform.NewCaller(unpublishedInputRuntime{draft}, member, "a", false, false)
	receipt, refusal := owner.ledger.Receive(caller, submission(tn, member, "a", "a.note", "a.topic", "feed", ""), at, nil, nil)
	if refusal != nil || len(draft.events) != 0 {
		t.Fatalf("unpublished private decision fixture failed: %v", refusal)
	}
	// The writer must not silently omit an actual unreported private receipt.
	if _, err := draft.inputResult("a", "a-feed", member, []byte("private"), receipt, at); err == nil ||
		len(owner.AcceptedLedger().RecordsFor(tn.ID)) != 0 || len(tn.committed.inputs) != 0 {
		t.Fatalf("input result dropped an unpublished private decision: %v", err)
	}
}
