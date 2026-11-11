package kernel

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"time"

	"platformserver/kernel/acceptedformat"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
)

func (d *Decision) SavedUniqueChanges() []persistence.StateUnique {
	d.Records.Lock()
	defer d.Records.Unlock()
	return UniqueReplacements(d.Records.UniqueChanges())
}

func (d *Decision) FlowVersions() map[string]int {
	if private, ok := d.PrivateApps[platform.FlowApp].(platform.Processes); ok {
		return private.Versions()
	}
	return nil
}

func (d *Decision) SavedAuthorizations() ([]acceptedformat.AuthorizationEvidence, error) {
	var saved []acceptedformat.AuthorizationEvidence
	seen := map[string]bool{}
	for _, item := range d.Authorizations {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		key := string(raw)
		if !seen[key] {
			seen[key] = true
			saved = append(saved, item)
		}
	}
	return saved, nil
}

func (d *Decision) Get(c platform.Caller, typ reflect.Type, id string) (any, bool) {
	return d.Records.Get(c, typ, id)
}

func (d *Decision) Check(c platform.Caller, entity any) *kernel.Error {
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	return d.Records.Check(c, entity, at)
}

func (d *Decision) CaptureBusinessRefusal(c platform.Caller, receipt *pb.ChangeRecord, fact platform.BusinessRefusalFact) *kernel.Error {
	if !c.Staging() || c.Replaying || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || fact.Member != c.ID || !fact.At.Equal(d.At) || fact.Subject != Target(receipt.GetSubmission()) || fact.Action != receipt.GetSubmission().GetSchema().GetName() || fact.Level == "block" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal evidence must belong to its original decision")
	}
	if err := acceptedformat.ValidateBusinessFact(c.App, fact); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if len(d.BusinessFacts) >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
	}
	d.BusinessFacts = append(d.BusinessFacts, acceptedformat.CloneBusinessFact(fact))
	return nil
}

func (d *Decision) RefuseBusinessCondition(c platform.Caller, code, message string, original any, inputs map[string]any, at time.Time) *kernel.Error {
	sub := d.CurrentSubmission
	if sub == nil || sub.GetAuthority() != c.App || sub.GetPrincipalId() != c.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", message)
	}
	fact := platform.BusinessRefusalFact{Level: "block", Code: code, Message: message, Subject: Target(sub), Action: sub.GetSchema().GetName(), Member: c.ID, At: at.UTC(), Original: platform.Raw(original), Inputs: map[string]json.RawMessage{}}
	for name, v := range inputs {
		fact.Inputs[name] = platform.Raw(v)
	}
	return c.RefuseBusiness(fact)
}
