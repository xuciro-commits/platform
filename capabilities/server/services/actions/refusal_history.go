package actions

import (
	"encoding/json"
	"reflect"

	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

func CheckCorrection(memberID string, sub *pb.Submission, binding *platform.SchemaVersion, refusal func(string) (acceptedformat.Refusal, bool)) *kernel.Error {
	key := CorrectionOf(sub)
	if key == "" {
		return nil
	}
	prior, ok := refusal(sub.GetAuthority() + "/" + key)
	var original pb.Submission
	if !ok || key == sub.GetIdempotencyKey() || protojson.Unmarshal(prior.Submission, &original) != nil || original.GetPrincipalId() != memberID || original.GetAuthority() != sub.GetAuthority() || original.GetSchema().GetName() != sub.GetSchema().GetName() || original.GetTarget().GetType() != sub.GetTarget().GetType() || original.GetTarget().GetId() != sub.GetTarget().GetId() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A correction needs the same applicant, action and target as its original refused request")
	}
	sameSchema := original.GetSchema().GetVersion() == sub.GetSchema().GetVersion() && reflect.DeepEqual(prior.SchemaVersion, binding)
	if sameSchema && len(ChangedBusinessInputs(original.GetPayload(), sub.GetPayload())) == 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unchanged inputs must repeat the original request key")
	}
	return nil
}

func BusinessRefusalEvents(refusals map[string]acceptedformat.Refusal, answers map[string]json.RawMessage) ([]RefusalEvent, *kernel.Error) {
	var events []RefusalEvent
	for key, r := range refusals {
		if r.Business != nil {
			events = append(events, RefusalEvent{Key: key, Fact: *r.Business})
		}
	}
	for key, raw := range answers {
		var envelope struct{ Kind string }
		if json.Unmarshal(raw, &envelope) != nil || envelope.Kind != "record-batch" {
			continue
		}
		r, _, err := acceptedformat.DecodeBatch(raw)
		if err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted refusal history cannot be read")
		}
		for _, fact := range r.Business {
			events = append(events, RefusalEvent{Key: key, Fact: fact})
		}
	}
	return events, nil
}
