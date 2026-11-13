package host

import (
	"context"
	runtimekernel "platformserver/kernel"
	"platformserver/services/actions"

	"reflect"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
)

func (t *Tenant) checkBusinessCorrection(m platform.Member, sub *pb.Submission, binding *platform.SchemaVersion) *kernel.Error {
	key := actions.CorrectionOf(sub)
	if key == "" {
		return nil
	}
	prior, ok := t.Committed.Refusal(sub.GetAuthority() + "/" + key)
	var original pb.Submission
	if !ok || key == sub.GetIdempotencyKey() || protojson.Unmarshal(prior.Submission, &original) != nil || original.GetPrincipalId() != m.ID || original.GetAuthority() != sub.GetAuthority() || original.GetSchema().GetName() != sub.GetSchema().GetName() || original.GetTarget().GetType() != sub.GetTarget().GetType() || original.GetTarget().GetId() != sub.GetTarget().GetId() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A correction needs the same applicant, action and target as its original refused request")
	}
	sameSchema := original.GetSchema().GetVersion() == sub.GetSchema().GetVersion() && reflect.DeepEqual(prior.SchemaVersion, binding)
	if sameSchema && len(actions.ChangedBusinessInputs(original.GetPayload(), sub.GetPayload())) == 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unchanged inputs must repeat the original request key")
	}
	return nil
}

func (t *Tenant) BusinessRefusals(m platform.Member, typ, id string, offset, limit int, at time.Time, keys ...string) (platform.BusinessRefusalPage, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (platform.BusinessRefusalPage, *kernel.Error) {
		return t.scopedBusinessRefusals(m, typ, id, offset, limit, at, keys...)
	})
}

func (t *Tenant) scopedBusinessRefusals(m platform.Member, typ, id string, offset, limit int, at time.Time, keys ...string) (platform.BusinessRefusalPage, *kernel.Error) {
	if err := t.Admits(m); err != nil {
		return platform.BusinessRefusalPage{Records: []platform.BusinessRefusalEntry{}}, err
	}
	if typ == "" || id == "" {
		return platform.BusinessRefusalPage{Records: []platform.BusinessRefusalEntry{}}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the original business record")
	}
	key := ""
	if len(keys) > 0 {
		key = keys[0]
	}
	return t.Committed.BusinessRefusals(runtimekernel.RefusalView{Member: m.ID, Correction: actions.CorrectionOf, Category: actions.RefusalCategory, Changes: actions.ChangedBusinessInputs,
		Readable: func(ref string) bool { return t.readable(m, ref, at) },
		Exists: func() bool {
			t.Projection.Lock()
			defer t.Projection.Unlock()
			et := t.Projection.Type(typ)
			return et != nil && et.Rows().Get(id) != nil
		},
		Issues: func(issues []platform.FieldIssue) []platform.FieldIssue { return t.readableFieldIssues(m, issues, at) },
		Fact: func(f platform.BusinessRefusalFact) platform.BusinessRefusalFact {
			return t.readableBusinessFact(m, f, at)
		},
	}, typ, id, key, offset, limit), nil
}

func (t *Tenant) readableBusinessFact(m platform.Member, f platform.BusinessRefusalFact, at time.Time) platform.BusinessRefusalFact {
	return objects.ReadableBusinessFact(t.Projection, m, f,
		func(ref string) bool { return t.readable(m, ref, at) },
		func(ref string) bool { return t.readableIn(t.Projection, m, ref, at) },
		func() *objects.Narrower { return t.narrower(m, at, true) })
}
