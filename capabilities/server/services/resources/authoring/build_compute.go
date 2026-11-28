package authoring

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"strings"
	"time"
)

func (b *Build) Answer(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *platform.Error {
	if effect.Endpoint == protocol.DeferredRequestEndpoint {
		return b.answerDeferredRequest(c, effect, out, now)
	}
	if effect.Event == protocol.CapabilityCallEvent {
		return b.answerCapabilityCall(c, effect, out, now)
	}
	if strings.HasPrefix(effect.Endpoint, dataowner.WritebackEndpoint) {
		return b.data.Answer(c, effect, out, now)
	}
	return b.functions.AnswerCompilation(c, b, effect, out, now)
}

func (b *Build) answerDeferredRequest(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *platform.Error {
	var req struct {
		Action     string         `json:"action"`
		Reply      string         `json:"reply"`
		Submission *pb.Submission `json:"submission"`
	}
	if json.Unmarshal([]byte(effect.Body), &req) != nil || req.Reply == "" || req.Submission == nil {
		return nil
	}
	typ, id, ok := strings.Cut(effect.Target, "/")
	if !ok || typ == "" || id == "" {
		return nil
	}
	outcome := map[string]string{"delivered": "accepted", "rejected": "refused", "discarded": "refused"}[out.Result]
	if effect.State == "failed" {
		outcome = "refused"
	}
	if outcome == "" {
		return nil
	}
	verb := req.Reply[strings.LastIndex(req.Reply, ".")+1:]
	replySchema := typ + "." + verb
	baseRaw, _ := json.Marshal(protocol.Answer{Call: req.Submission.GetTarget().GetId(), Action: req.Action, Outcome: outcome, Reason: out.Detail})
	payloadMap := map[string]any{}
	_ = json.Unmarshal(baseRaw, &payloadMap)
	if outcome == "accepted" {
		var delivered struct {
			Target string `json:"target"`
		}
		if json.Unmarshal(out.Answer, &delivered) == nil && delivered.Target != "" {
			payloadMap["ref"] = delivered.Target
			if pTyp, pID, ok := strings.Cut(delivered.Target, "/"); ok && pID != "" {
				if raw, readErr := c.ReadRecord(pTyp, pID, now); readErr == nil {
					var rec map[string]any
					dec := json.NewDecoder(strings.NewReader(string(raw)))
					dec.UseNumber()
					if dec.Decode(&rec) == nil {
						rawReserved, _ := json.Marshal(protocol.Answer{Tag: "_", Call: "_", Action: "_", Outcome: "_", Code: "_", Reason: "_", Ref: "_"})
						reservedKeys := map[string]any{}
						_ = json.Unmarshal(rawReserved, &reservedKeys)
						if declared, exists := b.Manifest().Actions.Action(replySchema); exists {
							for _, field := range declared.Payload {
								if _, reserved := reservedKeys[field.Name]; !reserved {
									if v, has := rec[field.Name]; has && v != nil {
										payloadMap[field.Name] = v
									}
								}
							}
						}
					}
				}
			}
		}
	}
	rawPayload, _ := json.Marshal(payloadMap)
	_, err := platform.Decide(c, b, &pb.Submission{
		TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID,
		IdempotencyKey: fmt.Sprintf("reply:%s:%d:%s", effect.ID, effect.Attempts, out.Result),
		Target:         &pb.EntityRef{Type: typ, Id: id},
		Schema:         &pb.SchemaRef{Name: replySchema, Version: 1},
		Payload:        rawPayload,
	}, now)
	return err
}

func (b *Build) OperationDefinition(name string, version int) (platform.Operation, int, bool) {
	return b.functions.OperationDefinition(name, version)
}

func (b *Build) CodeReleaseAssets() ([]platform.ReleaseAsset, error) {
	return b.functions.CodeReleaseAssets()
}

func (b *Build) InstallOperationAsset(asset platform.ReleaseAsset) error {
	return b.functions.InstallOperationAsset(asset)
}
