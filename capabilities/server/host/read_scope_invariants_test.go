package host

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"testing"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
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
func (s sqlShapedState) StateCandidates(context.Context, string, string, []persistence.StateFilter, string, int, int64) ([]string, error) {
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
func (v sqlShapedView) StateCandidates(_ context.Context, _, typ string, filters []persistence.StateFilter, after string, limit int, _ int64) ([]string, error) {
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
	live.authorizeFrom(live.Projection, c, platform.AuthzRequest{Principal: member.ID, Tenant: live.ID, Kind: "action", Action: "build.app.create", Type: build.AppType, Record: "workspace"}, invariantClock, false)
	if reads.opened == 0 {
		t.Fatal("the record authorization did not open the read scope")
	}
}
