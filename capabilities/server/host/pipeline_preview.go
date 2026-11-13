package host

import (
	"context"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (t *Tenant) PreviewPipeline(member platform.Member, request dataowner.PipelinePreviewRequest, at time.Time) (dataowner.PipelinePreview, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (dataowner.PipelinePreview, *kernel.Error) {
		return t.scopedPreviewPipeline(member, request, at)
	})
}

func (t *Tenant) scopedPreviewPipeline(member platform.Member, request dataowner.PipelinePreviewRequest, at time.Time) (dataowner.PipelinePreview, *kernel.Error) {
	out := dataowner.PipelinePreview{Rows: []map[string]any{}, Quarantine: []platform.QuarantinedRow{}}
	if problem := t.Admits(member); problem != nil {
		return out, problem
	}
	if t.quarantined() || t.App(build.ID) == nil {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Pipeline trial is unavailable")
	}
	caller := t.caller(member, t.App(build.ID), false)
	authorize := func(sources []string) *kernel.Error {
		return authorizationRefusal(t.authorizeCapability(caller, build.ID, "build.pipeline.preview", []string{build.Builder, build.Integrator}, true, authorizationDetails{At: at, Sources: sources}))
	}
	if problem := authorize(nil); problem != nil {
		return out, problem
	}
	return t.DataOwner().PreviewPipeline(caller, request, at, t.services.Rules, authorize)
}
