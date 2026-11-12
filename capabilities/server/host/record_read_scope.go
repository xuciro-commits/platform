package host

import runtimekernel "platformserver/kernel"

import (
	"context"
	"errors"
	"fmt"
	"platformserver/kernel/records"
	automation "platformserver/services/automate/runtime"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

type recordReadScope struct {
	ctx context.Context
	err error
}

func (t *Tenant) readContext() context.Context {
	if t.readScope != nil {
		return t.readScope.ctx
	}
	return context.Background()
}

func (t *Tenant) readError() error {
	if t.readScope != nil {
		return t.readScope.err
	}
	return nil
}

func (t *Tenant) scopeSource() *Tenant {
	return t.readOrigin
}

func (t *Tenant) beginRecordRead(ctx context.Context) {
	t.readScope = &recordReadScope{ctx: ctx}
	t.openRecordRead(ctx)
}

func (t *Tenant) enableCurrentReadScopes() {
	t.currentReadScopes = true
	t.invalidateReadImage()
	t.Lane.BindReadScope(t.beginRecordRead, t.endLane)
}
func (t *Tenant) openRecordRead(ctx context.Context) {
	if !t.currentReadScopes || t.quarantined() {
		return
	}
	view, err := t.Projection.Current().Open(ctx)
	if err != nil {
		t.readScope.err = err
		var mismatch *persistence.StatePositionError
		if errors.As(err, &mismatch) && (t.readOrigin == nil || mismatch.Position < mismatch.Applied) {
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
	if t.scopeSource() == nil && t.Projection.Current().Position() != nil && t.Projection.Current().Position()() != t.Projection.Current().Applied() {
		t.quarantine(fmt.Errorf("SQL commit is at %d before in-memory application at %d", t.Projection.Current().Position()(), t.Projection.Current().Applied()))
	}
	if view := t.Projection.Current().Detach(); view != nil {
		if store, ok := t.Projection.Current().Backend().(runtimekernel.AcceptedStateReader); ok {
			t.installRuntimeHistory(store, t.Projection.Current().Position())
		}
		if err := view.Close(); err != nil && t.readError() == nil {
			t.readScope.err = err
		}
	}
}

type laneHolderKey struct{}

func withLaneHolder(ctx context.Context, holder string) context.Context {
	return context.WithValue(ctx, laneHolderKey{}, holder)
}

func laneHolderOf(ctx context.Context, fallback string) string {
	if holder, ok := ctx.Value(laneHolderKey{}).(string); ok && holder != "" {
		return holder
	}
	return fallback
}

func (t *Tenant) recordReadRefusal() *kernel.Error {
	if t.readError() != nil || t.quarantined() {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return nil
}
func recordRead[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	if ctx.Err() != nil {
		return value, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The read was cancelled while waiting for the tenant")
	}
	if t.scopeSource() != nil {
		return read(t)
	}
	return coordinatedRecordRead(t, ctx, read)
}
func coordinatedRecordRead[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	if t.scopeSource() != nil {
		return read(t)
	}
	return readFromImage(t, ctx, read)
}

func readFromLane[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	return lockedRecordRead(t, ctx, func(t *Tenant) (value T, refusal *kernel.Error) {
		view, err := t.forkReadState(ctx)
		if err != nil {
			return value, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The read scope could not be prepared: {reason}", err.Error())
		}
		view.openRecordRead(ctx)
		defer func() {
			view.closeRecordRead()
			if view.readError() != nil {
				var zero T
				value, refusal = zero, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
		}()
		if refusal := view.recordReadRefusal(); refusal != nil {
			return value, refusal
		}
		return read(view)
	})
}

func lockedRecordRead[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	if err := t.Lane.Hold(ctx, laneHolderOf(ctx, "read")); err != nil {
		return value, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The read was cancelled while waiting for the tenant")
	}
	defer t.Lane.Unlock()
	defer func() {
		if failure := recover(); failure != nil {
			if err, ok := failure.(error); ok {
				t.readScope.err = err
				var mismatch *persistence.StatePositionError
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
	return read(t)
}

func (t *Tenant) Records(ctx context.Context, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (RecordPage, *kernel.Error) { return t.recordsRead(m, typ, q, now) })
}
func (t *Tenant) RecordOf(ctx context.Context, m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (RecordView, *kernel.Error) { return t.recordOf(m, typ, id, now) })
}

func (t *Tenant) ChainOf(ctx context.Context, m platform.Member, ref string, now time.Time) (automation.Chain, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (automation.Chain, *kernel.Error) { return t.chainRead(m, ref, now) })
}

func (t *Tenant) Context(ctx context.Context, reader *platform.Member, typ, id string, now time.Time) (ContextView, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (ContextView, *kernel.Error) { return t.contextRead(reader, typ, id, now) })
}

func (t *Tenant) Export(ctx context.Context, m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) ([]byte, *kernel.Error) { return t.exportRead(m, typ, q, now) })
}

func (t *Tenant) EnterpriseReferences(ctx context.Context, m platform.Member, element string, offset, limit int, now time.Time) ([]EnterpriseReferenceGroup, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) ([]EnterpriseReferenceGroup, *kernel.Error) {
		return t.enterpriseReferencesRead(m, element, offset, limit, now)
	})
}

func (t *Tenant) EnterpriseUsage(ctx context.Context, m platform.Member, id, until string, now time.Time) (EnterpriseUsage, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (EnterpriseUsage, *kernel.Error) { return t.enterpriseUsageRead(m, id, until, now) })
}

func (t *Tenant) Search(ctx context.Context, reader *platform.Member, q string, now time.Time) []Hit {
	out, _ := recordRead(t, ctx, func(t *Tenant) ([]Hit, *kernel.Error) { return t.searchRead(reader, q, now), nil })
	return out
}
func (t *Tenant) Readable(ctx context.Context, m platform.Member, ref string, now time.Time) bool {
	out, _ := recordRead(t, ctx, func(t *Tenant) (bool, *kernel.Error) { return t.readable(m, ref, now), nil })
	return out
}

func (t *Tenant) readRecords(ctx context.Context, read func(*records.Store)) error {
	_, refusal := recordRead(t, ctx, func(t *Tenant) (struct{}, *kernel.Error) {
		read(t.Projection)
		return struct{}{}, nil
	})
	if refusal != nil {
		return refusal
	}
	return nil
}
