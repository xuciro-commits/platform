package platformserver

import (
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/studio/build"
)

func TestInvariantK1ReplayIsByteIdentical(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k1")
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.create", build.ObjectType, "OBJ2", invariantObject("beta", "Beta"))
	live.accept(t, "build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, revised"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, "build.alpha.create", "build.alpha", "A1", map[string]any{"note": "first"})
	live.accept(t, "build.alpha.create", "build.alpha", "A2", map[string]any{"note": "second"})
	stale := live.submission("build.object.edit", build.ObjectType, "OBJ2", invariantObject("beta", "Beta, stale"))
	zero := uint32(0)
	stale.ExpectedRevision = &zero
	if _, refusal := live.Submit(live.member(t), stale, invariantClock); refusal == nil {
		t.Fatal("the stale submission was accepted")
	}
	if len(live.entries) != 7 {
		t.Fatalf("the journal holds %d entries; want 6 accepted results and 1 sealed refusal", len(live.entries))
	}
	want := live.recordImage(t)
	for _, typ := range []string{build.ObjectType, "build.alpha"} {
		if len(want[typ]) == 0 {
			t.Fatalf("the live record store has no %s rows", typ)
		}
	}
	for replay := 1; replay <= 2; replay++ {
		restored := live.restore(t, live.entries)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("replay %d differs from the live record store in %v", replay, differing)
		}
	}
}

func TestInvariantK2SameKeyReturnsSameAnswer(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		tn := newInvariantTenant(t, "invariant-k2-accepted")
		request := tn.submission("build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
		again := proto.Clone(request).(*pb.Submission)
		first, refusal := tn.Submit(tn.member(t), request, invariantClock)
		if refusal != nil {
			t.Fatalf("first submission: %s: %s", refusal.Code, refusal.Message)
		}
		if len(tn.entries) != 1 {
			t.Fatalf("first submission sealed %d results; want 1", len(tn.entries))
		}
		second, refusal := tn.Submit(tn.member(t), again, invariantClock.Add(time.Hour))
		if refusal != nil {
			t.Fatalf("repeated submission: %s: %s", refusal.Code, refusal.Message)
		}
		if !proto.Equal(first, second) {
			t.Fatalf("repeated submission answered differently:\nfirst:  %v\nsecond: %v", first, second)
		}
		if len(tn.entries) != 1 {
			t.Fatalf("the journal holds %d accepted results after the repeat; want 1", len(tn.entries))
		}
	})
	t.Run("refused", func(t *testing.T) {
		tn := newInvariantTenant(t, "invariant-k2-refused")
		tn.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
		request := tn.submission("build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, stale"))
		zero := uint32(0)
		request.ExpectedRevision = &zero
		again := proto.Clone(request).(*pb.Submission)
		_, first := tn.Submit(tn.member(t), request, invariantClock)
		if first == nil {
			t.Fatal("the stale submission was accepted")
		}
		if len(tn.entries) != 2 {
			t.Fatalf("the journal holds %d results after the refusal; want 2", len(tn.entries))
		}
		_, second := tn.Submit(tn.member(t), again, invariantClock.Add(time.Hour))
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("repeated refusal answered differently:\nfirst:  %+v\nsecond: %+v", first, second)
		}
		if len(tn.entries) != 2 {
			t.Fatalf("the journal holds %d results after the repeat; want 2", len(tn.entries))
		}
	})
}

func TestInvariantK3StaleRevisionIsRefusedAndSealed(t *testing.T) {
	tn := newInvariantTenant(t, "invariant-k3")
	tn.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	tn.accept(t, "build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, revised"))
	sealedBefore := len(tn.entries)
	imageBefore := tn.recordImage(t)
	stale := tn.submission("build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, stale"))
	read := uint32(1)
	stale.ExpectedRevision = &read
	record, refusal := tn.Submit(tn.member(t), stale, invariantClock)
	if record != nil || refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
		t.Fatalf("stale expected revision: record=%v refusal=%v; want a CONFLICT refusal", record, refusal)
	}
	if len(tn.entries) != sealedBefore+1 {
		t.Fatalf("the refusal sealed %d journal entries; want 1", len(tn.entries)-sealedBefore)
	}
	saved, sealed := tn.sealedRefusal(t, sealedBefore)
	if saved.Error.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || sealed.GetIdempotencyKey() != stale.GetIdempotencyKey() || sealed.GetExpectedRevision() != read {
		t.Fatalf("sealed refusal does not carry the refused submission: code=%s key=%q expected=%d", saved.Error.Code, sealed.GetIdempotencyKey(), sealed.GetExpectedRevision())
	}
	if differing := differingTypes(imageBefore, tn.recordImage(t)); len(differing) != 0 {
		t.Fatalf("the refused submission changed the record store in %v", differing)
	}
	current := uint32(2)
	fresh := tn.submission("build.object.edit", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha, current"))
	fresh.ExpectedRevision = &current
	accepted, refusal := tn.Submit(tn.member(t), fresh, invariantClock)
	if refusal != nil || accepted.GetRevision() != 3 {
		t.Fatalf("the current expected revision: record=%v refusal=%v; want revision 3", accepted, refusal)
	}
}
