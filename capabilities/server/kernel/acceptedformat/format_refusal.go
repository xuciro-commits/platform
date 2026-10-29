package acceptedformat

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"time"
)

// A refused top-level decision has a durable, effect-free answer too. It
// shares the accepted-result journal category and idempotency namespace, but
// cannot be mistaken for a kernel change receipt or applied to the record
// store. The bounded envelope carries the original submission and answer.
type Refusal struct {
	Business      *platform.BusinessRefusalFact `json:"business,omitempty"`
	SchemaVersion *platform.SchemaVersion       `json:"schemaVersion,omitempty"`
	Version       int                           `json:"version"`
	Kind          string                        `json:"kind"`
	Tenant        string                        `json:"tenant"`
	App           string                        `json:"app"`
	At            time.Time                     `json:"at"`
	RequestHash   string                        `json:"requestHash"`
	Digest        string                        `json:"digest"`
	Submission    json.RawMessage               `json:"submission"`
	Issues        []platform.FieldIssue         `json:"issues,omitempty"`
	Error         kernel.Error                  `json:"error"`
}

func EncodeRefusal(s *pb.Submission, at time.Time, refusal *kernel.Error, issues ...platform.FieldIssue) ([]byte, error) {
	return EncodeSchemaRefusal(s, at, refusal, nil, issues...)
}

func EncodeSchemaRefusal(s *pb.Submission, at time.Time, refusal *kernel.Error, binding *platform.SchemaVersion, issues ...platform.FieldIssue) ([]byte, error) {
	return EncodeBusinessRefusal(s, at, refusal, binding, nil, issues...)
}

func EncodeBusinessRefusal(s *pb.Submission, at time.Time, refusal *kernel.Error, binding *platform.SchemaVersion, business *platform.BusinessRefusalFact, issues ...platform.FieldIssue) ([]byte, error) {
	if s == nil || refusal == nil {
		return nil, fmt.Errorf("refused result needs a submission and answer")
	}
	sub, err := protojson.Marshal(s)
	if err != nil {
		return nil, err
	}
	hash, err := SubmissionHash(s, binding)
	if err != nil {
		return nil, err
	}
	result := Refusal{Version: 1, Kind: "refusal", Tenant: s.GetTenantId(), App: s.GetAuthority(),
		At: at.UTC(), RequestHash: hash, Submission: sub, Error: *refusal, SchemaVersion: binding}
	if len(issues) > 0 {
		result.Version = 2
		result.Issues = issues
		if slices.ContainsFunc(issues, func(issue platform.FieldIssue) bool { return issue.Constraint != "" }) {
			result.Version = 3
		}
	}
	if business != nil {
		fact := CloneBusinessFact(*business)
		result.Business = &fact
		result.Version = 4
	}
	result.Digest, err = DigestRefusal(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := DecodeRefusal(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func DecodeRefusal(raw []byte) (Refusal, *pb.Submission, error) {
	var result Refusal
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return result, nil, fmt.Errorf("refused result is empty or exceeds %d bytes", platform.AcceptedResultMaxBytes)
	}
	if err := platform.DecodeJSON(raw, &result, "refused result"); err != nil {
		return result, nil, err
	}
	digest, err := DigestRefusal(result)
	if err != nil || result.Digest != digest {
		return result, nil, fmt.Errorf("refused result content digest differs")
	}
	var sub pb.Submission
	if err := protojson.Unmarshal(result.Submission, &sub); err != nil {
		return result, nil, fmt.Errorf("refused submission: %w", err)
	}
	if err := ValidateSubmissionSchemaVersion(&sub, result.SchemaVersion); err != nil {
		return result, nil, err
	}
	hash, err := SubmissionHash(&sub, result.SchemaVersion)
	if err != nil {
		return result, nil, err
	}
	if (result.Version < 1 || result.Version > 4) || result.Version == 1 && len(result.Issues) > 0 || (result.Version == 4) != (result.Business != nil) || len(result.Issues) > 64 || result.Kind != "refusal" || result.Tenant == "" || result.App == "" ||
		result.At.IsZero() || result.RequestHash != hash || result.Tenant != sub.GetTenantId() ||
		result.App != sub.GetAuthority() || sub.GetPrincipalId() == "" || sub.GetIdempotencyKey() == "" ||
		sub.GetSchema() == nil || sub.GetTarget() == nil || result.Error.Code == pb.ErrorCode_ERROR_CODE_UNSPECIFIED {
		return result, nil, fmt.Errorf("refused result has inconsistent identity or answer")
	}
	if result.Business != nil {
		f := result.Business
		if result.Version != 4 || f.Level != "block" || f.Member != sub.GetPrincipalId() || f.Subject != sub.GetTarget().GetType()+"/"+sub.GetTarget().GetId() || f.Action != sub.GetSchema().GetName() || !f.At.Equal(result.At) {
			return result, nil, fmt.Errorf("business refusal evidence differs from its request")
		}
		if err := ValidateBusinessFact(result.App, *f); err != nil {
			return result, nil, err
		}
	}
	for _, issue := range result.Issues {
		if issue.Code == "" || len(issue.Code) > 64 || len(issue.Message) > 1024 || len(issue.Path) != 1 || len(issue.Path[0]) > 128 || len(issue.RelatedPaths) > 15 {
			return result, nil, fmt.Errorf("refused input issue exceeds its bounds")
		}
		if issue.Constraint == "" && len(issue.RelatedPaths) > 4 {
			return result, nil, fmt.Errorf("refused input issue exceeds its original related path bound")
		}
		if issue.Constraint != "" {
			if result.Version != 3 || issue.Code != "constraint_violation" || len(issue.Constraint) > 64 || issue.Record == "" || len(issue.Record) > 512 || len(issue.Reason) > 256 || len(issue.Translations) > 1 || len(issue.Values) > 16 {
				return result, nil, fmt.Errorf("refused constraint diagnostic exceeds its bounds")
			}
			for name, value := range issue.Values {
				if len(name) > 128 || len(value) > 4096 || !json.Valid(value) {
					return result, nil, fmt.Errorf("refused constraint value exceeds its bounds")
				}
			}
			for language, message := range issue.Translations {
				if language != "zh-CN" || len(message) > 512 {
					return result, nil, fmt.Errorf("refused constraint translation exceeds its bounds")
				}
			}
		} else if issue.Code == "business_refusal" {
			if result.Business == nil || issue.BusinessCode != result.Business.Code || issue.Record != result.Business.Subject || len(issue.Values) > 0 || len(issue.Translations) > 1 {
				return result, nil, fmt.Errorf("business diagnostic differs from its evidence")
			}
		} else if issue.BusinessCode != "" || issue.Record != "" || len(issue.Values) > 0 || issue.Reason != "" || len(issue.Translations) > 0 {
			return result, nil, fmt.Errorf("constraint facts need a named constraint")
		}
		for _, path := range issue.RelatedPaths {
			if len(path) != 1 || len(path[0]) > 128 {
				return result, nil, fmt.Errorf("refused related path exceeds its bounds")
			}
		}
	}
	return result, &sub, nil
}

func DigestRefusal(result Refusal) (string, error) {
	result.Digest = ""
	return platform.CanonicalDigest(result)
}

func CloneBusinessFact(f platform.BusinessRefusalFact) platform.BusinessRefusalFact {
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
