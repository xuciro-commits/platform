package platformserver

import runtimekernel "platformserver/kernel"

import records "platformserver/kernel/records"

import (
	"context"
	"errors"
	"fmt"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/journal"
	"platformserver/platform"
)

func (t *Tenant) beginRecordRead(ctx context.Context) {
	t.recordReadError = nil
	t.openRecordRead(ctx)
	if t.recordReadError == nil && !t.quarantined() {
		t.recordReadError = t.applySystemDefinitionProjection()
	}
}

func (t *Tenant) enableCurrentReadScopes() {
	t.currentReadScopes = true
	t.mu.BindReadScope(t.beginRecordRead, t.closeRecordRead)
}
func (t *Tenant) openRecordRead(ctx context.Context) {
	if !t.currentReadScopes || t.quarantined() {
		return
	}
	t.recordReadError = nil
	view, err := t.records.Current().Open(ctx)
	if err != nil {
		t.recordReadError = err
		var mismatch *journal.StatePositionError
		if errors.As(err, &mismatch) {
			t.quarantine(err)
		}
		return
	}
	if view != nil {
		t.installRuntimeHistory(view, view.Position)
	}
}
func (t *Tenant) closeRecordRead() {
	if !t.currentReadScopes {
		return
	}
	if t.records.Current().Position() != nil && t.records.Current().Position()() != t.records.Current().Applied() {
		t.quarantine(fmt.Errorf("SQL commit is at %d before in-memory application at %d", t.records.Current().Position()(), t.records.Current().Applied()))
	}
	if view := t.records.Current().Detach(); view != nil {
		if store, ok := t.records.Current().Backend().(runtimekernel.AcceptedStateReader); ok {
			t.installRuntimeHistory(store, t.records.Current().Position())
		}
		if err := view.Close(); err != nil && t.recordReadError == nil {
			t.recordReadError = err
		}
	}
}
func (t *Tenant) recordReadRefusal() *kernel.Error {
	if t.recordReadError != nil || t.quarantined() {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return nil
}
func recordRead[T any](t *Tenant, ctx context.Context, read func() (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	if !t.currentReadScopes && t.systemDefinitionState.source.Load() == nil {
		return read()
	}
	return coordinatedRecordRead(t, ctx, read)
}
func coordinatedRecordRead[T any](t *Tenant, ctx context.Context, read func() (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	t.mu.LockContext(ctx)
	defer t.mu.Unlock()
	defer func() {
		if failure := recover(); failure != nil {
			if err, ok := failure.(error); ok {
				t.recordReadError = err
				var mismatch *journal.StatePositionError
				if errors.As(err, &mismatch) {
					t.quarantine(err)
				}
				var zero T
				value = zero
				refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
				return
			}
			panic(failure)
		}
	}()
	if err := t.recordReadRefusal(); err != nil {
		return value, err
	}
	return read()
}

func (t *Tenant) Records(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return recordRead(t, context.Background(), func() (RecordPage, *kernel.Error) { return t.recordsRead(m, typ, q, now) })
}
func (t *Tenant) RecordOf(m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	return recordRead(t, context.Background(), func() (RecordView, *kernel.Error) { return t.recordOf(m, typ, id, now) })
}
func (t *Tenant) Held(ref string) (value any, ok bool) {
	_, _ = recordRead(t, context.Background(), func() (struct{}, *kernel.Error) { value, ok = t.held(ref); return struct{}{}, nil })
	return value, ok
}

func (t *Tenant) Aggregate(m platform.Member, typ string, q records.AggregateQuery, now time.Time) (records.Aggregate, *kernel.Error) {
	return recordRead(t, context.Background(), func() (records.Aggregate, *kernel.Error) { return t.aggregateRead(m, typ, q, now) })
}

func (t *Tenant) ChainOf(m platform.Member, ref string, now time.Time) (Chain, *kernel.Error) {
	return recordRead(t, context.Background(), func() (Chain, *kernel.Error) { return t.chainRead(m, ref, now) })
}

func (t *Tenant) Context(reader *platform.Member, typ, id string, now time.Time) (ContextView, *kernel.Error) {
	return recordRead(t, context.Background(), func() (ContextView, *kernel.Error) { return t.contextRead(reader, typ, id, now) })
}

func (t *Tenant) RunQuery(m platform.Member, app, name, of string, now time.Time) (RecordPage, *kernel.Error) {
	return recordRead(t, context.Background(), func() (RecordPage, *kernel.Error) { return t.queryRead(m, app, name, of, now) })
}

func (t *Tenant) RunQueryWindow(m platform.Member, app, name, of, version string, window platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return recordRead(t, context.Background(), func() (RecordPage, *kernel.Error) { return t.queryWindowRead(m, app, name, of, version, window, now) })
}

func (t *Tenant) Export(m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	return recordRead(t, context.Background(), func() ([]byte, *kernel.Error) { return t.exportRead(m, typ, q, now) })
}

func (t *Tenant) EnterpriseReferences(m platform.Member, element string, offset, limit int, now time.Time) ([]EnterpriseReferenceGroup, *kernel.Error) {
	return recordRead(t, context.Background(), func() ([]EnterpriseReferenceGroup, *kernel.Error) {
		return t.enterpriseReferencesRead(m, element, offset, limit, now)
	})
}

func (t *Tenant) EnterpriseUsage(m platform.Member, id, until string, now time.Time) (EnterpriseUsage, *kernel.Error) {
	return recordRead(t, context.Background(), func() (EnterpriseUsage, *kernel.Error) { return t.enterpriseUsageRead(m, id, until, now) })
}

func (t *Tenant) InterfaceRecords(m platform.Member, name string, query platform.Query, now time.Time) (InterfaceRecordPage, *kernel.Error) {
	return recordRead(t, context.Background(), func() (InterfaceRecordPage, *kernel.Error) { return t.interfaceRecordsRead(m, name, query, now) })
}

func (t *Tenant) Search(reader *platform.Member, q string, now time.Time) []Hit {
	out, _ := recordRead(t, context.Background(), func() ([]Hit, *kernel.Error) { return t.searchRead(reader, q, now), nil })
	return out
}
func (t *Tenant) Readable(m platform.Member, ref string, now time.Time) bool {
	out, _ := recordRead(t, context.Background(), func() (bool, *kernel.Error) { return t.readable(m, ref, now), nil })
	return out
}

func (t *Tenant) TraverseLink(m platform.Member, binding platform.AssetBinding, direction, id string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return recordRead(t, context.Background(), func() (RecordPage, *kernel.Error) { return t.traverseLinkRead(m, binding, direction, id, q, now) })
}
