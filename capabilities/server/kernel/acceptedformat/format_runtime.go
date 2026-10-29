package acceptedformat

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"io"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/systemscope"
	"platformserver/journal"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
	"time"
)

type RuntimeAgent struct {
	Version     int                      `json:"version"`
	Kind        string                   `json:"kind"`
	Tenant      string                   `json:"tenant"`
	App         string                   `json:"app"`
	Member      string                   `json:"member"`
	Key         string                   `json:"key"`
	At          time.Time                `json:"at"`
	RequestHash string                   `json:"requestHash"`
	Digest      string                   `json:"digest"`
	Step        platform.AgentResultStep `json:"step"`
	UsageBefore string                   `json:"usageBefore,omitempty"`
	Changes     json.RawMessage          `json:"changes"`
}

func DigestAgent(r RuntimeAgent) (string, error) { r.Digest = ""; return platform.CanonicalDigest(r) }

func AgentRequestHash(r RuntimeAgent) (string, error) {
	return platform.CanonicalDigest([]any{r.Tenant, r.Member, r.Step, r.UsageBefore})
}

func DecodeAgent(raw []byte) (RuntimeAgent, error) {
	var r RuntimeAgent
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return r, fmt.Errorf("agent result is empty or too large")
	}
	if err := platform.DecodeJSON(raw, &r, "agent result"); err != nil {
		return r, err
	}
	hash, err := AgentRequestHash(r)
	digest, digestErr := DigestAgent(r)
	batch, receipt, batchErr := DecodeBatch(r.Changes)
	if err != nil || digestErr != nil || batchErr != nil || r.Version != 1 || r.Kind != "agent-result" || r.Tenant == "" || r.App != platform.AgentApp || r.Member == "" || r.At.IsZero() || r.Step.Run == "" || r.RequestHash != hash || r.Digest != digest ||
		batch.Tenant != r.Tenant || batch.App != platform.AgentApp || !batch.At.Equal(r.At) || receipt.GetSubmission().GetPrincipalId() != r.Member || r.Key != "agent:"+receipt.GetSubmission().GetIdempotencyKey() {
		return r, fmt.Errorf("agent result has inconsistent identity or changes")
	}
	if r.Step.Evaluation == nil && r.Step.Usage.Model != "" {
		if !ValidUsage(r.Step.Usage) || r.UsageBefore == "" || r.Step.Usage.Member != r.Member {
			return r, fmt.Errorf("agent result has invalid usage")
		}
	} else if r.UsageBefore != "" {
		return r, fmt.Errorf("agent result has unexpected usage")
	}
	return r, nil
}

type HostControl struct {
	Authorizations []AuthorizationEvidence   `json:"authorizations,omitempty"`
	Version        int                       `json:"version"`
	Kind           string                    `json:"kind"`
	Tenant         string                    `json:"tenant"`
	App            string                    `json:"app"`
	Member         string                    `json:"member"`
	Key            string                    `json:"key"`
	At             time.Time                 `json:"at"`
	Action         string                    `json:"action"`
	Request        json.RawMessage           `json:"request"`
	RequestHash    string                    `json:"requestHash"`
	Digest         string                    `json:"digest"`
	Before         string                    `json:"before"`
	AuditBefore    string                    `json:"auditBefore"`
	Image          platform.HostControlImage `json:"image"`
	Audit          platform.AuditEntry       `json:"audit"`
	Answer         platform.SupportGrant     `json:"answer,omitempty"`
}

func HostRequestHash(r HostControl) (string, error) {
	return platform.CanonicalDigest([]any{r.Tenant, r.Member, r.Action, r.Request, r.At})
}

func HostResultDigest(r HostControl) (string, error) {
	r.Digest = ""
	return platform.CanonicalDigest(r)
}

func DecodeHostControl(raw []byte) (HostControl, error) {
	var r HostControl
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return r, fmt.Errorf("host result is empty or too large")
	}
	if err := platform.DecodeJSON(raw, &r, "host result"); err != nil {
		return r, err
	}
	if err := ValidateAuthorizationEvidence(r.Tenant, r.Authorizations); err != nil {
		return r, err
	}
	hash, hashErr := HostRequestHash(r)
	digest, digestErr := HostResultDigest(r)
	if hashErr != nil || digestErr != nil || r.Version != 1 || r.Kind != "host-result" || r.Tenant == "" || r.App != controlowner.PlatformApp || r.Member == "" || r.At.IsZero() ||
		!slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration", "composition"}, r.Action) || r.Key != "host:"+r.Action+":"+hash || r.RequestHash != hash || r.Digest != digest || r.Before == "" || r.AuditBefore == "" ||
		r.Audit.Member != r.Member || r.Audit.App != controlowner.PlatformApp || !r.Audit.At.Equal(r.At) || r.Audit.Action == "" {
		return r, fmt.Errorf("host result identity differs")
	}
	if err := controlowner.ValidateHostImage(r.Tenant, r.Image); err != nil {
		return r, err
	}
	receipt, ok := r.Image.Requests[r.Key]
	if !ok || receipt.RequestHash != hash || receipt.Answer != r.Answer {
		return r, fmt.Errorf("host result has no matching receipt")
	}
	return r, nil
}

type Work struct {
	Unique         []journal.StateUnique       `json:"unique,omitempty"`
	Authorizations []AuthorizationEvidence     `json:"authorizations,omitempty"`
	Version        int                         `json:"version"`
	Kind           string                      `json:"kind"`
	Tenant         string                      `json:"tenant"`
	App            string                      `json:"app"`
	Key            string                      `json:"key"`
	At             time.Time                   `json:"at"`
	RequestHash    string                      `json:"requestHash"`
	Digest         string                      `json:"digest"`
	Input          platform.ScheduledTaskState `json:"input"`
	PriorWork      json.RawMessage             `json:"priorWork,omitempty"`
	Work           json.RawMessage             `json:"work"`
	Outcome        string                      `json:"outcome"`
	After          platform.ScheduledTask      `json:"after"`
	Changes        json.RawMessage             `json:"changes,omitempty"`
	Notices        *Notices                    `json:"notices,omitempty"`
	Intents        []platform.Effect           `json:"intents,omitempty"`
	States         []State                     `json:"states,omitempty"`
	Control        *Control                    `json:"control,omitempty"`
}

func WorkInputHash(input platform.ScheduledTaskState, prior json.RawMessage) (string, error) {
	return platform.CanonicalDigest([]any{input, prior})
}

func DigestWork(result Work) (string, error) {
	result.Digest = ""
	return platform.CanonicalDigest(result)
}

func DecodeWork(raw []byte) (Work, error) {
	var result Work
	if len(raw) == 0 || len(raw) > platform.AcceptedResultMaxBytes {
		return result, fmt.Errorf("work result is empty or too large")
	}
	if err := platform.DecodeJSON(raw, &result, "work result"); err != nil {
		return result, err
	}
	if err := ValidateAuthorizationEvidence(result.Tenant, result.Authorizations); err != nil {
		return result, err
	}

	if result.Version < 3 && len(result.Unique) > 0 {
		return result, fmt.Errorf("legacy work result carries derived unique keys")
	}
	digest, err := DigestWork(result)
	hash, hashErr := WorkInputHash(result.Input, result.PriorWork)
	finished := new(pb.Work)
	workErr := protojson.Unmarshal(result.Work, finished)
	if err != nil || hashErr != nil || result.Digest != digest || result.RequestHash != hash || workErr != nil ||
		(result.Version != 1 && result.Version != 2 && result.Version != 3) || result.Version == 1 && (result.Control != nil || len(result.States) > 0) || result.Kind != "work-result" || result.Tenant == "" || result.App == "" || result.At.IsZero() ||
		result.Input.ID == "" || result.Input.App != result.App || result.Input.Kind != "delivery" && result.Input.Kind != "job" ||
		result.After.ID != result.Input.ID || result.After.App != result.App || result.After.Kind != result.Input.Kind ||
		result.After.Title != result.Input.Title || result.After.Attempts != result.Input.Attempts+1 ||
		!result.After.Last.Equal(result.At) || result.Key != fmt.Sprintf("work:%s:%d", result.Input.ID, result.After.Attempts) ||
		finished.GetWorkId() != result.Input.ID || finished.GetOwnerId() != "host" ||
		int(finished.GetGeneration()) != result.After.Attempts || result.Outcome == "" {
		return result, fmt.Errorf("work result has inconsistent identity, outcome or digest")
	}
	if err := platform.CheckApplicationBindings(result.After.Applications); err != nil {
		return result, err
	}
	if result.Input.Since < 0 || result.Input.Since > result.Input.Attempts {
		return result, fmt.Errorf("work result has an invalid retry base")
	}
	if len(result.PriorWork) > 0 {
		prior := new(pb.Work)
		if protojson.Unmarshal(result.PriorWork, prior) != nil || prior.GetWorkId() != result.Input.ID ||
			int(prior.GetGeneration()) != result.Input.Attempts || prior.GetState() == pb.WorkState_WORK_STATE_RUNNING {
			return result, fmt.Errorf("work result has an invalid prior generation")
		}
	} else if result.Input.Attempts != 0 {
		return result, fmt.Errorf("work result omits its prior generation")
	}
	if result.Input.Kind == "job" {
		if result.Input.State != "scheduled" || result.After.State != "scheduled" || !result.After.Due.After(result.At) ||
			result.Outcome == "ok" && result.After.Error != "" ||
			result.Outcome != "ok" && result.After.Error != result.Outcome {
			return result, fmt.Errorf("work result has an invalid job outcome")
		}
	} else {
		if result.Input.State != "queued" && result.Input.State != "retrying" ||
			result.Outcome == "ok" && (result.After.State != "done" || result.After.Error != "") ||
			result.Outcome != "ok" && (result.After.State != "failed" && result.After.State != "retrying" || result.After.Error != result.Outcome) ||
			result.After.State == "retrying" && !result.After.Due.After(result.At) ||
			result.After.State != "retrying" && !result.After.Due.Equal(result.Input.Due) {
			return result, fmt.Errorf("work result has an invalid delivery outcome")
		}
	}
	if len(result.Changes) > 0 {
		changes, _, err := DecodeBatch(result.Changes)
		if err != nil || changes.Tenant != result.Tenant || !changes.At.Equal(result.At) || result.Notices != nil || len(result.Intents) > 0 || len(result.States) > 0 || result.Control != nil ||
			result.Outcome != "ok" {
			return result, fmt.Errorf("work result has invalid business changes")
		}
	}
	return result, nil
}

type OperationClaim struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	Tenant      string          `json:"tenant"`
	App         string          `json:"app"`
	Key         string          `json:"key"`
	At          time.Time       `json:"at"`
	RequestHash string          `json:"requestHash"`
	Digest      string          `json:"digest"`
	Before      platform.Effect `json:"before"`
	After       platform.Effect `json:"after"`
	PriorWork   json.RawMessage `json:"priorWork,omitempty"`
	Work        json.RawMessage `json:"work"`
}

func OperationClaimDigest(r OperationClaim) (string, error) {
	r.Digest = ""
	return platform.CanonicalDigest(r)
}

func DecodeOperationClaim(raw []byte) (OperationClaim, error) {
	var saved OperationClaim
	if _, err := platform.DecodeValue(raw, platform.AcceptedResultMaxBytes); err != nil {
		return saved, err
	}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return saved, err
	}
	digest, err := OperationClaimDigest(saved)
	hash, _ := platform.CanonicalDigest([]any{saved.Before, saved.PriorWork})
	if err != nil || saved.Kind != "operation-claim" || saved.Version != 1 || saved.Tenant == "" || saved.App != controlowner.PlatformApp || saved.RequestHash != hash || saved.Digest != digest || saved.Before.Endpoint != OperationEndpoint || saved.Before.ID == "" || platform.EffectSettled(saved.Before.State) || saved.At.IsZero() {
		return saved, fmt.Errorf("invalid operation ownership result")
	}
	if saved.After.Generation == 0 || saved.Key != fmt.Sprintf("operation-claim:%s:%d", saved.Before.ID, saved.After.Generation) {
		return saved, fmt.Errorf("operation claim identity differs")
	}
	return saved, nil
}

func (r Work) ChangePlan() ChangePlan {
	return ChangePlan{At: r.At, Changes: r.Changes, States: r.States,
		Control: r.Control, Intents: r.Intents, Notices: r.Notices, Unique: r.Unique, CheckUnique: r.Version >= 3}
}

func SystemDefinitionInstallation(schema string, image json.RawMessage) (json.RawMessage, error) {
	if schema != systemscope.SchemaActivate && schema != systemscope.SchemaFinished && schema != systemscope.SchemaRollback {
		return nil, nil
	}
	var saved struct {
		Installation json.RawMessage `json:"installation"`
	}
	if err := json.Unmarshal(image, &saved); err != nil {
		return nil, err
	}
	return saved.Installation, nil
}

type Composite struct {
	Version     int                      `json:"version"`
	Kind        string                   `json:"kind"`
	Tenant      string                   `json:"tenant"`
	App         string                   `json:"app"`
	Member      string                   `json:"member"`
	Key         string                   `json:"key"`
	Context     string                   `json:"context,omitempty"`
	At          time.Time                `json:"at"`
	Edits       []platform.CompositeEdit `json:"edits"`
	Changes     json.RawMessage          `json:"changes,omitempty"`
	Refusal     string                   `json:"refusal,omitempty"`
	RequestHash string                   `json:"requestHash"`
	Digest      string                   `json:"digest"`
}

func CompositeRequestHash(tenant, member, key string, edits []platform.CompositeEdit, context ...string) (string, error) {
	hash, err := platform.CanonicalDigest(struct {
		Tenant, Member, Key string
		Edits               []platform.CompositeEdit
	}{tenant, member, key, edits})
	if err != nil || len(context) == 0 || context[0] == "" {
		return hash, err
	}
	return platform.CanonicalDigest([]string{hash, context[0]})
}

func EncodeComposite(saved Composite) ([]byte, error) {
	var err error
	if saved.Digest, err = CompositeDigest(saved); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(saved)
	if err != nil {
		return nil, err
	}
	_, err = DecodeComposite(raw)
	return raw, err
}

func CompositeDigest(saved Composite) (string, error) {
	saved.Digest = ""
	return platform.CanonicalDigest(saved)
}

func SameCompositePayload(a, b []byte) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	left, err := platform.CanonicalDigest(json.RawMessage(a))
	if err != nil {
		return false
	}
	right, err := platform.CanonicalDigest(json.RawMessage(b))
	return err == nil && left == right
}

func DecodeComposite(raw []byte) (Composite, error) {
	var saved Composite
	if len(raw) == 0 || len(raw) > 4<<20 {
		return saved, fmt.Errorf("composite result is empty or too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&saved); err != nil {
		return saved, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return saved, fmt.Errorf("composite result has trailing data")
	}
	hash, err := CompositeRequestHash(saved.Tenant, saved.Member, saved.Key, saved.Edits, saved.Context)
	if saved.Version == 3 {
		context, failure := hex.DecodeString(saved.Context)
		if failure != nil || len(context) != 32 {
			return saved, fmt.Errorf("composite batch context is invalid")
		}
	} else if saved.Context != "" {
		return saved, fmt.Errorf("legacy composite contains a batch context")
	}
	if err != nil || (saved.Version != 1 && saved.Version != 2 && saved.Version != 3) || saved.Kind != "composite-result" || saved.App != "build" ||
		saved.Tenant == "" || saved.Member == "" || saved.Key == "" || saved.At.IsZero() ||
		len(saved.Edits) == 0 || len(saved.Edits) > 64 || saved.RequestHash != hash {
		return saved, fmt.Errorf("invalid composite result identity")
	}
	digest, err := CompositeDigest(saved)
	if err != nil || saved.Digest != digest {
		return saved, fmt.Errorf("composite result digest differs")
	}
	for i, edit := range saved.Edits {
		if edit.Schema == "" || edit.Target == nil || edit.Target.GetType() == "" {
			return saved, fmt.Errorf("composite edit %d has no schema or target", i+1)
		}
	}
	if saved.Version == 1 && (len(saved.Changes) != 0 || saved.Refusal != "") || saved.Version >= 2 && (len(saved.Changes) == 0) == (saved.Refusal == "") {
		return saved, fmt.Errorf("composite result has incompatible changes or refusal")
	}
	if saved.Version >= 2 && saved.Refusal == "" {
		batch, receipt, err := DecodeBatch(saved.Changes)
		if err != nil || batch.Tenant != saved.Tenant || batch.App != saved.App || !batch.At.Equal(saved.At) || receipt.GetSubmission().GetPrincipalId() != saved.Member {
			return saved, fmt.Errorf("composite result changes differ: %v", err)
		}
		for i, edit := range saved.Edits {
			found := false
			for _, decision := range batch.Decisions {
				r := new(pb.ChangeRecord)
				if protojson.Unmarshal(decision.Receipt, r) != nil {
					return saved, fmt.Errorf("invalid composite receipt")
				}
				x := r.GetSubmission()
				if x.GetAuthority() == "build" && x.GetPrincipalId() == saved.Member && x.GetIdempotencyKey() == strings.TrimPrefix(saved.Key, "composite:")+"#"+strconv.Itoa(i) && x.GetSchema().GetName() == edit.Schema && proto.Equal(x.GetTarget(), edit.Target) && SameCompositePayload(x.GetPayload(), edit.Payload) {
					found = true
				}
			}
			if !found {
				return saved, fmt.Errorf("composite edit %d has no sealed decision", i)
			}
		}
	}
	return saved, nil
}
