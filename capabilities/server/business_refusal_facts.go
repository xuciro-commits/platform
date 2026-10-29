package platformserver

import (
	"encoding/json"

	"maps"
	acceptedformat "platformserver/kernel/acceptedformat"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (d *stagedDecision) BusinessRefused(answer *kernel.Error, fact platform.BusinessRefusalFact) {
	if answer == nil {
		return
	}
	app, _, _ := strings.Cut(fact.Action, ".")
	if err := acceptedformat.ValidateBusinessFact(app, fact); err != nil {
		d.failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
		return
	}
	if d.businessRefusals == nil {
		d.businessRefusals = map[*kernel.Error]platform.BusinessRefusalFact{}
	}
	d.businessRefusals[answer] = acceptedformat.CloneBusinessFact(fact)
	d.InputRefused(answer, []platform.FieldIssue{{Code: "business_refusal", BusinessCode: fact.Code, Record: fact.Subject, Message: fact.Message, Translations: maps.Clone(fact.Translations), Path: []string{"$business"}}})
}

func (d *stagedDecision) CaptureBusinessRefusal(c platform.Caller, receipt *pb.ChangeRecord, fact platform.BusinessRefusalFact) *kernel.Error {
	if !c.Staging() || c.Replaying || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || fact.Member != c.ID || !fact.At.Equal(d.at) || fact.Subject != target(receipt.GetSubmission()) || fact.Action != receipt.GetSubmission().GetSchema().GetName() || fact.Level == "block" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal evidence must belong to its original decision")
	}
	if err := acceptedformat.ValidateBusinessFact(c.App, fact); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if len(d.businessFacts) >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
	}
	d.businessFacts = append(d.businessFacts, acceptedformat.CloneBusinessFact(fact))
	return nil
}

func (d *stagedDecision) RefuseBusinessCondition(c platform.Caller, code, message string, original any, inputs map[string]any, at time.Time) *kernel.Error {
	sub := d.currentSubmission
	if sub == nil || sub.GetAuthority() != c.App || sub.GetPrincipalId() != c.ID {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{message}", message)
	}
	fact := platform.BusinessRefusalFact{Level: "block", Code: code, Message: message, Subject: target(sub), Action: sub.GetSchema().GetName(), Member: c.ID, At: at.UTC(), Original: platform.Raw(original), Inputs: map[string]json.RawMessage{}}
	for name, v := range inputs {
		fact.Inputs[name] = platform.Raw(v)
	}
	return c.RefuseBusiness(fact)
}
