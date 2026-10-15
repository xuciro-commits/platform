package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func cloneBusinessFact(f platform.BusinessRefusalFact) platform.BusinessRefusalFact {
	f.Original = slices.Clone(f.Original)
	if f.Escalation != nil {
		policy := *f.Escalation
		f.Escalation = &policy
	}
	f.Translations = maps.Clone(f.Translations)
	f.Inputs = maps.Clone(f.Inputs)
	for k, v := range f.Inputs {
		f.Inputs[k] = slices.Clone(v)
	}
	if f.Route != nil {
		r := *f.Route
		f.Route = &r
	}

	return f
}

func validateBusinessFact(app string, f platform.BusinessRefusalFact) error {
	if !slices.Contains([]string{"block", "flag", "route", "rejected"}, f.Level) || f.Code == "" || len(f.Code) > 64 || len(f.Message) > 1024 || f.Message == "" || f.Member == "" || f.At.IsZero() || !strings.HasPrefix(f.Subject, app+".") || !strings.Contains(f.Subject, "/") || !strings.HasPrefix(f.Action, app+".") || len(f.Subject) > 512 || len(f.Original) > 64<<10 || !json.Valid(f.Original) || len(f.Inputs) > 64 || len(f.Translations) > 1 {
		return fmt.Errorf("invalid business refusal evidence")
	}
	for language, message := range f.Translations {
		if language != "zh-CN" || len(message) > 1024 {
			return fmt.Errorf("invalid business refusal translation")
		}
	}
	for name, raw := range f.Inputs {
		if len(name) > 128 || len(raw) > 64<<10 || !json.Valid(raw) {
			return fmt.Errorf("invalid business refusal input")
		}
	}
	if f.Escalation != nil {
		if err := f.Escalation.Check(); err != nil {
			return err
		}
	}
	if f.Level == "route" && (f.Route == nil || f.Route.Action == "") {
		return fmt.Errorf("business refusal has no declared route")
	}

	return nil
}

func (d *stagedDecision) BusinessRefused(answer *kernel.Error, fact platform.BusinessRefusalFact) {
	if answer == nil {
		return
	}
	app, _, _ := strings.Cut(fact.Action, ".")
	if err := validateBusinessFact(app, fact); err != nil {
		d.failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
		return
	}
	if d.businessRefusals == nil {
		d.businessRefusals = map[*kernel.Error]platform.BusinessRefusalFact{}
	}
	d.businessRefusals[answer] = cloneBusinessFact(fact)
	d.InputRefused(answer, []platform.FieldIssue{{Code: "business_refusal", BusinessCode: fact.Code, Record: fact.Subject, Message: fact.Message, Translations: maps.Clone(fact.Translations), Path: []string{"$business"}}})
}

func (d *stagedDecision) CaptureBusinessRefusal(c platform.Caller, receipt *pb.ChangeRecord, fact platform.BusinessRefusalFact) *kernel.Error {
	if !c.Staging() || c.Replaying || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || fact.Member != c.ID || !fact.At.Equal(d.at) || fact.Subject != target(receipt.GetSubmission()) || fact.Action != receipt.GetSubmission().GetSchema().GetName() || fact.Level == "block" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Business refusal evidence must belong to its original decision")
	}
	if err := validateBusinessFact(c.App, fact); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if len(d.businessFacts) >= 64 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Business refusal evidence exceeds its declared bounds")
	}
	d.businessFacts = append(d.businessFacts, cloneBusinessFact(fact))
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
