package host

import (
	"context"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/protocol"
	"platformserver/services/actions"

	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/objects"
)

func (t *Tenant) checkBusinessCorrection(m platform.Member, sub *pb.Submission, binding *platform.SchemaVersion) *platform.Error {
	return actions.CheckCorrection(m.ID, sub, binding, t.Committed.Refusal)
}

func (t *Tenant) BusinessRefusals(m platform.Member, typ, id string, offset, limit int, at time.Time, keys ...string) (protocol.BusinessRefusalPage, *platform.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (protocol.BusinessRefusalPage, *platform.Error) {
		return t.scopedBusinessRefusals(m, typ, id, offset, limit, at, keys...)
	})
}

func (t *Tenant) scopedBusinessRefusals(m platform.Member, typ, id string, offset, limit int, at time.Time, keys ...string) (protocol.BusinessRefusalPage, *platform.Error) {
	if err := t.Admits(m); err != nil {
		return protocol.BusinessRefusalPage{Records: []protocol.BusinessRefusalEntry{}}, err
	}
	if typ == "" || id == "" {
		return protocol.BusinessRefusalPage{Records: []protocol.BusinessRefusalEntry{}}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose the original business record")
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
