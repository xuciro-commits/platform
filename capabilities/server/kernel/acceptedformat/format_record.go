package acceptedformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"io"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/journal"
	"platformserver/kernel/records"
	"platformserver/platform"
	"strings"
	"time"
)

const RecordVersion = 5

const RecordClaimVersion = 6

type Record struct {
	Claims         []SourceClaim                `json:"claims,omitempty"`
	Origin         string                       `json:"origin,omitempty"`
	Unique         []journal.StateUnique        `json:"unique,omitempty"`
	Definition     *platform.DecisionDefinition `json:"definition,omitempty"`
	SchemaVersion  *platform.SchemaVersion      `json:"schemaVersion,omitempty"`
	Authorizations []AuthorizationEvidence      `json:"authorizations,omitempty"`
	Version        int                          `json:"version"`
	Kind           string                       `json:"kind,omitempty"`
	Tenant         string                       `json:"tenant"`
	App            string                       `json:"app"`
	At             time.Time                    `json:"at,omitempty"`
	RequestHash    string                       `json:"requestHash"`
	Digest         string                       `json:"digest,omitempty"`
	Receipt        json.RawMessage              `json:"receipt"`
	Row            records.Image                `json:"row"`
	Event          platform.AcceptedEventPlan   `json:"event"`
}

func DecodeRecord(raw []byte) (Record, *pb.ChangeRecord, error) {
	var result Record
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return result, nil, fmt.Errorf("accepted result is empty or exceeds %d bytes", platform.AcceptedResultMaxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, nil, err
	}
	if result.Row.Projection != nil {
		return result, nil, fmt.Errorf("versioned migration row requires the complete record batch")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, nil, fmt.Errorf("accepted result has trailing data")
	}
	if err := ValidateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, nil, err
	}

	digest, err := DigestRecord(result)
	if err != nil || result.Digest != digest {
		return result, nil, fmt.Errorf("accepted result content digest differs")
	}
	receipt := new(pb.ChangeRecord)
	if err := protojson.Unmarshal(result.Receipt, receipt); err != nil {
		return result, nil, fmt.Errorf("accepted receipt: %w", err)
	}
	sub := receipt.GetSubmission()
	if result.Version < 1 || result.Version > RecordClaimVersion || result.Tenant == "" || result.App == "" ||
		sub == nil || sub.GetTenantId() != result.Tenant || sub.GetAuthority() != result.App ||
		sub.GetIdempotencyKey() == "" || sub.GetPrincipalId() == "" || receipt.GetChangeId() == "" ||
		receipt.GetRecordedTime() == nil || receipt.GetValidTime() == nil ||
		result.Row.Type == "" || result.Row.ID == "" || result.Row.Type != sub.GetTarget().GetType() ||
		result.Row.ID != sub.GetTarget().GetId() || result.Event.App != result.App ||
		len(result.Event.Changed) != 1 || result.Event.Changed[0] != result.Row.Type+"/"+result.Row.ID ||
		len(result.Row.History) == 0 || len(result.Row.Value) == 0 {
		return result, nil, fmt.Errorf("accepted result has inconsistent identity or effects")
	}
	if result.Version >= 2 {
		if result.At.IsZero() || len(result.Event.Names) == 0 || result.Event.Names[0] != sub.GetSchema().GetName() {
			return result, nil, fmt.Errorf("accepted result has no event intent")
		}
		seen := map[string]bool{}
		for _, id := range result.Event.Subscribers {
			if id == "" || seen[id] {
				return result, nil, fmt.Errorf("accepted result has duplicate or empty subscriber")
			}
			seen[id] = true
		}
		for _, effect := range result.Event.Effects {
			if effect.ID == "" || effect.Endpoint == "" || effect.Event == "" {
				return result, nil, fmt.Errorf("accepted result has incomplete effect intent")
			}
		}
	}
	if err := ValidateSourceClaims(result.Claims, result.Origin, result.Version == RecordClaimVersion, result.Version < RecordClaimVersion); err != nil {
		return result, nil, err
	}
	verb, ok := strings.CutPrefix(sub.GetSchema().GetName(), result.Row.Type+".")
	standard := verb == "create" || verb == "edit" || verb == "archive"
	publication := (result.Version == 3 && result.Kind == "" || result.Version == 5 && result.Kind == "publication") && verb == "publish"
	pureTransition := (result.Version == 4 || result.Version == 5) && !standard && verb != "publish" && result.Kind == "pure-transition"
	legacy := result.Version == 1 && (verb == "create" || verb == "edit")
	if !ok || !(legacy && result.Kind == "" ||
		standard && (result.Version == 2 && result.Kind == "" || (result.Version == RecordVersion || result.Version == RecordClaimVersion) && result.Kind == "record") || publication || pureTransition) {
		return result, nil, fmt.Errorf("accepted result is outside the supported generated action family")
	}
	if result.Version < 5 && len(result.Unique) > 0 {
		return result, nil, fmt.Errorf("legacy result has unique-key metadata")
	}
	if result.Version >= 5 {
		if err := records.ValidateHistoryRef(result.Row); err != nil {
			return result, nil, err
		}
	} else if result.Row.HistoryRef != nil || result.Row.Predecessor != "" || result.Row.Format != "" {
		return result, nil, fmt.Errorf("legacy record carries new metadata")
	}
	if err := ValidateSubmissionSchemaVersion(sub, result.SchemaVersion); err != nil {
		return result, nil, err
	}
	if result.Definition != nil {
		if err := result.Definition.Check(result.App, sub.GetSchema().GetName()); err != nil {
			return result, nil, err
		}
	}
	hash, err := SubmissionHash(sub, result.SchemaVersion)
	if err != nil || result.RequestHash != hash {
		return result, nil, fmt.Errorf("accepted result request digest differs from receipt")
	}
	last := result.Row.History[len(result.Row.History)-1]
	if last.Change != receipt.GetChangeId() || last.Schema != sub.GetSchema().GetName() ||
		last.By != sub.GetPrincipalId() || !last.At.Equal(receipt.GetRecordedTime().AsTime()) {
		return result, nil, fmt.Errorf("record history does not end at accepted receipt")
	}
	return result, receipt, nil
}

func DigestRecord(result Record) (string, error) {
	result.Digest = ""
	return platform.CanonicalDigest(result)
}
