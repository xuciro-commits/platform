package platformserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// A refused top-level decision has a durable, effect-free answer too. It
// shares the accepted-result journal category and idempotency namespace, but
// cannot be mistaken for a kernel change receipt or applied to the record
// store. The bounded envelope carries the original submission and answer.
type refusedResult struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	Tenant      string          `json:"tenant"`
	App         string          `json:"app"`
	At          time.Time       `json:"at"`
	RequestHash string          `json:"requestHash"`
	Digest      string          `json:"digest"`
	Submission  json.RawMessage `json:"submission"`
	Error       kernel.Error    `json:"error"`
}

func submissionHash(s *pb.Submission) (string, error) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(s)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func encodeRefusedResult(s *pb.Submission, at time.Time, refusal *kernel.Error) ([]byte, error) {
	if s == nil || refusal == nil {
		return nil, fmt.Errorf("refused result needs a submission and answer")
	}
	sub, err := protojson.Marshal(s)
	if err != nil {
		return nil, err
	}
	hash, err := submissionHash(s)
	if err != nil {
		return nil, err
	}
	result := refusedResult{Version: 1, Kind: "refusal", Tenant: s.GetTenantId(), App: s.GetAuthority(),
		At: at.UTC(), RequestHash: hash, Submission: sub, Error: *refusal}
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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, nil, fmt.Errorf("refused result has trailing data")
	}
	digest, err := digestRefusedResult(result)
	if err != nil || result.Digest != digest {
		return result, nil, fmt.Errorf("refused result content digest differs")
	}
	var sub pb.Submission
	if err := protojson.Unmarshal(result.Submission, &sub); err != nil {
		return result, nil, fmt.Errorf("refused submission: %w", err)
	}
	hash, err := submissionHash(&sub)
	if err != nil {
		return result, nil, err
	}
	if result.Version != 1 || result.Kind != "refusal" || result.Tenant == "" || result.App == "" ||
		result.At.IsZero() || result.RequestHash != hash || result.Tenant != sub.GetTenantId() ||
		result.App != sub.GetAuthority() || sub.GetPrincipalId() == "" || sub.GetIdempotencyKey() == "" ||
		sub.GetSchema() == nil || sub.GetTarget() == nil || result.Error.Code == pb.ErrorCode_ERROR_CODE_UNSPECIFIED {
		return result, nil, fmt.Errorf("refused result has inconsistent identity or answer")
	}
	return result, &sub, nil
}

func digestRefusedResult(result refusedResult) (string, error) {
	result.Digest = ""
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	var normalized any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
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
