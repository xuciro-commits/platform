package host

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"slices"
	"testing"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
	enterprisemodel "platformserver/services/security/organization"
)

type sqlShapedReads struct {
	opened     int
	candidates []string
	filters    [][]persistence.StateFilter
	current    int
	allIDs     []string
	rows       map[string]map[string]persistence.StateCurrent
	singles    int
	batches    [][]string
	windows    []persistence.StateWindow
	window     persistence.StateWindowResult
}

type sqlShapedState struct{ reads *sqlShapedReads }

func (s sqlShapedState) InitializeState(context.Context, string, int64, persistence.StateDelta) error {
	return nil
}
func (s sqlShapedState) StateRecord(context.Context, string, string, string) (json.RawMessage, error) {
	return nil, nil
}
func (s sqlShapedState) StateCandidates(ctx context.Context, _ string, _ string, _ []persistence.StateFilter, _ string, _ int, _ int64) ([]string, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, nil
}
func (s sqlShapedState) StateHistory(context.Context, string, string, string, int, int) ([]json.RawMessage, error) {
	return nil, nil
}
func (s sqlShapedState) StateChanges(context.Context, string, string, string, string, int) ([][]byte, error) {
	return nil, nil
}
func (s sqlShapedState) StateValue(context.Context, string, string, string, int64) (json.RawMessage, error) {
	return nil, nil
}
func (s sqlShapedState) StateValues(context.Context, string, string, int64) (map[string]json.RawMessage, error) {
	return nil, nil
}
func (s sqlShapedState) BeginStateRead(context.Context, string, int64) (persistence.StateReadView, error) {
	s.reads.opened++
	return sqlShapedView{reads: s.reads}, nil
}

type sqlShapedView struct{ reads *sqlShapedReads }

func (sqlShapedView) Checkpoint(context.Context, string) (persistence.StateDigest, error) {
	return persistence.StateDigest{}, nil
}
func (sqlShapedView) Position() int64 { return 0 }
func (sqlShapedView) Close() error    { return nil }
func (v sqlShapedView) StateCurrent(_ context.Context, _, typ, id string) (persistence.StateCurrent, error) {
	v.reads.current++
	v.reads.singles++
	return v.reads.rows[typ][id], nil
}
func (v sqlShapedView) StateCurrents(_ context.Context, _, typ string, ids []string) (map[string]persistence.StateCurrent, error) {
	v.reads.current += len(ids)
	v.reads.batches = append(v.reads.batches, slices.Clone(ids))
	out := map[string]persistence.StateCurrent{}
	for _, id := range ids {
		if row, found := v.reads.rows[typ][id]; found {
			out[id] = row
		}
	}
	return out, nil
}
func (v sqlShapedView) StateWindow(_ context.Context, _, _ string, window persistence.StateWindow) (persistence.StateWindowResult, error) {
	v.reads.windows = append(v.reads.windows, window)
	return v.reads.window, nil
}
func (sqlShapedView) StateRecord(context.Context, string, string, string) (json.RawMessage, error) {
	return nil, nil
}
func (v sqlShapedView) StateCandidates(ctx context.Context, _, typ string, filters []persistence.StateFilter, after string, limit int, _ int64) ([]string, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	v.reads.candidates = append(v.reads.candidates, typ)
	v.reads.filters = append(v.reads.filters, filters)
	if rows, configured := v.reads.rows[typ]; configured {
		var ids []string
		for _, id := range slices.Sorted(maps.Keys(rows)) {
			if id > after {
				ids = append(ids, id)
				if len(ids) == limit {
					break
				}
			}
		}
		return ids, nil
	}
	if len(filters) == 0 {
		return v.reads.allIDs, nil
	}
	return nil, nil
}
func (sqlShapedView) StateHistory(context.Context, string, string, string, int, int) ([]json.RawMessage, error) {
	return nil, nil
}
func (sqlShapedView) StateChanges(context.Context, string, string, string, string, int) ([][]byte, error) {
	return nil, nil
}
func (sqlShapedView) StateValue(context.Context, string, string, string, int64) (json.RawMessage, error) {
	return nil, nil
}
func (sqlShapedView) StateValues(context.Context, string, string, int64) (map[string]json.RawMessage, error) {
	return nil, nil
}
func (sqlShapedView) StateUnique(context.Context, string, string, string, string, int64) ([]string, error) {
	return nil, nil
}

func withSQLShapedReads(tb testing.TB, live *invariantTenant, reads *sqlShapedReads) {
	tb.Helper()
	live.Projection.Current().Configure(sqlShapedState{reads: reads}, live.ID, 0, func() int64 { return 0 })
	live.enableCurrentReadScopes()
	live.Projection.UseCurrentRows()
}

func TestInvariantBuilderWorkingDraftsAuthorizeInsideTheReadScope(t *testing.T) {
	live := newInvariantTenant(t, "invariant-working-drafts-read-scope")
	withSQLShapedReads(t, live, &sqlShapedReads{})
	state, err := live.WorkingDrafts(live.member(t))
	if err != nil {
		t.Fatalf("the builder's working drafts were refused: %v", err)
	}
	if state.Drafts == nil {
		t.Fatal("the working drafts answer is not a list")
	}
}

func TestInvariantAuthorizationOutsideTheReadScopeReadsTheResourceDirectory(t *testing.T) {
	live := newInvariantTenant(t, "invariant-authorize-read-scope")
	allIDs := make([]string, 500)
	for i := range allIDs {
		allIDs[i] = fmt.Sprintf("resource-%03d", i)
	}
	reads := &sqlShapedReads{allIDs: allIDs}
	withSQLShapedReads(t, live, reads)
	c := platform.NewCaller(runtime{t: live.Tenant}, live.member(t), build.ID, false, false)
	if decision := live.authorizeNative(c, "GET /v1/releases/drafts/working", "read", []string{build.Builder}, true); !decision.Allow {
		t.Fatalf("a builder was refused an authorized read: %s", decision.Reason)
	}
	if reads.opened == 0 {
		t.Fatal("the authorization did not open the read scope")
	}
	if !slices.Contains(reads.candidates, build.ResourceType) {
		t.Fatalf("the authorization did not read %s inside the read scope; read %v", build.ResourceType, reads.candidates)
	}
	for i, typ := range reads.candidates {
		if typ == build.ResourceType && len(reads.filters[i]) == 0 {
			t.Fatal("the authorization scanned the entire resource directory instead of asking for retired candidates")
		}
	}
	if reads.current != 0 {
		t.Fatalf("the authorization read %d resource rows without a matching retired candidate", reads.current)
	}
}

func TestInvariantRecordAuthorizationOutsideTheReadScopeReadsTheRecord(t *testing.T) {
	live := newInvariantTenant(t, "invariant-record-authorize-read-scope")
	reads := &sqlShapedReads{}
	withSQLShapedReads(t, live, reads)
	member := live.member(t)
	c := platform.NewCaller(runtime{t: live.Tenant}, member, build.ID, false, false)
	live.authorizeFrom(live.Projection, c, platform.AuthzRequest{Principal: member.ID, Tenant: live.ID, Kind: "action", Action: "build.app.create", Type: protocol.AppType, Record: "workspace"}, invariantClock, false)
	if reads.opened == 0 {
		t.Fatal("the record authorization did not open the read scope")
	}
}

func TestInvariantTeamScopeConsistentAcrossReadsAndAssignment(t *testing.T) {
	seed := platform.OrgSeed{
		Units:       []platform.Unit{{ID: "TEAM-A", Name: "Team A", Kind: "team"}, {ID: "TEAM-B", Name: "Team B", Kind: "team"}},
		Memberships: []platform.Membership{{Party: "member:rep-a1", Unit: "TEAM-A", Role: "member"}, {Party: "member:rep-a2", Unit: "TEAM-A", Role: "member"}, {Party: "member:rep-b1", Unit: "TEAM-B", Role: "member"}},
	}
	member := func(id, role string) platform.Member {
		return platform.Member{ID: id, Tenant: "invariant-team-scope", Roles: map[string]string{"invariantproject": role}}
	}
	repA1, repA2, repB1, mgr := member("rep-a1", "salesrep"), member("rep-a2", "salesrep"), member("rep-b1", "salesrep"), member("mgr-1", "salesmanager")
	var seats []protocol.Seat
	for _, m := range []platform.Member{repA1, repA2, repB1, mgr} {
		seats = append(seats, protocol.Seat{Subjects: []string{m.ID}, Member: m})
	}
	as := func(m platform.Member, request *pb.Submission) *pb.Submission {
		request.PrincipalId = m.ID
		return request
	}
	live := newInvariantTenantWithApps(t, "invariant-team-scope", []platform.Service{enterprisemodel.New("invariant-team-scope", actions.NewAuthority, seed)}, seats...)
	lead := map[string]any{
		"name": "caseitem", "project": invariantProject, "title": "Case", "plural": "Cases", "display": "title",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "owner", "title": "Owner", "type": "member", "default": "member"},
		},
		"access": []map[string]any{
			{"role": "salesrep", "read": "team", "create": true, "edit": true},
			{"role": "salesmanager", "read": "all", "create": true, "edit": true, "archive": true},
		},
		"scope": map[string]any{"owner": "owner"},
	}
	note := map[string]any{
		"name": "casenote", "project": invariantProject, "title": "Case note", "plural": "Case notes", "display": "title",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "subject", "title": "Subject", "type": "reference", "ref": "invariantproject.caseitem"},
		},
		"access": []map[string]any{
			{"role": "salesrep", "read": "all", "create": true, "edit": true},
			{"role": "salesmanager", "read": "all", "create": true, "edit": true},
		},
	}
	live.accept(t, "build.object.create", build.ObjectType, "CASE", lead)
	live.accept(t, "build.object.publish", build.ObjectType, "CASE", map[string]any{})
	live.accept(t, "build.object.create", build.ObjectType, "NOTE", note)
	live.accept(t, "build.object.publish", build.ObjectType, "NOTE", map[string]any{})

	if _, err := live.Submit(repA1, as(repA1, live.submission("invariantproject.caseitem.create", "invariantproject.caseitem", "C1", map[string]any{"title": "Alpha"})), invariantClock); err != nil {
		t.Fatalf("rep-a1 create with default owner: %v", err)
	}
	if _, err := live.Submit(repB1, as(repB1, live.submission("invariantproject.caseitem.create", "invariantproject.caseitem", "C2", map[string]any{"title": "Beta"})), invariantClock); err != nil {
		t.Fatalf("rep-b1 create with default owner: %v", err)
	}
	live.accept(t, "invariantproject.casenote.create", "invariantproject.casenote", "N1", map[string]any{"title": "Note A", "subject": "C1"})
	live.accept(t, "invariantproject.casenote.create", "invariantproject.casenote", "N2", map[string]any{"title": "Note B", "subject": "C2"})

	if _, err := live.RecordOf(t.Context(), repA2, "invariantproject.caseitem", "C1", invariantClock); err != nil {
		t.Fatalf("teammate rep-a2 could not read C1: %v", err)
	}
	if _, err := live.RecordOf(t.Context(), repA2, "invariantproject.caseitem", "C2", invariantClock); err == nil {
		t.Fatal("rep-a2 read out-of-team record C2")
	}
	pageA2, err := live.Records(t.Context(), repA2, "invariantproject.caseitem", platform.Query{Limit: 10}, invariantClock)
	if err != nil || pageA2.Total != 1 || len(pageA2.Records) != 1 {
		t.Fatalf("rep-a2 list/count mismatch: total=%d len=%d err=%v", pageA2.Total, len(pageA2.Records), err)
	}
	noteViewA2, err := live.RecordOf(t.Context(), repA2, "invariantproject.casenote", "N2", invariantClock)
	if err != nil {
		t.Fatalf("rep-a2 read note N2: %v", err)
	}
	if noteViewA2.Refs["subject"]["C2"] != "" {
		t.Fatalf("rep-a2 resolved out-of-team reference label for C2: %v", noteViewA2.Refs)
	}
	pageMgr, err := live.Records(t.Context(), mgr, "invariantproject.caseitem", platform.Query{Limit: 10}, invariantClock)
	if err != nil || pageMgr.Total != 2 {
		t.Fatalf("manager list/count mismatch: total=%d err=%v", pageMgr.Total, err)
	}

	if _, err := live.Submit(mgr, as(mgr, live.submission("invariantproject.caseitem.edit", "invariantproject.caseitem", "C2", map[string]any{"owner": "rep-a1"})), invariantClock); err != nil {
		t.Fatalf("manager assign C2 to rep-a1: %v", err)
	}
	if _, err := live.RecordOf(t.Context(), repA2, "invariantproject.caseitem", "C2", invariantClock); err != nil {
		t.Fatalf("rep-a2 could not read C2 immediately after assignment to teammate rep-a1: %v", err)
	}
	if _, err := live.RecordOf(t.Context(), repB1, "invariantproject.caseitem", "C2", invariantClock); err == nil {
		t.Fatal("rep-b1 still read C2 after reassignment out of team")
	}
}

func TestInvariantAggregateGroupsByMemberAndNarrowsDerivedFields(t *testing.T) {
	reader := platform.Member{ID: "rep-1", Tenant: "invariant-aggregate-narrow", Roles: map[string]string{"invariantproject": "salesrep"}}
	mgr := platform.Member{ID: "mgr-1", Tenant: "invariant-aggregate-narrow", Roles: map[string]string{"invariantproject": "salesmanager"}}
	live := newInvariantTenant(t, "invariant-aggregate-narrow",
		protocol.Seat{Subjects: []string{reader.ID}, Member: reader},
		protocol.Seat{Subjects: []string{mgr.ID}, Member: mgr},
	)
	const dealType, lineType = "invariantproject.quotedeal", "invariantproject.quoteline"
	deal := map[string]any{
		"name": "quotedeal", "project": invariantProject, "title": "Quote deal", "plural": "Quote deals", "display": "title",
		"sets":  []map[string]any{{"name": "lines", "object": lineType, "direction": "inverse", "from": "lines", "limit": 200, "order": []map[string]any{{"expr": map[string]any{"op": "", "kind": "text", "ref": "id"}}}}},
		"reads": map[string]any{"collections": map[string]any{"lines": map[string]any{"app": build.ID, "object": lineType, "policy": "complete", "set": "lines"}}},
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true},
			{"name": "owner", "title": "Owner", "type": "member"},
			{"name": "amount", "title": "Amount", "type": "integer", "required": true},
			{"name": "weighted", "title": "Weighted", "type": "integer", "formula": "amount * 2"},
			{"name": "secretscore", "title": "Secret score", "type": "integer", "formulaExpr": map[string]any{"op": "sum", "kind": "integer", "agg": map[string]any{"source": "lines", "field": []string{"score"}}}},
		},
		"access": []map[string]any{
			{"role": "salesrep", "read": "all"},
			{"role": "salesmanager", "read": "all", "create": true, "edit": true},
		},
	}
	line := map[string]any{
		"name": "quoteline", "project": invariantProject, "title": "Quote line", "plural": "Quote lines", "display": "title",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true},
			{"name": "deal", "title": "Deal", "type": "reference", "ref": dealType, "inverse": "lines", "required": true},
			{"name": "score", "title": "Score", "type": "integer", "required": true},
		},
		"access": []map[string]any{
			{"role": "salesrep", "read": "none"},
			{"role": "salesmanager", "read": "all", "create": true, "edit": true},
		},
	}
	live.accept(t, "build.object.create", build.ObjectType, "QLINE", line)
	live.accept(t, "build.object.create", build.ObjectType, "QDEAL", deal)
	live.accept(t, "build.object.publish", build.ObjectType, "QLINE", map[string]any{})
	live.accept(t, "build.object.publish", build.ObjectType, "QDEAL", map[string]any{})
	live.accept(t, dealType+".create", dealType, "D1", map[string]any{"title": "Alpha", "owner": "rep-1", "amount": 10})
	live.accept(t, dealType+".create", dealType, "D2", map[string]any{"title": "Beta", "owner": "mgr-1", "amount": 20})
	live.accept(t, lineType+".create", lineType, "L1", map[string]any{"title": "Line 1", "deal": "D1", "score": 70})
	live.accept(t, lineType+".create", lineType, "L2", map[string]any{"title": "Line 2", "deal": "D2", "score": 140})

	byOwner, err := live.AggregateObjectSet(reader, objects.ObjectSetRequest{
		Set:      platform.ObjectSet{Object: dealType},
		Groups:   []string{"owner"},
		Measures: []string{"sum:weighted"},
	}, invariantClock)
	if err != nil || len(byOwner.Rows) != 2 || byOwner.Rows[0]["owner"] != "mgr-1" || fmt.Sprint(byOwner.Rows[0]["sum:weighted"]) != "40" {
		t.Fatalf("member grouping and readable derived sum failed: rows=%+v err=%v", byOwner.Rows, err)
	}

	mgrSecret, err := live.AggregateObjectSet(mgr, objects.ObjectSetRequest{
		Set:      platform.ObjectSet{Object: dealType},
		Measures: []string{"sum:secretscore"},
	}, invariantClock)
	if err != nil || len(mgrSecret.Rows) != 1 || fmt.Sprint(mgrSecret.Rows[0]["sum:secretscore"]) != "210" {
		t.Fatalf("manager secret score sum mismatch: rows=%+v err=%v", mgrSecret.Rows, err)
	}

	repSecret, err := live.AggregateObjectSet(reader, objects.ObjectSetRequest{
		Set:      platform.ObjectSet{Object: dealType},
		Measures: []string{"sum:secretscore"},
	}, invariantClock)
	if err != nil || len(repSecret.Rows) != 1 || fmt.Sprint(repSecret.Rows[0]["sum:secretscore"]) != "0" {
		t.Fatalf("salesrep leaked unreadable derived source in aggregate: rows=%+v err=%v", repSecret.Rows, err)
	}

	if _, probeErr := live.AggregateObjectSet(reader, objects.ObjectSetRequest{
		Set:      platform.ObjectSet{Object: dealType, Domain: platform.Raw([][]any{{"secretscore", "=", 70}})},
		Measures: []string{"count"},
	}, invariantClock); probeErr == nil || probeErr.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Fatalf("expected domain filter on narrowable derived field to be rejected, got %v", probeErr)
	}
}

func TestInvariantReadScopeDetachesCanceledContextBeforeSweep(t *testing.T) {
	live := newInvariantTenant(t, "invariant-read-scope-detach")
	admin := live.member(t)
	reads := &sqlShapedReads{rows: map[string]map[string]persistence.StateCurrent{}}
	withSQLShapedReads(t, live, reads)

	reqCtx, cancel := context.WithCancel(context.Background())
	reqView, openErr := live.Projection.Current().Open(reqCtx)
	if openErr != nil || reqView == nil {
		t.Fatalf("open read scope failed: view=%v err=%v", reqView, openErr)
	}
	_ = live.Projection.Current().Detach()
	if _, refusal := live.Records(reqCtx, admin, build.ObjectType, platform.Query{Limit: 10}, invariantClock); refusal != nil {
		t.Fatalf("initial scoped read failed: %v", refusal)
	}
	cancel()

	if err := live.Projection.Current().Context().Err(); err != nil {
		t.Fatalf("detached read scope retained canceled request context: %v", err)
	}
	live.SweepDelegations(invariantClock)
}
