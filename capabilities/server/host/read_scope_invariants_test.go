package host

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type sqlShapedReads struct {
	opened     int
	candidates []string
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
func (sqlShapedView) StateCurrent(context.Context, string, string, string) (persistence.StateCurrent, error) {
	return persistence.StateCurrent{}, nil
}
func (sqlShapedView) StateRecord(context.Context, string, string, string) (json.RawMessage, error) {
	return nil, nil
}
func (v sqlShapedView) StateCandidates(_ context.Context, _, typ string, _ []persistence.StateFilter, _ string, _ int, _ int64) ([]string, error) {
	v.reads.candidates = append(v.reads.candidates, typ)
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
	live.records.Current().Configure(sqlShapedState{reads: reads}, live.ID, 0, func() int64 { return 0 })
	live.enableCurrentReadScopes()
	live.records.UseCurrentRows()
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
	reads := &sqlShapedReads{}
	withSQLShapedReads(t, live, reads)
	c := platform.NewCaller(runtime{live.Tenant}, live.member(t), build.ID, false, false)
	if decision := live.authorizeNative(c, "GET /v1/releases/drafts/working", "read", []string{build.Builder}, true); !decision.Allow {
		t.Fatalf("a builder was refused an authorized read: %s", decision.Reason)
	}
	if reads.opened == 0 {
		t.Fatal("the authorization did not open the read scope")
	}
	if !slices.Contains(reads.candidates, build.ResourceType) {
		t.Fatalf("the authorization did not read %s inside the read scope; read %v", build.ResourceType, reads.candidates)
	}
}

func TestInvariantRecordAuthorizationOutsideTheReadScopeReadsTheRecord(t *testing.T) {
	live := newInvariantTenant(t, "invariant-record-authorize-read-scope")
	reads := &sqlShapedReads{}
	withSQLShapedReads(t, live, reads)
	member := live.member(t)
	c := platform.NewCaller(runtime{live.Tenant}, member, build.ID, false, false)
	live.authorizeFrom(live.records, c, platform.AuthzRequest{Principal: member.ID, Tenant: live.ID, Kind: "action", Action: "build.app.create", Type: build.AppType, Record: "workspace"}, invariantClock, false)
	if reads.opened == 0 {
		t.Fatal("the record authorization did not open the read scope")
	}
}
