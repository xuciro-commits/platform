package acceptedformat

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ValidateSourceClaims(claims []SourceClaim, origin string, required, forbidden bool) error {
	if required && len(claims) == 0 || forbidden && (len(claims) > 0 || origin != "") || len(claims) > 256 || len(origin) > 256 {
		return fmt.Errorf("source claim version or budget differs")
	}
	seen := map[string]bool{}
	for _, claim := range claims {
		kind, source, ok := strings.Cut(claim.Connector, ":")
		if claim.Attribute == "" || len(claim.Attribute) > 128 || seen[claim.Attribute] || !ok || source == "" ||
			kind != "source" && kind != "pipeline" && kind != "dataset" || claim.SourceTime.IsZero() ||
			len(claim.Payload) == 0 || len(claim.Payload) > 64<<10 || !json.Valid(claim.Payload) {
			return fmt.Errorf("source claim has an invalid attribute, connector, time or payload")
		}
		seen[claim.Attribute] = true
	}
	return nil
}

func AttachSourceClaims(raw []byte, origin string, claims []SourceClaim) ([]byte, error) {
	var envelope struct{ Kind string }
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if err := ValidateSourceClaims(claims, origin, true, false); err != nil {
		return nil, err
	}
	if envelope.Kind == "record-batch" {
		result, _, err := DecodeBatch(raw)
		if err != nil {
			return nil, err
		}
		result.Version, result.Origin, result.Claims = 11, origin, claims
		result.Digest, err = DigestBatch(result)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(result)
		if err == nil {
			_, _, err = DecodeBatch(encoded)
		}
		return encoded, err
	}
	result, _, err := DecodeRecord(raw)
	if err != nil || result.Kind != "record" {
		return nil, fmt.Errorf("source claim requires an accepted object record: %v", err)
	}
	result.Version, result.Origin, result.Claims = RecordClaimVersion, origin, claims
	result.Digest, err = DigestRecord(result)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err == nil {
		_, _, err = DecodeRecord(encoded)
	}
	return encoded, err
}
