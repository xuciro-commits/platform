package kernel

import (
	"fmt"
	"math/rand"
	"testing"
	"testing/quick"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// TestIdempotencySequences maps generated SubmitChecked traces to ADR-0041's
// scoped, immutable receipt model; it is evidence, not a refinement proof.
func TestIdempotencySequences(t *testing.T) {
	property := func(seed uint64) bool {
		rng := rand.New(rand.NewSource(int64(seed)))
		log := NewChangeLog(NewSchemaRegistry([]*pb.SchemaRef{{Name: "x.edit", Version: 1}}, nil))
		index := map[[2]string]*pb.ChangeRecord{}
		records := map[string][]*pb.ChangeRecord{}
		for step := range 128 {
			s := &pb.Submission{TenantId: fmt.Sprintf("t%d", rng.Intn(2)), PrincipalId: fmt.Sprintf("p%d", rng.Intn(2)),
				Authority: "x", IdempotencyKey: fmt.Sprintf("k%d", rng.Intn(8)),
				Target: &pb.EntityRef{Type: "x", Id: "one"}, Schema: &pb.SchemaRef{Name: "x.edit", Version: 1},
				Payload: []byte{byte(rng.Intn(3))}}
			allowed := rng.Intn(2) == 0
			// Every trace begins with refusal, retry, replay under a changed rule.
			if step < 3 {
				s.TenantId, s.PrincipalId, s.IdempotencyKey, s.Payload = "t0", "p0", "k0", []byte{0}
				allowed = step == 1
			}
			key := [2]string{s.TenantId, s.IdempotencyKey}
			prior := index[key]
			calls := 0
			record, err := log.SubmitChecked(s, time.Unix(int64(step), 0), func() *Error {
				calls++
				if !allowed {
					return errorf(pb.ErrorCode_ERROR_CODE_POLICY_DENIED)
				}
				return nil
			})
			switch {
			case prior != nil && proto.Equal(prior.Submission, s):
				if calls != 0 || err != nil || record != prior {
					return false
				}
			case prior != nil:
				if calls != 0 || record != nil || err == nil || err.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT {
					return false
				}
			case !allowed:
				if calls != 1 || record != nil || err == nil || err.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					return false
				}
			default:
				if calls != 1 || err != nil || record == nil || !proto.Equal(record.Submission, s) {
					return false
				}
				index[key] = record
				records[s.TenantId] = append(records[s.TenantId], proto.Clone(record).(*pb.ChangeRecord))
			}
			// Compare both tenants after every outcome, including all refused requests.
			for _, tenant := range []string{"t0", "t1"} {
				actual, expected := log.Records(tenant), records[tenant]
				if len(actual) != len(expected) || len(log.byKey[tenant]) != len(expected) {
					return false
				}
				for i := range actual {
					if !proto.Equal(actual[i], expected[i]) {
						return false
					}
				}
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 200, Rand: rand.New(rand.NewSource(41))}); err != nil {
		t.Fatal(err)
	}
}

func TestReplayEqualityIncludesSubmissionFields(t *testing.T) {
	base := &pb.Submission{TenantId: "t", PrincipalId: "p", Authority: "x", IdempotencyKey: "k",
		Target: &pb.EntityRef{Type: "x", Id: "one"}, Schema: &pb.SchemaRef{Name: "x.edit", Version: 1}, Payload: []byte{1}}
	mutations := map[string]func(*pb.Submission){
		"principal":         func(s *pb.Submission) { s.PrincipalId = "q" },
		"authority":         func(s *pb.Submission) { s.Authority = "y" },
		"target":            func(s *pb.Submission) { s.Target.Id = "two" },
		"schema":            func(s *pb.Submission) { s.Schema.Version = 2 },
		"payload":           func(s *pb.Submission) { s.Payload = []byte{2} },
		"valid-time":        func(s *pb.Submission) { s.ValidTime = timestamppb.New(time.Unix(1, 0)) },
		"causation":         func(s *pb.Submission) { s.CausationId = "absent" },
		"evidence":          func(s *pb.Submission) { s.EvidenceFactIds = []string{"absent"} },
		"expected-revision": func(s *pb.Submission) { v := uint32(0); s.ExpectedRevision = &v },
		"correlation":       func(s *pb.Submission) { s.CorrelationId = "other" },
		"unknown-field":     func(s *pb.Submission) { s.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 1}) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			log := NewChangeLog(NewSchemaRegistry([]*pb.SchemaRef{{Name: "x.edit", Version: 1}, {Name: "x.edit", Version: 2}}, nil))
			s := proto.Clone(base).(*pb.Submission)
			accepted, err := log.Submit(s, time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			changed := proto.Clone(s).(*pb.Submission)
			mutate(changed)
			calls := 0
			_, err = log.SubmitChecked(changed, time.Unix(2, 0), func() *Error { calls++; return nil })
			if err == nil || err.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT || calls != 0 || len(log.Records("t")) != 1 {
				t.Fatalf("changed %s: %v; checks=%d", name, err, calls)
			}
			replay, err := log.SubmitChecked(proto.Clone(s).(*pb.Submission), time.Unix(3, 0), func() *Error { calls++; return nil })
			if err != nil || replay != accepted || calls != 0 {
				t.Fatal("equal cloned submission did not preserve the receipt")
			}
		})
	}
}
