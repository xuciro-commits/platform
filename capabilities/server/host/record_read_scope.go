package host

import (
	"context"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	"time"
)

import "platformserver/kernel/transport"

import runtimekernel "platformserver/kernel"

type recordReadScope struct {
	ctx         context.Context
	err         error
	definitions *releaseowner.DefinitionPlanCache
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
	if err := t.OpenReadScope(ctx, t.apps, t.readOrigin != nil, t.quarantine); err != nil {
		t.readScope.err = err
	}
}
func (t *Tenant) closeRecordRead() {
	if !t.currentReadScopes {
		return
	}
	if err := t.CloseReadScope(t.apps, t.readOrigin == nil, t.quarantine); err != nil && t.readError() == nil {
		t.readScope.err = err
	}
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
	return coordinatedRecordRead(t, ctx, read)
}
func coordinatedRecordRead[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	if t.readOrigin != nil {
		return read(t)
	}
	return readFromImage(t, ctx, read)
}

func readFromLane[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	return lockedRecordRead(t, ctx, func(t *Tenant) (value T, refusal *kernel.Error) {
		value, refusal, _ = readInView(t, ctx, read)
		return value, refusal
	})
}

func lockedRecordRead[T any](t *Tenant, ctx context.Context, read func(*Tenant) (T, *kernel.Error)) (value T, refusal *kernel.Error) {
	if err := t.Lane.Hold(ctx, transport.LaneHolderOf(ctx, "read")); err != nil {
		return value, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The read was cancelled while waiting for the tenant")
	}
	defer t.Lane.Unlock()
	defer runtimekernel.RecoverLockedRead(func(err error) { t.readScope.err = err }, t.quarantine, &value, &refusal)
	if err := t.recordReadRefusal(); err != nil {
		return value, err
	}
	return read(t)
}

func (t *Tenant) Records(ctx context.Context, m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (RecordPage, *kernel.Error) {
		return t.objectOwner(t.Projection).Records(m, typ, q, now)
	})
}
func (t *Tenant) RecordOf(ctx context.Context, m platform.Member, typ, id string, now time.Time) (objects.RecordView, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (objects.RecordView, *kernel.Error) { return t.recordOf(m, typ, id, now) })
}

func (t *Tenant) Export(ctx context.Context, m platform.Member, typ string, q platform.Query, now time.Time) ([]byte, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) ([]byte, *kernel.Error) { return t.exportRead(m, typ, q, now) })
}

func (t *Tenant) Search(ctx context.Context, reader *platform.Member, q string, now time.Time) []build.Hit {
	out, _ := recordRead(t, ctx, func(t *Tenant) ([]build.Hit, *kernel.Error) { return t.searchRead(reader, q, now), nil })
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
