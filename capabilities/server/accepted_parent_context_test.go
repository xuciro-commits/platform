package platformserver

import (
	"maps"
	controlowner "platformserver/kernel/control"
	"slices"
	"sync"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	enterprise "platformserver/engine/organization"
	"platformserver/platform"
)

type lockedParentDecision struct {
	*notes
	mu      sync.Mutex
	child   platform.App
	outcome string
}

func (p *lockedParentDecision) ForkAcceptedState() (platform.App, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &lockedParentDecision{notes: &notes{id: p.id, ledger: p.ledger, texts: maps.Clone(p.texts)}, child: p.child, outcome: p.outcome}, nil
}

func (p *lockedParentDecision) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		child := func(private platform.Caller) (*pb.ChangeRecord, *kernel.Error) {
			request := submission(&Tenant{ID: c.Tenant}, c.Member, "child", "child.note", "child.topic", "child", "child")
			record, err := platform.Decide(private, p.child, request, at)
			if err == nil && p.outcome == "panic" {
				panic("Optional child panic")
			}
			if err == nil && p.outcome == "refused" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Optional child refused")
			}
			return record, err
		}
		if p.outcome == "recursive-probe" {
			if err := platform.ProbeDecision(c, func(private platform.Caller) *kernel.Error {
				_, err := platform.Decide(private, p, s, at)
				return err
			}); err == nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Child re-entered its suspended parent")
			}
		} else if p.outcome == "probe" {
			if err := platform.ProbeDecision(c, func(private platform.Caller) *kernel.Error { _, err := child(private); return err }); err != nil {
				return nil, err
			}
		} else {
			var err *kernel.Error
			var caught any
			func() {
				defer func() { caught = recover() }()
				_, err = platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) { return child(c) })
			}()
			if caught != nil && (p.outcome != "panic" || caught != "Optional child panic") {
				panic(caught)
			}
			if err != nil && p.outcome != "refused" {
				return nil, err
			}
		}
		// This callback resumes on the same suspended parent receiver after the
		// child. Replacing that receiver drops the parent's private state update.
		return func(*pb.ChangeRecord) { p.texts[s.GetTarget().GetId()] = string(s.GetPayload()) }, nil
	})
}

func TestAcceptedParentKeepsActiveReceiverAcrossChildSavepoints(t *testing.T) {
	for _, outcome := range []string{"accepted", "refused", "panic", "probe", "recursive-probe"} {
		for _, directLedger := range []bool{false, true} {
			t.Run(outcome+map[bool]string{false: "/submit", true: "/internal-receive"}[directLedger], func(t *testing.T) {
				const tenant = "active-parent"
				child := newNotes(tenant, "child")
				parent := &lockedParentDecision{notes: newNotes(tenant, "parent"), child: child, outcome: outcome}
				tn := composeTenant(t, tenant, []platform.Seat{seatOf("writer", "parent:writer", "child:writer")}, parent, child)
				at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
				if directLedger {
					member := memberOf(t, tn, "writer")
					request := submission(tn, member, "parent", "parent.note", "parent.topic", "parent", "parent")
					draft := tn.newStagedDecision()
					receipt, refusal := draft.stateApp(parent).Submit(platform.NewCaller(draft, member, "parent", false, false), request, at)
					if refusal != nil {
						t.Fatal(refusal)
					}
					raw, err := draft.batchResult("parent", request, receipt, at)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := tn.applyAcceptedBatch(parent.AcceptedLedger(), raw); err != nil {
						t.Fatal(err)
					}
				} else {
					decide(t, tn, "writer", "parent", "parent.note", "parent.topic", "parent", "parent", at)
				}
				if parent.texts["parent"] != `"parent"` || len(parent.AcceptedLedger().RecordsFor(tenant)) != 1 {
					t.Fatal("child savepoint discarded the suspended parent's state or receipt")
				}
				_, childSaved := child.texts["child"]
				if childSaved != (outcome == "accepted") || len(child.AcceptedLedger().RecordsFor(tenant)) != map[bool]int{false: 0, true: 1}[outcome == "accepted"] {
					t.Fatal("optional child leaked or lost its private state")
				}
			})
		}

	}
}

func TestPrivateRecipientsReadUncommittedDirectoryOwners(t *testing.T) {
	const tenant = "private-recipients"
	enterpriseOwner := enterprise.New(tenant, platform.OrgSeed{Units: []platform.Unit{{ID: "unit", Name: "Unit", From: "2020-01-01"}}})
	tn := composeTenant(t, tenant, []platform.Seat{seatOf("admin", controlowner.PlatformApp+":"+controlowner.Admin, "stock:clerk", enterprise.ID+":"+enterprise.Admin), seatOf("recipient")}, newStock(tenant), enterpriseOwner)
	member := memberOf(t, tn, "admin")
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	draft := tn.newStagedDecision()
	caller := platform.NewCaller(draft, member, controlowner.PlatformApp, false, false)
	_, refused := platform.Decide(caller, tn.app(controlowner.PlatformApp), submission(tn, member, controlowner.PlatformApp, controlowner.SchemaGrant, controlowner.MemberType, "recipient", map[string]string{"app": "stock", "role": "clerk"}), at)
	if refused != nil {
		t.Fatal(refused)
	}
	_, refused = platform.Decide(caller, enterpriseOwner, submission(tn, member, enterprise.ID, enterprise.SchemaRelationshipAdd, enterprise.RelationshipType, "membership", map[string]string{"stereotype": enterprise.Membership, "source": "member:recipient", "target": "unit", "role": "operator"}), at)
	if refused != nil {
		t.Fatal(refused)
	}
	stockCaller := platform.RouteCaller(caller, "stock")
	roles := stockCaller.Notify(platform.Notification{Title: "New role", Key: "role"}, at, platform.Recipient{AppRole: "clerk"})
	units := stockCaller.Notify(platform.Notification{Title: "New membership", Key: "unit"}, at, platform.Recipient{Unit: "unit", Role: "operator"})
	if !slices.Contains(roles, "recipient") || !slices.Equal(units, []string{"recipient"}) {
		t.Fatalf("private recipient owners were read from live state: roles=%v units=%v", roles, units)
	}
	if live, _ := tn.member("recipient"); live.Holds("stock", "clerk") || len(tn.notices.ForMember("recipient")) != 0 || len(enterpriseOwner.Holders("", "unit", "operator", at.Format(time.DateOnly))) != 0 {
		t.Fatal("resolving private recipients exposed the directory or notifications")
	}
}
