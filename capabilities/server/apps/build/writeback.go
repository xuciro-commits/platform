package build

import (
	"encoding/json"
	"fmt"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (b *Build) writebackEntity() platform.Entity {
	return platform.Entity{Type: WritebackType, Title: "Writeback", Plural: "Writebacks", Model: Writeback{}, Display: "title",
		Description: "An accepted action on an object sent to an external system through a connection: queued, in order, retried with the same key for receiver deduplication.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			w := record.(*Writeback)
			if w.State == "published" {
				fixed, err := b.ExecutionWriteback(*w)
				if err != nil {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
				}
				if err := b.checkWritebackConnection(c, *w); err != nil {
					return err
				}
				return b.checkWriteback(c, fixed)
			}
			return nil
		},
		Scope:    platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant, Integrator: platform.ScopeTenant}},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder, Integrator}, Capability: "integrations"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{
				{Name: "publish", Title: "Publish", Description: "Check the writeback and send every matching decision from now on.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}, Do: b.publishWriteback},
				{Name: "pause", Title: "Pause", Description: "Stop sending; what is queued still goes.", From: []string{"published"}, To: []string{"draft"}, Roles: []string{Builder, Integrator}, Capability: "integrations", Payload: []platform.Field{}}}}}
}

func writebackActions() []platform.Action {
	return []platform.Action{{Schema: SchemaWritebackAnswered, Target: WritebackType, Capability: "integrations", Title: "Keep answer", Description: "Retain what the external system answered to one writeback.", Automation: true,
		Payload: []platform.Field{{Name: "answer", Type: "json", Required: true, Description: "The attempt's result and the answer's first bytes"}}}}
}

func (b *Build) publishWriteback(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	w, ok := record.(*Writeback)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkWriteback(c, *w); err != nil {
		return err
	}
	w.Publisher = c.ID
	if !c.Replaying {
		fixed, err := b.freezeDirectIntegration(platform.AssetWriteback, *w)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		*w = fixed.(Writeback)
	}
	return nil
}

func (b *Build) checkWriteback(c platform.Caller, w Writeback) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	if err := b.checkName(w.Name, w.ID); err != nil {
		return refuse(err.Error())
	}
	if err := b.checkWritebackConnection(c, w); err != nil {
		return err
	}
	info, ok := b.lookupEntity(w.Object)
	if !ok {
		return refuse("The object {object} is not installed", w.Object)
	}
	if info.App != ID {
		return refuse("A writeback currently listens to objects owned by Build; native application objects need an owner callback")
	}
	if w.On == "" || strings.ContainsAny(w.On, "./ ") {
		return refuse("After action is the verb of the object's action: create, edit or a declared action's name")
	}
	if !slices.Contains([]string{"", "POST", "PUT", "PATCH"}, w.Method) {
		return refuse("The method is POST, PUT or PATCH")
	}
	if strings.Contains(w.Path, "://") {
		return refuse("The path is relative to the connection's address")
	}
	for _, m := range append(slices.Clone(w.Mapping), w.Result...) {
		if m.From == "" || m.To == "" {
			return refuse("Every mapping names both sides")
		}
		if !slices.Contains([]string{"", "string", "number", "boolean", "date"}, m.Convert) {
			return refuse("A conversion is one of string, number, boolean, date")
		}
	}
	for _, m := range w.Result {
		if f, ok := info.Field(m.To); !ok || f.ReadOnly {
			return refuse("The answer field {field} is not a writable field of the object", m.To)
		}
	}
	return nil
}

func (b *Build) checkWritebackConnection(c platform.Caller, w Writeback) *kernel.Error {
	refuse := func(message string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, message, args...)
	}
	conn, ok := platform.Get[Connection](c, w.Connection)
	if ok {
		fixed, err := b.ExecutionConnection(conn)
		if err != nil {
			return refuse(err.Error())
		}
		conn = fixed
	}
	if !ok || conn.State != "ready" {
		return refuse("The connection is not ready; check it first")
	}
	if conn.Kind != "http" && conn.Kind != "odata" {
		return refuse("A writeback goes through an http or OData connection")
	}
	if conn.Protocol != nil && (w.On != "dispatch" || w.Method != "" && w.Method != "POST" || strings.Trim(w.Path, "/") != "execute") {
		return refuse("Protocol commands use a dispatch-only POST execute writeback")
	}
	return nil
}

func (b *Build) submitWritebackAnswered(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return b.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var payload struct {
			Answer WritebackAns `json:"answer"`
		}
		w, ok := platform.Get[Writeback](c, s.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The writeback does not exist")
		}
		if json.Unmarshal(s.GetPayload(), &payload) != nil || payload.Answer.Result == "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "An answer carries its result")
		}
		return func(r *pb.ChangeRecord) {
			switch payload.Answer.Result {
			case "delivered":
				w.Sent++
			case "rejected":
				w.Rejected++
			case "unknown":
				w.Unknown++
			default:
				w.Failed++
			}
			payload.Answer.At = now
			w.Answers = append([]WritebackAns{payload.Answer}, w.Answers...)
			if len(w.Answers) > writebackAnswerKept {
				w.Answers = w.Answers[:writebackAnswerKept]
			}
			c.Put(r, w)
		}, nil
	})
}

// Answered takes a settled writeback effect (platform.Answerer): it keeps the
// answer on the writeback and, when delivered with a Result mapping, writes
// the answer's fields onto the record through the object's own edit.
func (b *Build) answered(c platform.Caller, effect platform.Effect, out platform.Outcome, now time.Time) *kernel.Error {
	var req WritebackRequest
	if json.Unmarshal([]byte(effect.Body), &req) != nil || req.Writeback == "" {
		return nil
	}
	if req.Dispatch != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "failed", "unknown": "unknown", "discarded": "unknown"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		if outcome == "" {
			return nil
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, dispatchSubmission(c, req.Dispatch, platform.DispatchAnswerSchema, key, map[string]any{"intent": effect.ID, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	if req.Confirmation != "" {
		outcome := map[string]string{"delivered": "accepted", "rejected": "refused", "unknown": "unknown", "discarded": "refused"}[out.Result]
		if effect.State == "failed" {
			outcome = "failed"
		}
		tag := req.Party
		if req.Compensates != "" {
			tag += ":compensate"
		}
		key := fmt.Sprintf("%s:%d:%s", effect.ID, effect.Attempts, out.Result)
		_, err := platform.Decide(c, b, confirmationSubmission(c, req.Confirmation, confirmationAnswer, key, map[string]any{"tag": tag, "outcome": outcome, "reason": out.Detail, "answer": out.Answer}, now), now)
		return err
	}
	w, ok := platform.Get[Writeback](c, req.Writeback)
	if !ok {
		return nil
	}
	// New requests carry their original answer mapping, so an accepted reply
	// never acquires a different meaning after a compatible definition upgrade.
	if req.Version == 1 {
		w.Object, w.Result = req.Object, slices.Clone(req.Result)
		if w.Object == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Writeback request has no frozen target")
		}
	} else if req.Version != 0 || w.DefinitionVersion != 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Historical writeback request requires its original unversioned definition")
	}
	ans := WritebackAns{Effect: effect.ID, Target: effect.Target, Result: out.Result, Detail: out.Detail, Answer: clipAnswer(out.Answer)}
	if out.Result != "delivered" && out.Result != "rejected" && out.Result != "unknown" {
		ans.Result = "failed"
	}
	payload, _ := json.Marshal(map[string]any{"answer": ans})
	if _, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answered:" + effect.ID + map[bool]string{true: ":unknown"}[out.Result == "unknown"],
		Target: &pb.EntityRef{Type: WritebackType, Id: w.ID}, Schema: &pb.SchemaRef{Name: SchemaWritebackAnswered, Version: 1}, Payload: payload}, now); err != nil {
		return err
	}
	typ, id, ok := strings.Cut(effect.Target, "/")
	if out.Result != "delivered" || !ok || typ != w.Object || len(w.Result) == 0 {
		return nil
	}
	fields := w.Fields(out.Answer)
	if len(fields) == 0 {
		return nil
	}
	edit, _ := json.Marshal(fields)
	_, err := platform.Decide(c, b, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, IdempotencyKey: "answer-fields:" + effect.ID,
		Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + ".edit", Version: 1}, Payload: edit}, now)
	return err
}

func clipAnswer(raw []byte) string {
	s := string(raw)
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}
