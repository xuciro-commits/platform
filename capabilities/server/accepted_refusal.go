package platformserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// A refused top-level decision has a durable, effect-free answer too. It
// shares the accepted-result journal category and idempotency namespace, but
// cannot be mistaken for a kernel change receipt or applied to the record
// store. The bounded envelope carries the original submission and answer.
type refusedResult struct {
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

func submissionHash(s *pb.Submission, versions ...*platform.SchemaVersion) (string, error) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(s)
	if err != nil {
		return "", err
	}
	if len(versions) > 0 && versions[0] != nil {
		binding, err := json.Marshal(versions[0])
		if err != nil {
			return "", err
		}
		raw = append(append(raw, 0), binding...)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func encodeRefusedResult(s *pb.Submission, at time.Time, refusal *kernel.Error, issues ...platform.FieldIssue) ([]byte, error) {
	return encodeSchemaRefusedResult(s, at, refusal, nil, issues...)
}

func encodeSchemaRefusedResult(s *pb.Submission, at time.Time, refusal *kernel.Error, binding *platform.SchemaVersion, issues ...platform.FieldIssue) ([]byte, error) {
	return encodeBusinessRefusedResult(s, at, refusal, binding, nil, issues...)
}

func encodeBusinessRefusedResult(s *pb.Submission, at time.Time, refusal *kernel.Error, binding *platform.SchemaVersion, business *platform.BusinessRefusalFact, issues ...platform.FieldIssue) ([]byte, error) {
	if s == nil || refusal == nil {
		return nil, fmt.Errorf("refused result needs a submission and answer")
	}
	sub, err := protojson.Marshal(s)
	if err != nil {
		return nil, err
	}
	hash, err := submissionHash(s, binding)
	if err != nil {
		return nil, err
	}
	result := refusedResult{Version: 1, Kind: "refusal", Tenant: s.GetTenantId(), App: s.GetAuthority(),
		At: at.UTC(), RequestHash: hash, Submission: sub, Error: *refusal, SchemaVersion: binding}
	if len(issues) > 0 {
		result.Version = 2
		result.Issues = issues
		if slices.ContainsFunc(issues, func(issue platform.FieldIssue) bool { return issue.Constraint != "" }) {
			result.Version = 3
		}
	}
	if business != nil {
		fact := cloneBusinessFact(*business)
		result.Business = &fact
		result.Version = 4
	}
	result.Digest, err = digestRefusedResult(result)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, _, err := decodeRefusedResult(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func decodeRefusedResult(raw []byte) (refusedResult, *pb.Submission, error) {
	var result refusedResult
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return result, nil, fmt.Errorf("refused result is empty or exceeds %d bytes", maxAcceptedResultBytes)
	}
	if err := decodeAcceptedJSON(raw, &result, "refused result"); err != nil {
		return result, nil, err
	}
	digest, err := digestRefusedResult(result)
	if err != nil || result.Digest != digest {
		return result, nil, fmt.Errorf("refused result content digest differs")
	}
	var sub pb.Submission
	if err := protojson.Unmarshal(result.Submission, &sub); err != nil {
		return result, nil, fmt.Errorf("refused submission: %w", err)
	}
	if err := validateSubmissionSchemaVersion(&sub, result.SchemaVersion); err != nil {
		return result, nil, err
	}
	hash, err := submissionHash(&sub, result.SchemaVersion)
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
		if result.Version != 4 || f.Level != "block" || f.Member != sub.GetPrincipalId() || f.Subject != target(&sub) || f.Action != sub.GetSchema().GetName() || !f.At.Equal(result.At) {
			return result, nil, fmt.Errorf("business refusal evidence differs from its request")
		}
		if err := validateBusinessFact(result.App, *f); err != nil {
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

func digestRefusedResult(result refusedResult) (string, error) {
	result.Digest = ""
	return canonicalDigest(result)
}

func resultRequestHash(raw []byte) (string, error) {
	identity, err := acceptedIdentity(raw)
	return identity.Hash, err
}

func acceptedIdentity(raw []byte) (resultIdentity, error) {
	var identity resultIdentity
	var envelope struct{ Kind string }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return identity, err
	}
	identity.Scope = "submission"
	if envelope.Kind == "host-result" {
		r, e := decodeAcceptedHostControl(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "host"
		return identity, e
	}
	if envelope.Kind == "composite-result" {
		r, e := decodeAcceptedComposite(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "composite"
		return identity, e
	}
	if envelope.Kind == "usage-result" {
		r, e := decodeAcceptedUsage(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "usage"
		return identity, e
	}
	if envelope.Kind == "agent-result" {
		r, e := decodeAcceptedAgent(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash, identity.Scope = r.Tenant, r.App, r.Key, r.RequestHash, "agent"
		return identity, e
	}
	if envelope.Kind == "operation-claim" {
		result, err := decodeOperationClaim(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	if envelope.Kind == "refusal" {
		result, sub, err := decodeRefusedResult(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "record-batch" {
		result, receipt, err := decodeAcceptedBatch(raw)
		if err != nil {
			return identity, err
		}
		sub, err := batchSubmission(result, receipt)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, sub.GetIdempotencyKey(), result.RequestHash
		return identity, err
	}
	if envelope.Kind == "work-result" {
		result, err := decodeAcceptedWork(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "work"
		return identity, err
	}
	if envelope.Kind == "input-result" {
		result, err := decodeAcceptedInput(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		identity.Scope = "input"
		return identity, err
	}
	if envelope.Kind == "effect-result" {
		result, err := decodeAcceptedEffect(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, PlatformApp, result.Key, result.RequestHash
		identity.Scope = "effect"
		return identity, err
	}
	if envelope.Kind == "release-result" {
		result, err := decodeAcceptedRelease(raw)
		identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, result.Key, result.RequestHash
		return identity, err
	}
	result, receipt, err := decodeAcceptedResult(raw)
	identity.Tenant, identity.App, identity.Key, identity.Hash = result.Tenant, result.App, receipt.GetSubmission().GetIdempotencyKey(), result.RequestHash
	return identity, err
}
