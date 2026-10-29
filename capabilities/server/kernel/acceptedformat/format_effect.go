package acceptedformat

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/journal"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"time"
)

const OperationEndpoint = "operation"
const ModelEndpoint = "model"

type EffectState struct {
	platform.Effect
	Since int `json:"since,omitempty"`
}

type Effect struct {
	Unique         []journal.StateUnique   `json:"unique,omitempty"`
	Authorizations []AuthorizationEvidence `json:"authorizations,omitempty"`
	Version        int                     `json:"version"`
	Kind           string                  `json:"kind"`
	Tenant         string                  `json:"tenant"`
	App            string                  `json:"app"`
	Key            string                  `json:"key"`
	At             time.Time               `json:"at"`
	RequestHash    string                  `json:"requestHash"`
	Digest         string                  `json:"digest"`
	Outcome        platform.Outcome        `json:"outcome"`
	Usage          *platform.AIUsage       `json:"usage,omitempty"`
	UsageBefore    string                  `json:"usageBefore,omitempty"`
	CallbackError  string                  `json:"callbackError,omitempty"`
	Before         EffectState             `json:"before"`
	After          EffectState             `json:"after"`
	Member         string                  `json:"member,omitempty"`
	Rows           []InputRow              `json:"rows,omitempty"`
	Changes        json.RawMessage         `json:"changes,omitempty"`
	Notices        *Notices                `json:"notices,omitempty"`
	Intents        []platform.Effect       `json:"intents,omitempty"`
	States         []State                 `json:"states,omitempty"`
	WorkBefore     json.RawMessage         `json:"workBefore,omitempty"`
	Work           json.RawMessage         `json:"work,omitempty"`
	Control        *Control                `json:"control,omitempty"`
}

func EffectResultKey(before EffectState) string {
	return "effect:" + before.ID + ":" + strconv.Itoa(before.Attempts+1)
}

func EffectResultHash(before EffectState, out platform.Outcome, at time.Time) (string, error) {
	return platform.CanonicalDigest([]any{before, out, at})
}

func DigestEffect(result Effect) (string, error) {
	result.Digest = ""
	return platform.CanonicalDigest(result)
}

func DecodeEffect(raw []byte) (Effect, error) {
	var result Effect
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return result, fmt.Errorf("effect result is empty or too large")
	}
	if err := platform.DecodeJSON(raw, &result, "effect result"); err != nil {
		return result, err
	}
	if err := ValidateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, err
	}

	if result.Version < 5 && (result.Outcome.PublicMessage != "" || result.Before.PublicMessage != "" || result.After.PublicMessage != "") {
		return result, fmt.Errorf("legacy effect result carries public delivery text")
	}
	if result.Version >= 5 && result.Outcome.Detail != "" && result.Outcome.PublicMessage == "" {
		return result, fmt.Errorf("effect result omits its public delivery text")
	}
	if result.Version < 4 && len(result.Unique) > 0 {
		return result, fmt.Errorf("legacy effect result carries derived unique keys")
	}
	digest, err := DigestEffect(result)
	hash, hashErr := EffectResultHash(result.Before, result.Outcome, result.At)
	expected := result.Before
	MarkEffect(&expected.Effect, expected.Since, result.Outcome, result.At)
	if result.Version >= 3 {
		expected.CallbackError = result.CallbackError
	}
	if err != nil || hashErr != nil || result.Digest != digest || result.RequestHash != hash ||
		(result.Version != 1 && result.Version != 2 && result.Version != 3 && result.Version != 4 && result.Version != 5) || result.Version == 1 && result.Control != nil || result.Kind != "effect-result" || result.Tenant == "" ||
		result.App != "platform" || result.At.IsZero() || result.Before.ID == "" ||
		!slices.Contains([]string{"delivered", "rejected", "retry", "unknown"}, result.Outcome.Result) ||
		len(result.Outcome.Answer) > 64<<10 ||
		len(result.Outcome.Answer) > 0 && !json.Valid(result.Outcome.Answer) ||
		result.Outcome.Effect != result.Before.ID || result.Key != EffectResultKey(result.Before) ||
		!reflect.DeepEqual(result.After, expected) {
		return result, fmt.Errorf("effect result has inconsistent identity, transition or digest")
	}
	if len(result.Changes) > 0 && (result.Notices != nil || len(result.Intents) > 0 || len(result.States) > 0 || result.Control != nil) {
		return result, fmt.Errorf("effect result duplicates its child decision effects")
	}
	if len(result.Rows) > 0 && result.Member == "" {
		return result, fmt.Errorf("effect result omits the direct record writer")
	}
	for _, row := range result.Rows {
		if result.Version >= 4 {
			if err := records.ValidateHistoryRef(row.Image); err != nil {
				return result, err
			}
		} else if row.HistoryRef != nil || row.Predecessor != "" || row.Format != "" {
			return result, fmt.Errorf("legacy effect result carries new record metadata")
		}
		if row.App != result.Before.App || result.Member != "app:"+row.App ||
			row.Type == "" || row.ID == "" || len(row.History) == 0 || len(row.Value) == 0 {
			return result, fmt.Errorf("effect result has an invalid direct record")
		}
	}
	if result.Usage != nil {
		if result.Before.Endpoint != ModelEndpoint || result.UsageBefore == "" ||
			result.Usage.Member != "app:"+result.Before.App || result.Usage.Model == "" ||
			!result.Usage.At.Equal(result.At) || result.Usage.Input < 0 || result.Usage.Output < 0 ||
			result.Usage.Cost < 0 || result.Usage.Millis < 0 {
			return result, fmt.Errorf("effect result has an invalid model usage")
		}
	} else if result.UsageBefore != "" {
		return result, fmt.Errorf("effect result has a meter predecessor without usage")
	}
	if result.Before.Endpoint == OperationEndpoint {
		var prior, work pb.Work
		if result.Outcome.Generation == 0 || result.Outcome.Generation != result.Before.Generation ||
			protojson.Unmarshal(result.WorkBefore, &prior) != nil || protojson.Unmarshal(result.Work, &work) != nil ||
			prior.GetWorkId() != result.Before.ID || prior.GetGeneration() != result.Before.Generation || prior.GetState() != pb.WorkState_WORK_STATE_RUNNING ||
			work.GetWorkId() != prior.GetWorkId() || work.GetGeneration() != prior.GetGeneration() {
			return result, fmt.Errorf("operation result has invalid ownership")
		}
	} else if len(result.WorkBefore) > 0 || len(result.Work) > 0 {
		return result, fmt.Errorf("external effect has compute ownership")
	}
	return result, nil
}
