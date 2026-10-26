package platformserver

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

func TestSubmitRejectsForeignMembersBeforeNewOrRepeatedDecisions(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "accepted-result"}[accepted], func(t *testing.T) {
			tn := stockTenant(t)
			member, _ := tn.Member("ana")
			var journal []Entry
			tn.Record = func(e Entry) { journal = append(journal, e) }
			if accepted {
				tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { journal = append(journal, e); return e.Body, nil }
			}
			s := &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: "stock", IdempotencyKey: "scoped",
				Target: &pb.EntityRef{Type: "stock.bin", Id: "B1"}, Schema: &pb.SchemaRef{Name: "stock.bin.create", Version: 1}, Payload: []byte(`{"code":"A"}`)}
			at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
			first, err := tn.Submit(member, s, at)
			if err != nil {
				t.Fatal(err)
			}
			before, count := snapshot(tn), len(journal)
			for _, tenant := range []string{"another", ""} {
				foreign := member
				foreign.Tenant = tenant
				for _, duplicate := range []bool{true, false} {
					request := proto.Clone(s).(*pb.Submission)
					if !duplicate {
						request.IdempotencyKey = "fresh"
						request.Target.Id = "B2"
					}
					if record, refusal := tn.Submit(foreign, request, at); refusal == nil || record != nil || refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
						t.Fatalf("foreign member received a decision: %+v %v", record, refusal)
					}
					if snapshot(tn) != before || len(journal) != count {
						t.Fatal("foreign submission changed state/journal")
					}
				}
			}
			if replay, err := tn.Submit(member, s, at); err != nil || !proto.Equal(first, replay) {
				t.Fatal("valid member lost original receipt")
			}
		})
	}
}
