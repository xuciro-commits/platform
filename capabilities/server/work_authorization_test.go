package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/work"
	enterprise "platformserver/engine/organization"
	"platformserver/platform"
)

// A neutral owner fixture keeps approval/calendar and protected-read coverage
// independent of retired reference applications. It has no business callbacks.
type approvalDocument struct {
	platform.Record
	Title  string `json:"title" field:"required,search"`
	Secret string `json:"secret,omitempty" read:"reviewer" personal:"true"`
	Owner  string `json:"owner" field:"required"`
	State  string `json:"state" field:"readonly"`
}
type approvalDocumentApp struct{ ledger *platform.Ledger }

func approvalDocumentEntity() platform.Entity {
	return platform.Entity{Type: "review.document", Title: "Document", Model: approvalDocument{},
		Standard: platform.Standard{Create: true, Edit: true, Roles: []string{"writer", "reviewer"}},
		Scope:    platform.Scope{Owner: "owner", Levels: map[string]string{"writer": platform.ScopeOwn, "reviewer": platform.ScopeTenant}},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft"}, {Name: "pending", Title: "Pending"}, {Name: "approved", Title: "Approved"}, {Name: "rejected", Title: "Rejected"}}, Transitions: []platform.Transition{
			{Name: "submit", Title: "Submit", From: []string{"draft"}, To: []string{"approved"}, Roles: []string{"writer"}, Payload: []platform.Field{{Name: "extended", Type: "boolean"}}, Approval: &platform.Approval{Pending: "pending", Rejected: "rejected", Levels: []platform.ApprovalLevel{
				{Title: "Manager", Structure: "management", Role: "manager", WorkingDays: 2},
				{Title: "Head", Structure: "management", Role: "head", When: func(_ platform.Caller, s *pb.Submission) bool {
					var input struct{ Extended bool }
					_ = json.Unmarshal(s.Payload, &input)
					return input.Extended
				}},
			}}},
			{Name: "revise", Title: "Revise", From: []string{"rejected"}, To: []string{"draft"}, Roles: []string{"writer"}},
		}},
	}
}
func newApprovalDocumentApp(id string) *approvalDocumentApp {
	return &approvalDocumentApp{platform.NewLedger(id, "review", platform.NewCatalog(platform.EntityActions(approvalDocumentEntity())...), "review.document")}
}
func (a *approvalDocumentApp) Manifest() platform.Manifest {
	return platform.Manifest{ID: "review", Version: "1", Actions: a.ledger.Catalog, Entities: []platform.Entity{approvalDocumentEntity()}}
}
func (a *approvalDocumentApp) AcceptedLedger() *platform.Ledger { return a.ledger }
func (a *approvalDocumentApp) Declarations() []*pb.AuthorityDeclaration {
	return a.ledger.Declarations()
}
func (a *approvalDocumentApp) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }
func (a *approvalDocumentApp) Restore(raw json.RawMessage) error  { return a.ledger.Restore(raw) }
func (a *approvalDocumentApp) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, unknown()
}
func (a *approvalDocumentApp) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, unknown()
}
func (a *approvalDocumentApp) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if receipt, problem, ok := a.ledger.Generated(c, s, at, nil, approvalDocumentEntity()); ok {
		return receipt, problem
	}
	return nil, unknown()
}
func approvalOwnerTenant(t *testing.T, id string) *Tenant {
	seats := []platform.Seat{seatOf("alice", "review:writer"), seatOf("front", "review:writer"), seatOf("bob", "review:reviewer"), seatOf("carol", "review:reviewer"), seatOf("other", "review:reviewer"), seatOf("bot", "review:reviewer")}
	seats[5].Agent = true
	chart := platform.OrgSeed{Structures: []platform.Structure{{ID: "management", Name: "Management", Kind: "management"}}, Units: []platform.Unit{{ID: "root", Name: "Root", Kind: "company"}, {ID: "office", Name: "Office", Kind: "team", Calendar: "office"}, {ID: "front", Name: "Front", Kind: "team", Calendar: "daily"}}, Edges: []platform.Edge{{Structure: "management", Unit: "office", Parent: "root"}, {Structure: "management", Unit: "front", Parent: "root"}}, Memberships: []platform.Membership{{Party: "member:alice", Unit: "office", Role: "member", Primary: true}, {Party: "member:front", Unit: "front", Role: "member", Primary: true}, {Party: "member:bob", Unit: "office", Role: "manager"}, {Party: "member:bot", Unit: "office", Role: "manager"}, {Party: "member:bob", Unit: "root", Role: "manager"}, {Party: "member:carol", Unit: "root", Role: "head"}}, Calendars: []platform.Calendar{{ID: "office", Name: "Office", Holidays: []platform.Date{"2026-10-01", "2026-10-02", "2026-10-05", "2026-10-06", "2026-10-07"}}, {ID: "daily", Name: "Daily", Workdays: []int{1, 2, 3, 4, 5, 6, 7}}}}
	return composeTenant(t, id, seats, enterprise.New(id, chart), work.New(id), newApprovalDocumentApp(id))
}
func TestWorkApprovalKeepsOrganizationCalendarVisibilityAndRecovery(t *testing.T) {
	at := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	tn := approvalOwnerTenant(t, "work-owner")
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	request := func(who, id string, extended bool) work.ApprovalRequest {
		t.Helper()
		decide(t, tn, who, "review", "review.document.create", "review.document", id, map[string]any{"title": id, "owner": who}, at)
		view, problem := tn.RecordOf(memberOf(t, tn, who), "review.document", id, at)
		if problem != nil {
			t.Fatal(problem)
		}
		revision := view.Record.(approvalDocument).Revision
		input := submission(tn, memberOf(t, tn, who), "review", "review.document.submit", "review.document", id, map[string]any{"extended": extended})
		input.ExpectedRevision = &revision
		if _, problem := tn.Submit(memberOf(t, tn, who), input, at); problem != nil {
			t.Fatal(problem)
		}
		rows, problem := tn.Read(memberOf(t, tn, who), "requests")
		if problem != nil {
			t.Fatal(problem)
		}
		for _, r := range rows.([]work.ApprovalRequest) {
			if r.Target == "review.document/"+id && r.State == "pending" {
				return r
			}
		}
		t.Fatal("missing held approval")
		return work.ApprovalRequest{}
	}
	one := request("alice", "short", false)
	if len(one.Levels) != 1 || !slices.Equal(one.Levels[0].Approvers, []string{"bob", "bot"}) || one.Levels[0].Due.Format(time.DateOnly) != "2026-10-09" {
		t.Fatalf("office approvers/calendar: %+v", one)
	}
	for _, who := range []string{"bot", "carol", "other"} {
		if got := refuse(t, tn, who, work.ID, "work.approval.approve", work.ApprovalType, one.ID, map[string]any{}, at); !strings.Contains(got, "POLICY_DENIED") {
			t.Fatalf("%s approved: %s", who, got)
		}
	}
	if _, problem := tn.RecordOf(memberOf(t, tn, "other"), work.ApprovalType, one.ID, at); problem == nil {
		t.Fatal("unrelated member read the approval")
	}
	decide(t, tn, "bob", work.ID, "work.approval.approve", work.ApprovalType, one.ID, map[string]any{}, at)
	view, problem := tn.RecordOf(memberOf(t, tn, "alice"), "review.document", "short", at)
	if problem != nil || view.Record.(approvalDocument).State != "approved" {
		t.Fatalf("original revision did not finish: %+v %v", view.Record, problem)
	}
	notes, _ := tn.Read(memberOf(t, tn, "bob"), "notifications")
	if !slices.ContainsFunc(notes.([]platform.Notification), func(n platform.Notification) bool { return strings.Contains(n.Title, "short") && n.Read }) {
		t.Fatal("answered task notification stayed unread")
	}
	chain := request("alice", "extended", true)
	if len(chain.Levels) != 2 || !slices.Equal(chain.Levels[1].Approvers, []string{"carol"}) {
		t.Fatalf("conditional organization chain: %+v", chain)
	}
	decide(t, tn, "bob", work.ID, "work.approval.approve", work.ApprovalType, chain.ID, map[string]any{}, at)
	decide(t, tn, "carol", work.ID, "work.approval.reject", work.ApprovalType, chain.ID, map[string]any{"note": "Revise the source"}, at)
	for _, who := range []string{"alice", "bob"} {
		notes, _ := tn.Read(memberOf(t, tn, who), "notifications")
		if !slices.ContainsFunc(notes.([]platform.Notification), func(n platform.Notification) bool {
			return strings.Contains(n.Title, "rejected by carol") && n.Body == "Revise the source"
		}) {
			t.Fatalf("%s did not hear the rejection", who)
		}
	}
	withdrawn := request("alice", "withdrawn", false)
	if got := refuse(t, tn, "bob", work.ID, "work.approval.withdraw", work.ApprovalType, withdrawn.ID, map[string]any{}, at); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatal(got)
	}
	decide(t, tn, "alice", work.ID, "work.approval.withdraw", work.ApprovalType, withdrawn.ID, map[string]any{}, at)
	view, problem = tn.RecordOf(memberOf(t, tn, "alice"), "review.document", "withdrawn", at)
	if problem != nil || view.Record.(approvalDocument).State != "draft" {
		t.Fatal("withdrawal did not return the original state")
	}
	decide(t, tn, "bob", work.ID, work.SchemaDelegate, work.DelegationType, "delegation", map[string]any{"to": "other", "start": "2026-09-30", "end": "2026-10-20"}, at)
	delegated := request("alice", "delegated", false)
	decide(t, tn, "other", work.ID, "work.approval.approve", work.ApprovalType, delegated.ID, map[string]any{}, at)
	delegatedView, problem := tn.RecordOf(memberOf(t, tn, "alice"), work.ApprovalType, delegated.ID, at)
	if problem != nil {
		t.Fatal(problem)
	}
	decided := delegatedView.Record.(work.ApprovalRequest)
	if decided.State != "approved" || !slices.Contains(decided.Levels[0].Approved, "bob") || decided.Levels[0].DecidedBy["bob"] != "other" {
		t.Fatalf("delegation lost original approver or actual signer: %+v", decided)
	}
	decide(t, tn, "bob", work.ID, work.SchemaUndelegate, work.DelegationType, "delegation", map[string]any{}, at)
	request("alice", "office-late", false)
	daily := request("front", "daily", true)
	if daily.Levels[0].Due.Format(time.DateOnly) != "2026-10-02" {
		t.Fatalf("daily calendar: %+v", daily.Levels[0])
	}
	tn.Work(at.Add(72 * time.Hour))
	notes, _ = tn.Read(memberOf(t, tn, "bob"), "notifications")
	if slices.ContainsFunc(notes.([]platform.Notification), func(n platform.Notification) bool {
		return strings.Contains(n.Title, "Overdue") && strings.Contains(n.Title, "withdrawn")
	}) {
		t.Fatal("withdrawn task escalated")
	}
	countOverdue := func(id string) int {
		t.Helper()
		notes, problem := tn.Read(memberOf(t, tn, "bob"), "notifications")
		if problem != nil {
			t.Fatal(problem)
		}
		count := 0
		for _, n := range notes.([]platform.Notification) {
			if strings.HasPrefix(n.Title, "Overdue:") && strings.Contains(n.Title, "/"+id) {
				count++
			}
		}
		return count
	}
	if countOverdue("daily") != 1 || countOverdue("office-late") != 0 {
		t.Fatal("overdue work ignored the requester's calendar")
	}
	late := time.Date(2026, 10, 9, 9, 1, 0, 0, time.UTC)
	tn.Work(late)
	tn.Work(late.Add(time.Hour))
	if countOverdue("daily") != 1 || countOverdue("office-late") != 1 {
		t.Fatal("overdue work was missing or notified twice")
	}
	CheckReplay(t, tn, entries, func() *Tenant { return approvalOwnerTenant(t, tn.ID) })
}
func TestProtectedFieldsKeepHistorySearchExportAndAuditAtOriginalOwner(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	tn := approvalOwnerTenant(t, "protected-owner")
	decide(t, tn, "bob", "review", "review.document.create", "review.document", "note", map[string]any{"title": "Public title", "owner": "alice", "secret": "restricted fixture value"}, at)
	alice, bob := memberOf(t, tn, "alice"), memberOf(t, tn, "bob")
	visible, problem := tn.RecordOf(alice, "review.document", "note", at)
	if problem != nil || visible.Record.(approvalDocument).Secret != "" || strings.Contains(string(platform.Raw(visible.History)), "restricted fixture value") {
		t.Fatal("a protected field escaped the record/history projection")
	}
	private, problem := tn.RecordOf(bob, "review.document", "note", at)
	if problem != nil || private.Record.(approvalDocument).Secret != "restricted fixture value" {
		t.Fatal("reviewer lost its protected field")
	}
	if result, problem := tn.Records(alice, "review.document", platform.Query{Search: "restricted fixture value"}, at); problem != nil || result.Total != 0 {
		t.Fatal("search disclosed a hidden field")
	}
	if _, problem := tn.Records(alice, "review.document", platform.Query{Domain: platform.Raw([]any{[]any{"secret", "=", "restricted fixture value"}})}, at); problem == nil {
		t.Fatal("hidden filter was accepted")
	}
	if _, problem := tn.Aggregate(alice, "review.document", AggregateQuery{Groups: []string{"secret"}}, at); problem == nil {
		t.Fatal("hidden aggregation was accepted")
	}
	publicCSV, problem := tn.Export(alice, "review.document", platform.Query{}, at)
	if problem != nil || strings.Contains(string(publicCSV), "secret") || strings.Contains(string(publicCSV), "restricted fixture value") {
		t.Fatal("export disclosed a protected field")
	}
	privateCSV, problem := tn.Export(bob, "review.document", platform.Query{}, at)
	if problem != nil || !strings.Contains(string(privateCSV), "restricted fixture value") {
		t.Fatal("authorized export lost a protected field")
	}
	for _, entity := range tn.Entities(alice) {
		if entity.Type == "review.document" {
			if _, visible := entity.Field("secret"); visible {
				t.Fatal("a public form offered a protected field")
			}
		}
	}
	reads := tn.PersonalReads()
	if !slices.ContainsFunc(reads, func(r platform.PersonalRead) bool {
		return r.Member == "bob" && r.Type == "review.document" && slices.Equal(r.Fields, []string{"secret"})
	}) || slices.ContainsFunc(reads, func(r platform.PersonalRead) bool { return r.Member == "alice" }) {
		t.Fatalf("protected read audit: %+v", reads)
	}
}
