package host

import records "platformserver/kernel/records"

import (
	"context"
	"encoding/json"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/services/actions"

	"reflect"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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
	f = acceptedformat.CloneBusinessFact(f)
	if f.Record != "" && !t.readable(m, f.Record, at) {
		f.Record = ""
	}
	if f.Route != nil && f.Route.ID != "" && !t.readable(m, f.Route.Type+"/"+f.Route.ID, at) {
		f.Route = nil
	}
	typ, id, _ := strings.Cut(f.Subject, "/")
	var readable bool
	var et *records.Type
	func() {
		t.Projection.Lock()
		defer t.Projection.Unlock()
		et = t.Projection.Type(typ)
		readable = et != nil && et.Rows().Get(id) != nil && t.readableIn(t.Projection, m, f.Subject, at)
	}()
	if !readable {
		f.Original = nil
		f.Inputs = nil
		return f
	}
	var original map[string]json.RawMessage
	if json.Unmarshal(f.Original, &original) != nil {
		f.Original = nil
		return f
	}
	info := et.Info()
	historical := reflect.New(info.Go).Elem()
	if json.Unmarshal(f.Original, historical.Addr().Interface()) != nil {
		f.Original = nil
		return f
	}
	if info.HasReadSources() || len(info.VersionPins) > 0 {
		narrowed, _ := t.narrower(m, at, true).Derive(et, historical)
		var rawBefore, rawAfter map[string]json.RawMessage
		_ = json.Unmarshal(platform.Raw(historical.Interface()), &rawBefore)
		_ = json.Unmarshal(platform.Raw(narrowed.Interface()), &rawAfter)
		for name := range original {
			beforeHash, _ := platform.CanonicalDigest(rawBefore[name])
			afterHash, _ := platform.CanonicalDigest(rawAfter[name])
			if beforeHash != afterHash {
				delete(original, name)
			}
		}
	}
	for name := range original {
		field, ok := info.Field(name)
		if name != "id" && (!ok || !field.ReadsAny(m.RolesIn(info.App))) {
			delete(original, name)
		}
	}
	f.Original = platform.Raw(original)
	if m.ID != f.Member {
		f.Inputs = nil
	}
	return f
}
