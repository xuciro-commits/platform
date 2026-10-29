package acceptedformat

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/journal"
	"platformserver/kernel/records"
	"platformserver/platform"
	"time"
)

// Input saves the independent connector answer, record images and K8 marks.
// It is not a submission manufactured to fit an application callback to a ledger.
type Input struct {
	Unique         []journal.StateUnique   `json:"unique,omitempty"`
	Authorizations []AuthorizationEvidence `json:"authorizations,omitempty"`
	Version        int                     `json:"version"`
	Kind           string                  `json:"kind"`
	Tenant         string                  `json:"tenant"`
	App            string                  `json:"app"`
	Name           string                  `json:"name"`
	Member         string                  `json:"member"`
	Key            string                  `json:"key"`
	At             time.Time               `json:"at"`
	Body           []byte                  `json:"body"`
	RequestHash    string                  `json:"requestHash"`
	Digest         string                  `json:"digest"`
	Answer         json.RawMessage         `json:"answer,omitempty"`
	Refusal        *kernel.Error           `json:"refusal,omitempty"`
	Changes        json.RawMessage         `json:"changes,omitempty"`
	Rows           []InputRow              `json:"rows,omitempty"`
	Deliveries     []ConnectorDelivery     `json:"deliveries,omitempty"`
	States         []State                 `json:"states,omitempty"`
	Notices        *Notices                `json:"notices,omitempty"`
}

func InputHash(tenant, app, name, member string, body []byte) (string, error) {
	return platform.CanonicalDigest(struct {
		Tenant, App, Name, Member string
		Body                      []byte
	}{tenant, app, name, member, body})
}

func DigestInput(result Input) (string, error) {
	result.Digest = ""
	return platform.CanonicalDigest(result)
}

func DecodeInput(raw []byte) (Input, error) {
	var result Input
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return result, fmt.Errorf("connector result is empty or oversized")
	}
	if err := platform.DecodeJSON(raw, &result, "connector result"); err != nil {
		return result, err
	}
	if err := ValidateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, err
	}

	if result.Version == 1 && len(result.Unique) > 0 {
		return result, fmt.Errorf("legacy connector result carries derived unique keys")
	}
	digest, err := DigestInput(result)
	if err != nil || digest != result.Digest {
		return result, fmt.Errorf("connector result digest differs")
	}
	hash, err := InputHash(result.Tenant, result.App, result.Name, result.Member, result.Body)
	if err != nil || (result.Version != 1 && result.Version != 2) || result.Kind != "input-result" ||
		result.Tenant == "" || result.App == "" || result.Name == "" || result.Member == "" ||
		result.At.IsZero() || result.RequestHash != hash || result.Key != "input:"+hash ||
		result.Refusal != nil && (result.Refusal.Code == pb.ErrorCode_ERROR_CODE_UNSPECIFIED ||
			len(result.Changes) > 0 || len(result.Rows) > 0 || len(result.Deliveries) > 0 ||
			len(result.States) > 0 || len(result.Unique) > 0 || result.Notices != nil) ||
		result.Refusal == nil && len(result.Answer) == 0 {
		return result, fmt.Errorf("connector result has inconsistent identity or effects")
	}
	if len(result.Changes) > 0 {
		batch, receipt, err := DecodeBatch(result.Changes)
		answer := new(pb.ChangeRecord)
		if err != nil || protojson.Unmarshal(result.Answer, answer) != nil ||
			batch.Tenant != result.Tenant || batch.App != result.App || !batch.At.Equal(result.At) ||
			receipt.GetSubmission().GetPrincipalId() != result.Member || !proto.Equal(receipt, answer) ||
			len(result.Rows) > 0 || len(result.Deliveries) > 0 || len(result.States) > 0 || len(result.Unique) > 0 || result.Notices != nil {
			return result, fmt.Errorf("connector result has a foreign child decision: %v", err)
		}
	}
	seen := map[string]bool{}
	for _, row := range result.Rows {
		if result.Version >= 2 {
			if err := records.ValidateHistoryRef(row.Image); err != nil {
				return result, err
			}
		} else if row.HistoryRef != nil || row.Predecessor != "" || row.Format != "" {
			return result, fmt.Errorf("legacy connector result carries new record metadata")
		}
		ref := row.Type + "/" + row.ID
		if row.Type == "" || row.ID == "" || row.App != result.App || seen[ref] ||
			len(row.Value) == 0 || len(row.History) == 0 {
			return result, fmt.Errorf("connector result has a duplicate or invalid row")
		}
		seen[ref] = true
	}
	return result, nil
}
