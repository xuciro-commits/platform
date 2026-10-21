package platformserver

import (
	"context"
	"errors"
	"fmt"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/journal"
	"platformserver/platform"
)

// tenantLane is the one lane through which a tenant's read snapshots and
// writes pass. It is held for as long as an operation owns the SQL snapshot.
// A read issued from inside that window - an acceptance callback going through
// the public entry point - must observe the position the lane already
// established instead of waiting for the lock it holds itself, so the lane
// records its owning goroutine and lets that goroutine re-enter it. Nested
// reads never open or close the snapshot: pre-commit visibility is the
// contract the acceptance callback is checking.
type tenantLane struct {
	lock   sync.Mutex
	owner  atomic.Uint64
	tenant *Tenant
}

func (l *tenantLane) Lock() { l.lockContext(context.Background()) }

func (l *tenantLane) lockContext(ctx context.Context) {
	l.lock.Lock()
	l.owner.Store(goroutineID())
	if l.tenant != nil {
		l.tenant.openRecordRead(ctx)
	}
}

// lockRead acquires the lane for one read and returns its release. A goroutine
// that already holds the lane keeps the open snapshot and gets a no-op release.
func (l *tenantLane) lockRead(ctx context.Context) func() {
	if !l.lock.TryLock() {
		if l.owner.Load() == goroutineID() {
			return func() {}
		}
		l.lock.Lock()
		l.owner.Store(goroutineID())
		if l.tenant != nil {
			l.tenant.openRecordRead(ctx)
		}
		return l.Unlock
	}
	l.owner.Store(goroutineID())
	if l.tenant != nil {
		l.tenant.openRecordRead(ctx)
	}
	return l.Unlock
}

func (l *tenantLane) Unlock() {
	l.owner.Store(0)
	if l.tenant != nil {
		l.tenant.closeRecordRead()
	}
	l.lock.Unlock()
}

// goroutineID identifies the lane's re-entrant caller. Reads take the
// uncontended fast path without calling it.
func goroutineID() uint64 {
	var buf [64]byte
	n := goruntime.Stack(buf[:], false)
	text := strings.TrimPrefix(string(buf[:n]), "goroutine ")
	if space := strings.IndexByte(text, ' '); space > 0 {
		if id, err := strconv.ParseUint(text[:space], 10, 64); err == nil {
			return id
		}
	}
	return 0
}

func (t *Tenant) enableCurrentReadScopes() {
	t.currentReadScopes = true
	t.mu.tenant = t
}
func (t *Tenant) openRecordRead(ctx context.Context) {
	if !t.currentReadScopes || t.quarantined() {
		return
	}
	t.recordReadError = nil
	store := t.records
	views, ok := store.historyStore.(journal.StateReadViews)
	if !ok {
		return
	}
	view, err := views.BeginStateRead(ctx, t.ID, store.appliedPosition)
	if err != nil {
		t.recordReadError = err
		var mismatch *journal.StatePositionError
		if errors.As(err, &mismatch) {
			t.quarantine(err)
		}
		return
	}
	store.bindReadView(view)
	t.installRuntimeHistory(view, view.Position)
}
func (t *Tenant) closeRecordRead() {
	if !t.currentReadScopes {
		return
	}
	if t.records.currentPosition != nil && t.records.currentPosition() != t.records.appliedPosition {
		t.quarantine(fmt.Errorf("SQL commit is at %d before in-memory application at %d", t.records.currentPosition(), t.records.appliedPosition))
	}
	if view := t.records.readView; view != nil {
		t.records.bindReadView(nil)
		if store, ok := t.records.historyStore.(acceptedStateReader); ok {
			t.installRuntimeHistory(store, t.records.currentPosition)
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
	if !t.currentReadScopes {
		return read()
	}
	return coordinatedRecordRead(t, ctx, read)
}
func coordinatedRecordRead[T any](t *Tenant, ctx context.Context, read func() (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	release := t.mu.lockRead(ctx)
	defer release()
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

func (t *Tenant) Aggregate(m platform.Member, typ string, q AggregateQuery, now time.Time) (Aggregate, *kernel.Error) {
	return recordRead(t, context.Background(), func() (Aggregate, *kernel.Error) { return t.aggregateRead(m, typ, q, now) })
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
